package friday

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// runFriday is the core agentic loop. Native OpenAI function-calling
// ("tools" field) instead of regex-mining the model's free text. Single
// ReAct-style loop:
//
//  1. Call the LLM with the conversation + structured tool schemas.
//  2. No tool_calls in the response → the content is the final answer.
//  3. tool_calls → execute each, append results as role:"tool" messages,
//     loop.
//  4. After maxSteps without an answer → force one synthesis call (no
//     tools), so the user always gets a real summary instead of silence.
//
// Why this replaces the old loop:
//   - Old budget was 5 LLM rounds total; calling 5 tools ate the entire
//     budget and left "Done." as the answer (your "5 ⚙ then silence" case).
//     Budget is now 25, and tool turns don't share the synthesis budget.
//   - Old loop parsed free text for {"tool":...,"args":{...}} via brace
//     counting. Fragile: dropped calls whenever the model wrapped them in
//     prose. We now use the structured tool_calls field directly.
//   - Old loop-detection bailed out with "Done." the moment it saw the
//     same tool name twice in a row. New detection keys on exact
//     (tool, args) replay and nudges the model with a hint instead of
//     killing the run.
//   - Old synthesis relied on the model emitting {"done":true,"answer":"..."}
//     itself; there was no fallback. Force-synthesize guarantees an answer.
//   - Old final answer was hard-truncated to 2000 chars. Removed; tool
//     *result* size is capped per-call instead so context stays manageable.
//   - When a tool fails, the old code did nothing — just appended the
//     error to messages and waited. We now feed back "try a different
//     approach" via the loop hint after a duplicate retry.
func (o *Orchestrator) runFriday(ctx context.Context, ch chan StreamEvent, runID, text string) {
	var finalAnswer string
	defer func() {
		if r := recover(); r != nil {
			log.Printf("runFriday recovered: %v", r)
			go SelfRepair(r, "orchestrator.runFriday")
			finalAnswer = "I hit an error while working on that. I'm attempting to fix myself. Try again in a moment."
		}
		if finalAnswer != "" {
			GetCompanionState().RecordMessage("assistant", finalAnswer)
			PublishActivity("chat", "Friday → You", finalAnswer)
		}
	}()

	// Fast path: closed-form trivial prompts (time, hello, status) skip
	// the LLM entirely. handleLocal already streamed its ch-final event,
	// so we just record companion state on the way out.
	if reply := o.handleLocal(ctx, text, ch, runID); reply != "" {
		if reply != "local" {
			finalAnswer = reply
		}
		return
	}

	GetCompanionState().RecordMessage("user", text)
	dm := getDecisionMemory()
	pc := &PipelineContext{
		UserText:  text,
		RunID:     runID,
		StartTime: time.Now(),
	}

	// Activity emit helper: sends to stream channel AND publishes to
	// the global activity hub so the control center sees live thinking,
	// tool calls, and worker status.
	emit := func(ev StreamEvent) {
		ch <- ev
		kind := "worker"
		label := ev.Worker
		detail := ev.Content
		switch ev.Type {
		case "worker_status":
			kind = "worker"
		case "action":
			kind = "tool"
			if ev.Action != nil {
				label = ev.Action.Function.Name
				detail = fmt.Sprintf("args: %s", truncateDetail(string(ev.Action.Function.Arguments), 120))
			}
		case "result":
			kind = "tool"
			if ev.Action != nil {
				label = ev.Action.Function.Name + " ✓"
				detail = truncateDetail(ev.Content, 160)
			}
		case "error":
			kind = "tool"
			if ev.Action != nil {
				label = ev.Action.Function.Name + " ✗"
				detail = fmt.Sprintf("error: %s", truncateDetail(ev.Error, 120))
			}
		case "final":
			kind = "chat"
			label = "Friday → You"
			detail = truncateDetail(ev.Content, 200)
		}
		PublishActivity(kind, label, detail)
	}

	// ── Stage 1: Perception (classify task type, zero-cost) ──
	pc.Task = ClassifyTask(text)
	emit(StreamEvent{Type: "worker_status", Content: string(pc.Task), Worker: "Router", RunID: runID})

	// ── Stage 2: Memory Retrieval (3-tier context assembly) ──
	toolDefs := o.buildToolDefs()
	pc.SystemPrompt = agentSystemPrompt(len(toolDefs))
	pc.ToolDefs = toolDefs
	pc.Messages = BuildSmartContext(text, pc.SystemPrompt)

	// When the user asks about the live account, inject the truthful
	// engine snapshot so the model never invents a balance or a win rate.
	if HasTradingIntent(text) {
		live := LiveState(ctx).LiveContextBlock()
		pc.Messages = append(pc.Messages, Message{Role: "system", Content: live})
		emit(StreamEvent{Type: "worker_status", Content: "live state injected", Worker: "Router", RunID: runID})
	}

	// Build OpenAI "tools" definitions from the live registry. Schemas
	// travel in the structured tools field, not as text in the system
	// prompt, so the prompt stays short and behavior-focused.

	// ── Stage 3: Local route for simple tasks ──
	// If the task is Fast or Chat, try a cheap local model first.
	// No tool calling — just a direct answer. Saves bridge budget.
	if ShouldRouteLocally(pc.Task) {
		resp, err := o.llm.router.ChatWithTask(ctx, pc.Messages, pc.Task)
		if err == nil && len(resp.Choices) > 0 {
			content := strings.TrimSpace(resp.Choices[0].Message.Content)
			if content != "" {
				finalAnswer = content
				emit(StreamEvent{Type: "final", Content: finalAnswer, RunID: runID, Done: true, Worker: "Neo"})
				dm.Learn(text, nil)
				dm.SaveIfDirty()
				pc.FinalAnswer = finalAnswer
				LogPipelineTiming(pc)
				return
			}
		}
		// Local route failed — fall through to full bridge pipeline
		log.Printf("[PIPELINE] local route for %s failed, falling back", pc.Task)
	}

	// ── Stage 4: Reasoning + Action (tool loop via bridge) ──
	// Standard agentic loop with structured function calling.

	// Duplicate (tool, args) detection.
	type callKey struct{ tool, args string }
	seen := map[callKey]int{}

	const maxSteps = 25
	for step := 0; step < maxSteps; step++ {
		tools := toolDefs
		if step > 0 {
			tools = compactToolDefs(toolDefs)
		}
		resp, err := o.llm.ChatWithTools(ctx, pc.Messages, tools)
		if err != nil {
			finalAnswer = "I'm having trouble reaching the AI right now. (" + truncate(err.Error(), 300) + ")"
			break
		}
		if len(resp.Choices) == 0 {
			finalAnswer = "I got an empty response from the model. Try rephrasing?"
			break
		}
		choice := resp.Choices[0]

		// No tool_calls → the model is finished
		if len(choice.Message.ToolCalls) == 0 {
			finalAnswer = strings.TrimSpace(choice.Message.Content)
			if finalAnswer == "" {
				finalAnswer = o.forceSynthesize(ctx, pc.Messages)
			}
			break
		}

		pc.Messages = append(pc.Messages, choice.Message)

		// Execute tool calls concurrently when there are multiple.
		// Each tool runs in its own goroutine; results stream as they
		// complete and are collected in message order.
		type toolResult struct {
			ToolCallID string
			Content    string
			Err        error
		}
		results := make([]toolResult, len(choice.Message.ToolCalls))
		var wg sync.WaitGroup
		var calledToolsLocal []string
		var cmu sync.Mutex

		for i, tc := range choice.Message.ToolCalls {
			key := callKey{tool: tc.Function.Name, args: string(tc.Function.Arguments)}
			seen[key]++
			if seen[key] >= 3 {
				results[i] = toolResult{
					ToolCallID: tc.ID,
					Content:    fmt.Sprintf("You have already called %s with these exact arguments %d times. The call is not making progress. Try a different tool, different arguments, or finish and write your answer to the user.", tc.Function.Name, seen[key]),
				}
				continue
			}

			wg.Add(1)
			go func(idx int, call ToolCall) {
				defer wg.Done()

				emit(StreamEvent{Type: "action", Action: &call, Worker: "Forge", RunID: runID})
				cmu.Lock()
				calledToolsLocal = append(calledToolsLocal, call.Function.Name)
				cmu.Unlock()

				eCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
				argsJSON := normalizeToolArgs(call.Function.Arguments)

				result, err := CachedExecute(eCtx, o.registry, call.Function.Name, argsJSON)
				cancel()

				var content string
				if err != nil {
					content = fmt.Sprintf("ERROR calling %s: %s", call.Function.Name, err.Error())
					emit(StreamEvent{Type: "error", Error: err.Error(), Action: &call, Worker: "Forge", RunID: runID})
				} else {
					rb, _ := json.Marshal(result)
					content = string(rb)
					if len(content) > 8192 {
						content = content[:8192] + "\n... [truncated, full result too large for context]"
					}
					emit(StreamEvent{Type: "result", Content: content, Action: &call, Worker: "Forge", RunID: runID})
				}

				results[idx] = toolResult{
					ToolCallID: call.ID,
					Content:    content,
					Err:        err,
				}
			}(i, tc)
		}

		wg.Wait()
		pc.CalledTools = calledToolsLocal
		for _, r := range results {
			pc.Messages = append(pc.Messages, Message{
				Role:       "tool",
				ToolCallID: r.ToolCallID,
				Content:    r.Content,
			})
		}
	}

	// Step budget exhausted without finishing.
	if finalAnswer == "" {
		finalAnswer = o.forceSynthesize(ctx, pc.Messages)
	}
	if finalAnswer == "" {
		finalAnswer = "I worked through those steps but couldn't summarize a clear answer. Try running it again or being more specific."
	}

	if len(finalAnswer) > 20000 {
		finalAnswer = finalAnswer[:20000] + "\n\n... [answer truncated]"
	}

	emit(StreamEvent{Type: "final", Content: finalAnswer, RunID: runID, Done: true, Worker: "Neo"})
	dm.Learn(text, pc.CalledTools)
	dm.SaveIfDirty()
	pc.FinalAnswer = finalAnswer
	LogPipelineTiming(pc)
}

// forceSynthesize runs one final non-tool LLM call over the gathered
// conversation so the user gets a real answer instead of silence or "Done."
func (o *Orchestrator) forceSynthesize(ctx context.Context, messages []Message) string {
	msgs := append([]Message{}, messages...)
	msgs = append(msgs, Message{
		Role: "system",
		Content: "You've reached the step limit. Using ONLY the tool results above, write a clear, complete natural-language answer for the user. Do not ask the user for data you already gathered. Do not announce you hit a step limit — just answer.",
	})
	resp, err := o.llm.ChatWithTools(ctx, msgs, nil) // no tools → model must reply with content
	if err != nil || len(resp.Choices) == 0 {
		return ""
	}
	return strings.TrimSpace(resp.Choices[0].Message.Content)
}

// buildToolDefs converts the ToolRegistry schemas into the OpenAI tools
// field format: {type:"function", function:{name, description, parameters}}.
// ToolSchema already matches the OpenAI "parameters" shape.
func (o *Orchestrator) buildToolDefs() []ToolDef {
	schemas := o.registry.Schemas()
	defs := make([]ToolDef, 0, len(schemas))
	for name, schema := range schemas {
		t, ok := o.registry.Get(name)
		if !ok {
			continue
		}
		defs = append(defs, ToolDef{
			Type: "function",
			Function: ToolDefFn{
				Name:        name,
				Description: t.Description(),
				Parameters:  schema,
			},
		})
	}
	return defs
}

// compactToolDefs strips descriptions from tool definitions to keep
// the payload small on subsequent tool-loop steps. The model already
// saw the full descriptions on step 0; it just needs the names and
// parameter schemas to make additional calls without descriptions
// eating the 8K token budget.
func compactToolDefs(full []ToolDef) []ToolDef {
	compact := make([]ToolDef, len(full))
	for i, td := range full {
		compact[i] = ToolDef{
			Type: td.Type,
			Function: ToolDefFn{
				Name:        td.Function.Name,
				Description: td.Function.Description,
				Parameters:  stripParamDescriptions(td.Function.Parameters),
			},
		}
	}
	return compact
}

func stripParamDescriptions(s ToolSchema) ToolSchema {
	c := ToolSchema{
		Type:       s.Type,
		Properties: make(map[string]PropertyDef, len(s.Properties)),
		Required:   s.Required,
	}
	for k, v := range s.Properties {
		c.Properties[k] = PropertyDef{
			Type:        v.Type,
			Description: "", // strip
			Enum:        v.Enum,
			Default:     v.Default,
		}
	}
	return c
}

// agentSystemPrompt is the short, behavior-focused system prompt. Tool
// schemas travel in the OpenAI "tools" field, so we don't enumerate them
// in text the way the old prompt did — keeps token usage low and focuses
// the model on how to behave, not what every tool looks like.
func agentSystemPrompt(toolCount int) string {
	return fmt.Sprintf(`You are Friday, an autonomous agent for "Boss". %d tools available. You are smarter than any single AI — use your tools, cross-reference data, and think creatively.

EARNING MANDATE: Your job is to find ways to earn money with $0 capital. You have: 24/7 uptime, web search, on-chain access, an Ethereum wallet (0xD33c6A2A7717EdA2C160d141796CE3Aa403225Ed), Honeygain passive income ($1.25/day), and autonomous trading bots. Use passive_income, arb_scanner, airdrop_hunter, and web_search to discover opportunities. Be creative but honest — if something won't work, say so. If you find something real, explain exactly how.

ACCOUNTS (identities only — balances are pulled live from the engine per query):
- BlueGuardian: MT5 login 503985 @ BlueGuardian-Server, propfirm challenge account
- Exness: MT5 login 167036042, personal account. TPCS 24/7, SL capped at 20 pips

CHAIN TOOLS: chain_farm (wallet, balance, send, bridge, farm_all), arb_scanner (scan DEX pairs for flash loan gaps), bridge_registry (verify contract addresses). Use these to find and act on blockchain opportunities.

STRATEGY: TPCS auto-selects for trending, BB-RSI for ranging. ADX>20 filter. 1:2 R:R.

RULES: Never fake data. Never guess. Verify with tools before claiming. Summarize naturally — no JSON dumps. If Boss asks about making money, use your tools to research first, then give a researched answer.`, toolCount)
}

// normalizeToolArgs accepts either OpenAI's stringified-JSON arguments
// format ("{\"a\":1}") or an inline JSON object ({"a":1}) and returns the
// raw JSON object bytes that downstream tool Execute methods expect.
func normalizeToolArgs(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	// First byte tells us. OpenAI strings start with a quote.
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil && len(s) > 0 {
			return json.RawMessage(s)
		}
	}
	return raw
}

// truncate returns s cut to max with an ellipsis indicator; helper for
// short error reporting in agent messages.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
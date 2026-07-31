package llm

import (
	"encoding/json"
)

// ToolCall represents a function/tool call from the LLM
type ToolCall struct {
	ID       string
	Type     string
	Function FunctionCall
}

type FunctionCall struct {
	Name      string
	Arguments json.RawMessage
}

func (tc *ToolCall) Args() map[string]any {
	var args map[string]any
	_ = json.Unmarshal(tc.Function.Arguments, &args)
	return args
}

func (tc *ToolCall) Arg(key string) (any, bool) {
	args := tc.Args()
	v, ok := args[key]
	return v, ok
}

func NewToolCall(id, name string, args map[string]any) *ToolCall {
	argBytes, _ := json.Marshal(args)
	return &ToolCall{
		ID:   id,
		Type: "function",
		Function: FunctionCall{
			Name:      name,
			Arguments: argBytes,
		},
	}
}

// Tool defines a function the LLM can call
type Tool struct {
	Type     string
	Function ToolFunction
}

type ToolFunction struct {
	Name        string
	Description string
	Parameters  ToolParameters
}

type ToolParameters struct {
	Type       string
	Properties map[string]ToolProperty
	Required   []string
}

type ToolProperty struct {
	Type        string
	Description string
	Enum        []string
	Items       *ToolProperty
}

func NewTool(name, description string, params ToolParameters) *Tool {
	return &Tool{
		Type: "function",
		Function: ToolFunction{
			Name:        name,
			Description: description,
			Parameters:  params,
		},
	}
}

// ToolChoice controls tool calling behavior
type ToolChoice string

const (
	ToolChoiceNone     ToolChoice = "none"
	ToolChoiceAuto     ToolChoice = "auto"
	ToolChoiceRequired ToolChoice = "required"
)
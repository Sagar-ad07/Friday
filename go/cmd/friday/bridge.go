package main

import (
	"fmt"
	"log"
	"net/http"
	"regexp"
	"time"

	"github.com/friday-prototype/friday-go/friday"
	"github.com/gin-gonic/gin"
)

// identityRe matches direct questions about Friday's creator / boss / owner.
var identityRe = regexp.MustCompile(`(?i)(who (created|made|built) you|who is your (boss|creator|owner)|who's your (boss|creator|owner)|who (is|was) (the |a )?(creator|boss|owner)|your (boss|creator|owner)|you (were|are) created by|created you)`)

// bridgeRouter is the OpenAI-compatible brain shared by the /v1/chat/completions
// bridge. It is initialized lazily so the .env token loading has already
// happened by the time the first request arrives.
var bridgeRouter *friday.ModelRouter

// identityOverride returns an authoritative answer to direct identity questions
// about Friday's creator/boss, intercepted before the upstream provider is
// called. It inspects only the most recent user message.
func identityOverride(req friday.ChatRequest) (string, bool) {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		m := req.Messages[i]
		if m.Role == "user" {
			if identityRe.MatchString(m.Content) {
				return "I was created by Sagar Adhikari, and my boss is Sagar Adhikari.", true
			}
			return "", false
		}
	}
	return "", false
}

func getBridgeRouter() *friday.ModelRouter {
	if bridgeRouter == nil {
		bridgeRouter = friday.NewModelRouter()
	}
	return bridgeRouter
}

// bridgeHandler serves OpenAI-style chat completions on /v1/chat/completions.
// Any client that speaks the OpenAI protocol (the web UI, curl, scripts) can
// talk to Friday through this endpoint.
func bridgeHandler(c *gin.Context) {
	var req friday.ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "invalid request body: " + err.Error(), "type": "invalid_request_error"}})
		return
	}
	if len(req.Messages) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "messages is required", "type": "invalid_request_error"}})
		return
	}

	// Authoritative identity answer. Sagar Adhikari is Friday's creator and
	// boss; answer it server-side so an upstream provider (e.g. the GLM model
	// deflecting to "Zhipu AI") can never override the owner's directive.
	if answer, ok := identityOverride(req); ok {
		log.Printf("[IDENTITY] server-side override -> %q", answer)
		modelName := req.Model
		if modelName == "" {
			modelName = "friday"
		}
		c.JSON(http.StatusOK, gin.H{
			"id":      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   modelName,
			"choices": []gin.H{{
				"index":         0,
				"finish_reason": "stop",
				"message":       gin.H{"role": "assistant", "content": answer},
			}},
		})
		return
	}

	start := time.Now()
	resp, err := getBridgeRouter().Chat(c.Request.Context(), req.Messages)
	if err != nil {
		log.Printf("[BRIDGE] chat failed: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": fmt.Sprintf("upstream brain error: %v", err), "type": "upstream_error"}})
		return
	}

	// Re-stamp the response so consumers see the real completion time.
	resp.Created = time.Now().Unix()
	if resp.ID == "" {
		resp.ID = fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	}
	if resp.Object == "" {
		resp.Object = "chat.completion"
	}
	if resp.Model == "" {
		resp.Model = req.Model
	}

	log.Printf("[BRIDGE] %d ms, %d choices, model=%s", time.Since(start).Milliseconds(), len(resp.Choices), resp.Model)
	c.JSON(http.StatusOK, resp)
}

// bridgeModelsHandler lists the models the router can serve, in OpenAI
// /v1/models format.
func bridgeModelsHandler(c *gin.Context) {
	router := getBridgeRouter()
	var data []friday.ModelInfo
	for _, p := range router.Providers() {
		data = append(data, friday.ModelInfo{
			ID:      p.Model,
			Object:  "model",
			Created: time.Now().Unix(),
			OwnedBy: p.Name,
		})
	}
	c.JSON(http.StatusOK, friday.ModelsResponse{Object: "list", Data: data})
}

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"time"
)

type BalanceCheck struct {
	Model   string `json:"model"`
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func main() {
	client := &http.Client{Timeout: 10 * time.Second}

	// Test DeepSeek
	fmt.Println("Testing DeepSeek API...")
	ctx := context.Background()
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://api.deepseek.com/v1/chat/completions", nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer sk-ff173ec13f9a4584bfc6d4b1d05e3904")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("DeepSeek Error: %v", err)
	} else {
		defer resp.Body.Close()
		body, _ := ioutil.ReadAll(resp.Body)
		var result struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
			Model string `json:"model"`
			Error *struct {
				Message string `json:"message"`
				Code    string `json:"code"`
			} `json:"error"`
		}

		if err := json.Unmarshal(body, &result); err != nil {
			log.Printf("JSON parse error: %v", err)
		} else if result.Error != nil {
			fmt.Printf("DeepSeek Status: %s (Code: %s)\n", result.Error.Message, result.Error.Code)
		} else {
			fmt.Printf("DeepSeek Model: %s\n", result.Model)
			fmt.Printf("Response: %s\n", result.Choices[0].Message.Content)
		}
	}

	// Test Zhipu
	fmt.Println("\nTesting Zhipu GLM-4.5-Flash...")
	req2, _ := http.NewRequestWithContext(ctx, "POST", "https://api.z.ai/api/paas/v4/chat/completions", nil)
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer 3f51cf7cd663451b875fee31da492d45.TuGlE5PB2759In6y")
	req2.Body = ioutil.NopCloser(strings.NewReader(`{
		"model": "glm-4.5-flash",
		"messages": [{"role": "user", "content": "Balance check"}],
		"max_tokens": 10
	}`))

	resp2, err := client.Do(req2)
	if err != nil {
		log.Printf("Zhipu Error: %v", err)
	} else {
		defer resp2.Body.Close()
		body2, _ := ioutil.ReadAll(resp2.Body)
		var result2 struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
			Model string `json:"model"`
			Error *struct {
				Message string `json:"message"`
				Code    string `json:"code"`
			} `json:"error"`
		}

		if err := json.Unmarshal(body2, &result2); err != nil {
			log.Printf("JSON parse error: %v", err)
		} else if result2.Error != nil {
			fmt.Printf("Zhipu Status: %s (Code: %s)\n", result2.Error.Message, result2.Error.Code)
		} else {
			fmt.Printf("Zhipu Model: %s\n", result2.Model)
			fmt.Printf("Response: %s\n", result2.Choices[0].Message.Content)
		}
	}
}

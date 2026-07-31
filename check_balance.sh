#!/bin/bash

echo "=== TESTING API BALANCES ==="
echo ""

# Test DeepSeek
echo "1. Testing DeepSeek API..."
curl -X POST "https://api.deepseek.com/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-ff173ec13f9a4584bfc6d4b1d05e3904" \
  -d '{
    "model": "deepseek-chat",
    "messages": [{"role": "user", "content": "Balance check"}],
    "max_tokens": 10
  }' -s -w "\n\nHTTP Status: %{http_code}\n" | head -20
echo ""

# Test Zhipu
echo "2. Testing Zhipu API..."
curl -X POST "https://open.bigmodel.cn/api/paas/v4/chat/completions" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer 3f51cf7cd663451b875fee31da492d45.TuGlE5PB2759In6y" \
  -d '{
    "model": "glm-4-32b",
    "messages": [{"role": "user", "content": "Balance check"}]
  }' -s -w "\n\nHTTP Status: %{http_code}\n" | head -20
echo ""

# Test Zhipu free
echo "3. Testing Zhipu GLM-4.5-Flash..."
curl -X POST "https://api.z.ai/api/paas/v4/chat/completions" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer 3f51cf7cd663451b875fee31da492d45.TuGlE5PB2759In6y" \
  -d '{
    "model": "glm-4.5-flash",
    "messages": [{"role": "user", "content": "Balance check"}]
  }' -s -w "\n\nHTTP Status: %{http_code}\n" | head -20
echo ""

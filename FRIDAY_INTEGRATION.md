## Friday Integration Guide

**Goal:** Configure Friday to use the Failover Proxy

**Current Friday Status:**
- 77 tools available
- Orchestrator ready
- zai-paid primary provider
- Voice configured

**Integration Plan:**

### Step 1: Friday already works with OpenAI-compatible endpoints
Friday uses OpenAI format for all LLM providers

### Step 2: The proxy is already OpenAI-compatible
Proxy accepts: /v1/chat/completions
Proxy returns: OpenAI format responses

### Step 3: Configure Friday's primary provider
Friday needs to point to: http://localhost:3000/v1/chat/completions

**Configuration Method:**
Option A: Update .env file with custom endpoint
Option B: Create Friday plugin to use proxy
Option C: Use OpenRouter with custom base URL

**Test Friday Voice:**
1. Say "hello" or use voice command
2. Friday routes through failover proxy
3. Llama 3.3 70B responds
4. 100% free

**Next Steps:**
Need to configure Friday's .env to use localhost:3000 instead of current provider.

**Verdict:** Friday can easily use this proxy. Need to configure endpoint in Friday's configuration.
## Claude API Setup - Step by Step

### Step 1: Get Your API Key
1. Go to: https://console.anthropic.com/settings/keys
2. Click "Create Key" or "New API Key"
3. Copy the COMPLETE key (starts with `sk-ant-api03-`)
4. Make sure you copy ALL characters - no truncation

### Step 2: Update Your .env File
Open `D:\Friday - Prototype\.env` and add/edit:
```
CLAUDE_API_KEY=sk-ant-api03-YOUR_FULL_KEY_HERE
```

### Step 3: Test the Connection
1. Close current proxy (Ctrl+C)
2. Restart proxy:
   ```powershell
   cd "D:\Friday - Prototype"
   $env:CLAUDE_API_KEY="CLAUDE_API_KEY"
   Start-Process -FilePath "go\claude_proxy.exe" -WindowStyle Hidden
   ```
3. Test health:
   ```powershell
   Invoke-RestMethod -Uri "http://localhost:9000/health"
   ```

### Step 4: Test Full Flow
```powershell
Invoke-RestMethod -Method POST -Uri "http://localhost:9000/proxy" -ContentType "application/json" -Body '{"model":"gpt-3.5-turbo","messages":[{"role":"user","content":"test message"}]}'
```

### Step 5: Test Friday Voice
Say "hello" or use voice command to test Claude integration

---

**Need your new Claude API key to continue?**

Once you paste the key, I'll:
1. Update .env file
2. Restart proxy with correct key
3. Test Claude connection
4. Test Friday voice routing
5. Verify 100% smooth operation
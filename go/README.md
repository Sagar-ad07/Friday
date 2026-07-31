# Friday Go Implementation

Single-binary AI assistant with native desktop window, 55 tools, real-time trading, and self-healing.

## Build

```powershell
cd D:\Friday - Prototype\go
go build -o friday.exe ./cmd/friday/
```

Output: `friday.exe` (~33MB, no external files needed).

## Run

```powershell
.\friday.exe
```

Opens a native 1200x800 WebView2 window. Falls back to default browser if WebView2 unavailable.

## Quick Test

```powershell
# Check server is running
curl http://localhost:8000/health

# Check system status (online/offline, services, uptime)
curl http://localhost:8000/status

# Chat via SSE streaming
curl -X POST http://localhost:8000/command/stream -d "{\"text\":\"hello\"}"
```

## Management

```powershell
.\friday.ps1 status    # Check running + service health
.\friday.ps1 logs      # View last 20 log lines
.\friday.ps1 stop      # Graceful shutdown
.\friday.ps1 restart   # Full restart
```

## Dependencies

- Go 1.26+
- Windows 10/11 (WebView2 runtime — built-in on Windows 11)
- No C compiler required (zero CGo usage)

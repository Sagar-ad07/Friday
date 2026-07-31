@echo off
TITLE Friday Control Center - Standalone Mode
cd /d "%~dp0"

echo ===================================================
echo   FRIDAY CONTROL CENTER (Standalone)
echo   Shows: Exness | Blue Guardian | XMRig Miner
echo ===================================================
echo.
echo   This requires:
echo     1. MT5 terminal running (Exness account logged in)
echo     2. MT5 Bridge running on port 8001
echo     3. Friday server running on port 8000
echo.
echo   Use launch_all.bat to start everything at once.
echo.

REM Check if bridge is up
curl -s http://localhost:8001/health >nul 2>nul
if %ERRORLEVEL% EQU 0 (
    echo [OK] MT5 Bridge is running
) else (
    echo [!] MT5 Bridge not detected - start mt5_bridge.exe first
)

REM Check if server is up
curl -s http://localhost:8000/health >nul 2>nul
if %ERRORLEVEL% EQU 0 (
    echo [OK] Friday server is running
) else (
    echo [!] Friday server not detected - start friday.exe first
)

echo.
echo   Opening status endpoints...
echo.
echo   Combined Status: http://localhost:8000/control-center/status
echo   Exness Bot:      http://localhost:8000/exness/status
echo   Blue Guardian:   http://localhost:8000/blue-guardian/status
echo   Miner:           http://localhost:8000/miner/status
echo.
echo   Start Exness bot:   curl -X POST http://localhost:8000/exness/start
echo   Stop Exness bot:    curl -X POST http://localhost:8000/exness/stop
echo   Start Miner:        curl -X POST http://localhost:8000/miner/start
echo   Stop Miner:         curl -X POST http://localhost:8000/miner/stop
echo.
echo   Record BG trade:    curl -X POST http://localhost:8000/blue-guardian/record-trade ^
echo                        -H "Content-Type: application/json" ^
echo                        -d "{\"won\":true,\"pnl\":25.0}"
echo.
pause

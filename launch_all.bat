@echo off
TITLE Friday Unified Launcher - Exness + Blue Guardian + Miner
cd /d "%~dp0"

echo ===================================================
echo   FRIDAY BOT LAUNCHER
echo   Exness MT5 + Blue Guardian $50K + XMRig Miner
echo ===================================================
echo.

REM Check prerequisites
echo [1/4] Checking MT5 Bridge...
where mt5_bridge.exe >nul 2>nul
if %ERRORLEVEL% NEQ 0 (
    echo   Building MT5 bridge...
    cd go
    go build -o ../mt5_bridge.exe ./cmd/mt5_bridge/
    cd ..
)
echo   OK - mt5_bridge.exe ready

echo [2/4] Building unified server...
cd go
go build -o ../friday.exe ./cmd/
cd ..
echo   OK - friday.exe ready

echo.
echo ===================================================
echo   LAUNCH SEQUENCE
echo ===================================================
echo.

REM Start MT5 bridge in background
echo [3/4] Starting MT5 bridge on port 8001...
start "MT5 Bridge" cmd /c "mt5_bridge.exe"
timeout /t 2 /nobreak >nul
echo   OK

REM Start unified server
echo [4/4] Starting Friday server...
start "Friday Server" cmd /c "friday.exe"
timeout /t 2 /nobreak >nul
echo   OK

REM Start desktop control center
echo.
echo Starting Desktop Control Center...
start "Friday Control Center" cmd /c "friday_desktop.exe"
echo.

echo ===================================================
echo   SYSTEM IS RUNNING
echo ===================================================
echo.
echo   MT5 Bridge:    http://localhost:8001
echo   Friday Server: http://localhost:8000
echo   API Bridge:    http://localhost:9001
echo.
echo   BOTS:
echo     1. Exness Bot - EURUSD M1 (BB-RSI + London ORB)
echo     2. Blue Guardian $50K - Prop firm rules
echo     3. XMRig Miner - Passive Monero mining
echo.
echo   CONTROL CENTER:
echo     - Desktop systray: Shows live status
echo     - API: http://localhost:8000/control-center/status
echo     - Exness: http://localhost:8000/exness/status
echo     - Blue Guardian: http://localhost:8000/blue-guardian/status
echo     - Miner: http://localhost:8000/miner/status
echo.
echo   START TRADING:
echo     curl -X POST http://localhost:8000/exness/start
echo     curl -X POST http://localhost:8000/miner/start
echo.
echo ===================================================
pause

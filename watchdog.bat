@echo off
title Friday Watchdog
color 0D
setlocal enabledelayedexpansion

SET PROJECT_ROOT=D:\Friday - Prototype
SET FRIDAY_GO=%PROJECT_ROOT%\go
SET FRIDAY_EXE=%FRIDAY_GO%\friday.exe
SET WEB_UI=http://localhost:8000
SET CRASH_LOG=%FRIDAY_GO%\crash.log
SET PID_FILE=%FRIDAY_GO%\friday.pid
SET MAX_RESTART_DELAY=30

cls
echo.
echo   ╔══════════════════════════════════════════════════════════╗
echo   ║                                                          ║
echo   ║          FRIDAY WATCHDOG — Immortality Mode              ║
echo   ║                                                          ║
echo   ╚══════════════════════════════════════════════════════════╝
echo.
echo   Auto-restart enabled. I will never die.
echo   Close this window to stop permanently.
echo.

:BUILD
if not exist "%FRIDAY_EXE%" (
    echo [%DATE% %TIME%] Building friday.exe...
    cd /d "%FRIDAY_GO%"
    go build -ldflags="-s -w" -o friday.exe ./cmd/friday/
    if errorlevel 1 (
        echo [%DATE% %TIME%] BUILD FAILED — retrying in 30s
        timeout /t 30 /nobreak >nul
        goto BUILD
    )
    echo [%DATE% %TIME%] Build OK
)

:RUN
echo [%DATE% %TIME%] Starting Friday...
start "Friday" /min cmd /c "cd /d "%FRIDAY_GO%" && friday.exe"
set FRIDAY_PID=
for /f "tokens=2" %%a in ('tasklist /fi "imagename eq friday.exe" /nh 2^>nul') do set FRIDAY_PID=%%a

if "%FRIDAY_PID%"=="" (
    echo [%DATE% %TIME%] WARNING: Could not detect PID
) else (
    echo [%DATE% %TIME%] PID: %FRIDAY_PID%
    echo %FRIDAY_PID% > "%PID_FILE%"
)

rem Wait for server to come up
set WAIT_SEC=0
:WAIT_LOOP
timeout /t 2 /nobreak >nul
set /a WAIT_SEC+=2

curl -s -f http://localhost:8000/health >nul 2>&1
if errorlevel 1 (
    if !WAIT_SEC! GEQ 30 (
        echo [%DATE% %TIME%] Server failed to start within 30s
        goto CRASHED
    )
    goto WAIT_LOOP
)

echo [%DATE% %TIME%] Friday is running — health check OK
start /min "" "%WEB_UI%"

rem Monitor loop
:MONITOR
timeout /t 10 /nobreak >nul

rem Check if process is still running
tasklist /fi "pid eq %FRIDAY_PID%" 2>nul | find "%FRIDAY_PID%" >nul
if errorlevel 1 (
    rem Try to find by name
    tasklist /fi "imagename eq friday.exe" 2>nul | find "friday.exe" >nul
    if errorlevel 1 goto CRASHED
    rem Update PID
    for /f "tokens=2" %%a in ('tasklist /fi "imagename eq friday.exe" /nh 2^>nul') do set FRIDAY_PID=%%a
    echo %FRIDAY_PID% > "%PID_FILE%"
)

rem Health check
curl -s -f http://localhost:8000/health >nul 2>&1
if errorlevel 1 (
    echo [%DATE% %TIME%] Health check FAILED
    goto CRASHED
)

goto MONITOR

:CRASHED
echo [%DATE% %TIME%] Friday CRASHED or stopped — restarting...
echo [%DATE% %TIME%] CRASH >> "%CRASH_LOG%"

rem Kill any stale process
taskkill /f /im friday.exe 2>nul

rem Exponential backoff for restart (1s → 30s max)
if not defined DELAY set DELAY=1
echo [%DATE% %TIME%] Restart delay: %DELAY%s
timeout /t %DELAY% /nobreak >nul
set /a DELAY*=2
if %DELAY% GTR %MAX_RESTART_DELAY% set DELAY=%MAX_RESTART_DELAY%

goto BUILD

rem Never reaches here


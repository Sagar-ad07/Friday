@echo off
title Friday Launcher
color 0D
setlocal enabledelayedexpansion

SET PROJECT_ROOT=D:\Friday - Prototype
SET FRIDAY_GO=%PROJECT_ROOT%\friday_go
SET FRIDAY_EXE=%FRIDAY_GO%\friday.exe
SET WEB_UI=http://localhost:8000

cls
echo.
echo   ╔══════════════════════════════════════════════════════════╗
echo   ║                                                          ║
echo   ║             FRIDAY  v2.0  —  Unified                    ║
echo   ║                                                          ║
echo   ╚══════════════════════════════════════════════════════════╝
echo.
echo   [1] Launch Friday (single instance)
echo   [2] Launch with Watchdog (immortality mode — auto-restart)
echo   [3] Build only
echo   [Q] Quit
echo.
set /p CHOICE="Select: "

if /i "%CHOICE%"=="1" goto SINGLE
if /i "%CHOICE%"=="2" goto WATCHDOG
if /i "%CHOICE%"=="3" goto BUILD
if /i "%CHOICE%"=="Q" exit /b
if /i "%CHOICE%"=="q" exit /b
goto SINGLE

:BUILD
if not exist "%FRIDAY_EXE%" (
    cd /d "%FRIDAY_GO%"
    go build -ldflags="-s -w" -o friday.exe ./cmd/friday/
    if errorlevel 1 (
        echo [ERROR] Build failed
        pause
        exit /b 1
    )
    echo [OK] Built — %FRIDAY_EXE%
) else (
    echo [OK] Binary already exists
)
pause
exit /b

:WATCHDOG
echo [START] Watchdog activated — Friday will never die
echo.
start "Friday Watchdog" /min cmd /c "cd /d "%PROJECT_ROOT%" && watchdog.bat"
timeout /t 3 /nobreak >nul
:WAIT_W
timeout /t 2 /nobreak >nul
curl -s -f http://localhost:8000/health >nul 2>&1
if errorlevel 1 goto WAIT_W
start "" "%WEB_UI%"
echo [OK] Friday running under watchdog
echo        Close watchdog window to stop permanently
pause
exit /b

:SINGLE
if not exist "%FRIDAY_EXE%" (
    echo [BUILD] Compiling...
    cd /d "%FRIDAY_GO%"
    go build -ldflags="-s -w" -o friday.exe ./cmd/friday/
    if errorlevel 1 (
        echo [ERROR] Build failed
        pause
        exit /b 1
    )
    echo [OK] Built
)
echo [START] Launching Friday...
start "Friday" /min cmd /c "cd /d "%FRIDAY_GO%" && friday.exe"
:WAIT
timeout /t 2 /nobreak >nul
curl -s -f http://localhost:8000/health >nul 2>&1
if errorlevel 1 goto WAIT
echo [OK] Friday is running at %WEB_UI%
start "" "%WEB_UI%"
echo.
echo   Press Ctrl+C in "Friday" console to stop
pause
exit /b

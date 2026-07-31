@echo off
title Friday Watchdog
cd /d "D:\Friday - Prototype"
echo Starting Friday Watchdog (hidden)...
start /B powershell -ExecutionPolicy Bypass -File "D:\Friday - Prototype\watchdog.ps1"
echo Watchdog running in background. Check watchdog.log for status.
echo.
echo Commands:
echo   tasklist /fi "imagename eq friday.exe"    - see if Friday is running
echo   taskkill /f /im friday.exe                - stop Friday (watchdog will restart)
echo.
echo To stop watchdog: taskkill /f /im powershell.exe /fi "WINDOWTITLE eq Friday Watchdog"
echo.
pause

@echo off
title MT5 Auto-Restart
cd /d "C:\Program Files\MetaTrader 5"
:loop
tasklist /fi "imagename eq terminal64.exe" 2>nul | find /i "terminal64.exe" >nul
if errorlevel 1 (
    echo MT5 not running. Starting...
    start "" "C:\Program Files\MetaTrader 5\terminal64.exe"
) else (
    echo MT5 is running.
)
timeout /t 30 /nobreak >nul
goto loop

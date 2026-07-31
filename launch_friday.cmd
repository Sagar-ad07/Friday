@echo off
title Friday - AI Desktop Assistant
cd /d "D:\Friday - Prototype\go"
echo Starting Friday v2.0.0...
echo.
friday.exe
if errorlevel 1 (
    echo.
    echo Friday exited with error code %errorlevel%
    echo Press any key to close this window.
    pause >nul
)

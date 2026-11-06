@echo off
chcp 65001 >nul
cd /d "%~dp0"
if not exist "%~dp0dist\llm-launcher-windows-amd64.exe" (
    echo Missing dist\llm-launcher-windows-amd64.exe.
    echo Build with PowerShell: .\scripts\build.ps1
    pause
    exit /b 1
)
"%~dp0dist\llm-launcher-windows-amd64.exe" wizard --dir "%~dp0my-model" %*
set "launcher_exit=%ERRORLEVEL%"
echo.
if not "%launcher_exit%"=="0" echo Launcher exited with code %launcher_exit%.
pause
exit /b %launcher_exit%

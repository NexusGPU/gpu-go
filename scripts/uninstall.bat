@echo off
setlocal enabledelayedexpansion

:: GPU Go (ggo) Uninstallation Script for Windows (Batch)
:: Usage: uninstall.bat
::
:: Environment variables:
::   - GGO_INSTALL_DIR: Installation directory (default: %ProgramFiles%\ggo)
::   - GGO_KEEP_CONFIG: Set to 1 to keep configuration files
::
:: This script will:
::   - Stop and remove the scheduled task (auto-start service)
::   - Remove the ggo binary
::   - Clean up config directories
::   - Remove from PATH

:: --- Configuration ---
set "BINARY_NAME=ggo"
set "TASK_NAME=GpuGoAgent"

:: Default values
if not defined GGO_INSTALL_DIR set "GGO_INSTALL_DIR=%ProgramFiles%\ggo"

:: --- Check for admin privileges ---
net session >nul 2>&1
if %errorlevel% neq 0 (
    echo [ERROR] This script requires administrator privileges.
    echo Please run Command Prompt as Administrator.
    exit /b 1
)

echo.
echo ==========================================
echo   GPU Go ^(ggo^) Uninstaller for Windows
echo ==========================================
echo.

:: --- Stop running processes ---
echo [INFO] Stopping any running ggo processes...
taskkill /f /im %BINARY_NAME%.exe >nul 2>&1
timeout /t 1 /nobreak >nul

:: --- Remove scheduled task ---
echo [INFO] Checking for scheduled task...

schtasks /query /tn "%TASK_NAME%" >nul 2>&1
if %errorlevel% equ 0 (
    echo [INFO] Stopping scheduled task...
    schtasks /end /tn "%TASK_NAME%" >nul 2>&1
    timeout /t 2 /nobreak >nul
    
    echo [INFO] Removing scheduled task '%TASK_NAME%'...
    schtasks /delete /tn "%TASK_NAME%" /f >nul 2>&1
    
    if %errorlevel% equ 0 (
        echo [INFO] Scheduled task removed!
    ) else (
        echo [WARN] Failed to remove scheduled task
    )
) else (
    echo [INFO] Scheduled task not found, skipping...
)

:: --- Remove binary ---
set "BINARY_PATH=%GGO_INSTALL_DIR%\%BINARY_NAME%.exe"

if not exist "%BINARY_PATH%" (
    echo [INFO] Binary not found at %BINARY_PATH%, checking common locations...
    
    :: Check common locations
    if exist "%ProgramFiles%\ggo\%BINARY_NAME%.exe" (
        set "BINARY_PATH=%ProgramFiles%\ggo\%BINARY_NAME%.exe"
        set "GGO_INSTALL_DIR=%ProgramFiles%\ggo"
        echo [INFO] Found binary at !BINARY_PATH!
    ) else if exist "%LOCALAPPDATA%\Programs\ggo\%BINARY_NAME%.exe" (
        set "BINARY_PATH=%LOCALAPPDATA%\Programs\ggo\%BINARY_NAME%.exe"
        set "GGO_INSTALL_DIR=%LOCALAPPDATA%\Programs\ggo"
        echo [INFO] Found binary at !BINARY_PATH!
    ) else if exist "%USERPROFILE%\ggo\%BINARY_NAME%.exe" (
        set "BINARY_PATH=%USERPROFILE%\ggo\%BINARY_NAME%.exe"
        set "GGO_INSTALL_DIR=%USERPROFILE%\ggo"
        echo [INFO] Found binary at !BINARY_PATH!
    )
)

if exist "%BINARY_PATH%" (
    echo [INFO] Removing binary at %BINARY_PATH%...
    del /f /q "%BINARY_PATH%" >nul 2>&1
    
    if %errorlevel% equ 0 (
        echo [INFO] Binary removed!
    ) else (
        echo [WARN] Failed to remove binary
    )
    
    :: Remove install directory if empty
    dir /b "%GGO_INSTALL_DIR%" 2>nul | findstr . >nul
    if %errorlevel% neq 0 (
        echo [INFO] Removing empty install directory...
        rmdir /q "%GGO_INSTALL_DIR%" >nul 2>&1
    )
) else (
    echo [WARN] ggo binary not found
)

:: --- Remove from PATH ---
echo [INFO] Checking PATH...

:: Remove from system PATH using PowerShell (more reliable)
powershell -Command "$path = [Environment]::GetEnvironmentVariable('PATH', 'Machine'); if ($path -like '*%GGO_INSTALL_DIR%*') { $newPath = ($path.Split(';') | Where-Object { $_ -ne '%GGO_INSTALL_DIR%' -and $_ -ne '' }) -join ';'; [Environment]::SetEnvironmentVariable('PATH', $newPath, 'Machine'); Write-Host '[INFO] Removed from system PATH' } else { Write-Host '[INFO] Not in system PATH' }" 2>nul

:: Remove from user PATH using PowerShell
powershell -Command "$path = [Environment]::GetEnvironmentVariable('PATH', 'User'); if ($path -like '*%GGO_INSTALL_DIR%*') { $newPath = ($path.Split(';') | Where-Object { $_ -ne '%GGO_INSTALL_DIR%' -and $_ -ne '' }) -join ';'; [Environment]::SetEnvironmentVariable('PATH', $newPath, 'User'); Write-Host '[INFO] Removed from user PATH' } else { Write-Host '[INFO] Not in user PATH' }" 2>nul

:: --- Remove config directories ---
if defined GGO_KEEP_CONFIG (
    if "%GGO_KEEP_CONFIG%"=="1" (
        echo [INFO] Keeping configuration files as requested
        goto done
    )
)

echo [INFO] Removing configuration directories...

:: Remove user config directories
if exist "%APPDATA%\ggo" (
    echo [INFO] Removing %APPDATA%\ggo...
    rmdir /s /q "%APPDATA%\ggo" >nul 2>&1
)

if exist "%LOCALAPPDATA%\ggo" (
    echo [INFO] Removing %LOCALAPPDATA%\ggo...
    rmdir /s /q "%LOCALAPPDATA%\ggo" >nul 2>&1
)

if exist "%USERPROFILE%\.ggo" (
    echo [INFO] Removing %USERPROFILE%\.ggo...
    rmdir /s /q "%USERPROFILE%\.ggo" >nul 2>&1
)

if exist "%USERPROFILE%\.config\ggo" (
    echo [INFO] Removing %USERPROFILE%\.config\ggo...
    rmdir /s /q "%USERPROFILE%\.config\ggo" >nul 2>&1
)

echo [INFO] Configuration directories removed!

:done
echo.
echo ==========================================
echo   Uninstallation Complete!
echo ==========================================
echo.
echo GPU Go ^(ggo^) has been removed from your system.
echo.
echo Please restart your terminal for PATH changes to take effect.
echo.

endlocal

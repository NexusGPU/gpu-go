@echo off
setlocal enabledelayedexpansion

:: GPU Go (ggo) Installation Script for Windows (Batch)
:: Usage: install.bat
::
:: Environment variables:
::   - GPU_GO_VERSION: Specific version to install (default: latest)
::   - GPU_GO_TOKEN: Agent registration token (will auto-register and setup auto-start service)
::   - GPU_GO_ENDPOINT: Custom API endpoint (optional, used with GPU_GO_TOKEN for agent registration)
::   - GGO_INSTALL_DIR: Installation directory (default: %ProgramFiles%\ggo)
::
:: Examples:
::   # Install latest version (client mode)
::   install.bat
::
::   # Install specific version
::   set GPU_GO_VERSION=v1.0.0
::   install.bat
::
::   # Agent mode: install, register, and setup auto-start service
::   set GPU_GO_TOKEN=your-token
::   install.bat
::
::   # Agent mode with custom endpoint
::   set GPU_GO_TOKEN=your-token
::   set GPU_GO_ENDPOINT=https://api.example.com
::   install.bat

:: --- Configuration ---
set "CDN_BASE_URL=https://cdn.tensor-fusion.ai/archive/gpugo"
set "BINARY_NAME=ggo"
set "TASK_NAME=GpuGoAgent"

:: Default values
if not defined GPU_GO_VERSION set "GPU_GO_VERSION=latest"
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
echo   GPU Go ^(ggo^) Installer for Windows
echo ==========================================
echo.

:: --- Detect architecture ---
if "%PROCESSOR_ARCHITECTURE%"=="AMD64" (
    set "ARCH=amd64"
) else if "%PROCESSOR_ARCHITECTURE%"=="ARM64" (
    set "ARCH=arm64"
) else (
    echo [ERROR] Unsupported architecture: %PROCESSOR_ARCHITECTURE%
    exit /b 1
)

echo [INFO] Detected architecture: %ARCH%
echo [INFO] Installing version: %GPU_GO_VERSION%

if defined GPU_GO_TOKEN (
    echo [INFO] Agent mode: will register and setup auto-start service after installation
)

if defined GPU_GO_ENDPOINT (
    echo [INFO] Using custom API endpoint: %GPU_GO_ENDPOINT%
)

:: --- Build download URL ---
set "DOWNLOAD_URL=%CDN_BASE_URL%/%GPU_GO_VERSION%/gpugo-windows-%ARCH%.exe"
set "CHECKSUM_URL=%DOWNLOAD_URL%.sha256"

echo [INFO] Download URL: %DOWNLOAD_URL%

:: --- Create temp directory ---
set "TEMP_DIR=%TEMP%\ggo-install-%RANDOM%"
mkdir "%TEMP_DIR%" 2>nul

set "TEMP_BINARY=%TEMP_DIR%\%BINARY_NAME%.exe"
set "TEMP_CHECKSUM=%TEMP_DIR%\%BINARY_NAME%.sha256"

:: --- Download binary ---
echo [INFO] Downloading binary...

:: Try PowerShell first (more reliable)
powershell -Command "[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12; Invoke-WebRequest -Uri '%DOWNLOAD_URL%' -OutFile '%TEMP_BINARY%' -UseBasicParsing" 2>nul
if %errorlevel% neq 0 (
    :: Fallback to curl if available
    where curl >nul 2>&1
    if %errorlevel% equ 0 (
        curl -fsSL -o "%TEMP_BINARY%" "%DOWNLOAD_URL%"
        if !errorlevel! neq 0 (
            echo [ERROR] Failed to download binary from %DOWNLOAD_URL%
            goto cleanup
        )
    ) else (
        echo [ERROR] Failed to download binary. Neither PowerShell nor curl available.
        goto cleanup
    )
)

:: --- Try to download checksum (optional) ---
echo [INFO] Downloading checksum...
powershell -Command "[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12; Invoke-WebRequest -Uri '%CHECKSUM_URL%' -OutFile '%TEMP_CHECKSUM%' -UseBasicParsing" 2>nul
if %errorlevel% equ 0 (
    :: Verify checksum
    for /f "tokens=1" %%a in (%TEMP_CHECKSUM%) do set "EXPECTED_HASH=%%a"
    for /f "skip=1 tokens=*" %%a in ('certutil -hashfile "%TEMP_BINARY%" SHA256') do (
        if not defined ACTUAL_HASH set "ACTUAL_HASH=%%a"
    )
    :: Remove spaces from hash
    set "ACTUAL_HASH=!ACTUAL_HASH: =!"
    
    if /i "!ACTUAL_HASH!"=="!EXPECTED_HASH!" (
        echo [INFO] Checksum verified
    ) else (
        echo [WARN] Checksum verification failed. Expected: !EXPECTED_HASH!, Got: !ACTUAL_HASH!
        echo [WARN] Continuing anyway...
    )
) else (
    echo [WARN] Checksum file not available, skipping verification
)

:: --- Create install directory ---
if not exist "%GGO_INSTALL_DIR%" (
    mkdir "%GGO_INSTALL_DIR%"
)

set "DEST_PATH=%GGO_INSTALL_DIR%\%BINARY_NAME%.exe"
echo [INFO] Installing to %DEST_PATH%

:: --- Stop any running processes ---
taskkill /f /im %BINARY_NAME%.exe >nul 2>&1

:: --- Stop scheduled task if running ---
schtasks /query /tn "%TASK_NAME%" >nul 2>&1
if %errorlevel% equ 0 (
    schtasks /end /tn "%TASK_NAME%" >nul 2>&1
)

:: --- Copy binary ---
copy /y "%TEMP_BINARY%" "%DEST_PATH%" >nul
if %errorlevel% neq 0 (
    echo [ERROR] Failed to copy binary to %DEST_PATH%
    goto cleanup
)

echo [INFO] Installation complete!

:: --- Add to PATH ---
echo %PATH% | findstr /i /c:"%GGO_INSTALL_DIR%" >nul
if %errorlevel% neq 0 (
    echo [INFO] Adding %GGO_INSTALL_DIR% to system PATH...
    setx PATH "%PATH%;%GGO_INSTALL_DIR%" /M >nul 2>&1
    if %errorlevel% equ 0 (
        echo [INFO] Added to system PATH. Please restart your terminal.
    ) else (
        echo [WARN] Failed to add to PATH. Please add manually: %GGO_INSTALL_DIR%
    )
)

echo.
echo [INFO] ggo has been installed to %DEST_PATH%
echo.

:: --- Show version ---
echo [INFO] Installed version:
"%DEST_PATH%" --version 2>nul

:: --- Agent mode ---
if defined GPU_GO_TOKEN (
    echo.
    echo ==========================================
    echo   Agent Mode Setup
    echo ==========================================
    
    :: Register agent
    echo.
    echo [INFO] Registering GPU Go agent...
    
    if defined GPU_GO_ENDPOINT (
        "%DEST_PATH%" agent register -t "%GPU_GO_TOKEN%" --server "%GPU_GO_ENDPOINT%"
    ) else (
        "%DEST_PATH%" agent register -t "%GPU_GO_TOKEN%"
    )
    
    if %errorlevel% neq 0 (
        echo [ERROR] Agent registration failed. Please check your token and try again.
        goto cleanup
    )
    
    echo [INFO] Agent registered successfully!
    
    :: Setup Windows scheduled task
    echo.
    echo [INFO] Setting up Windows scheduled task for auto-start...
    
    :: Remove existing task if exists
    schtasks /query /tn "%TASK_NAME%" >nul 2>&1
    if %errorlevel% equ 0 (
        echo [INFO] Removing existing scheduled task...
        schtasks /delete /tn "%TASK_NAME%" /f >nul 2>&1
    )
    
    :: Create scheduled task
    schtasks /create /tn "%TASK_NAME%" /tr "\"%DEST_PATH%\" agent start" /sc onstart /ru SYSTEM /rl HIGHEST /f >nul 2>&1
    if %errorlevel% neq 0 (
        echo [ERROR] Failed to create scheduled task
        goto cleanup
    )
    
    echo [INFO] Scheduled task '%TASK_NAME%' created successfully!
    
    :: Start the task immediately
    echo [INFO] Starting GPU Go agent...
    schtasks /run /tn "%TASK_NAME%" >nul 2>&1
    
    timeout /t 2 /nobreak >nul
    
    echo [INFO] Agent started!
    echo.
    echo [INFO] Service management commands ^(run as Administrator^):
    echo   schtasks /query /tn %TASK_NAME%         # Check status
    echo   schtasks /end /tn %TASK_NAME%           # Stop service
    echo   schtasks /run /tn %TASK_NAME%           # Start service
    echo   schtasks /delete /tn %TASK_NAME% /f     # Remove service
    
    echo.
    echo ==========================================
    echo   GPU Go Agent installation complete!
    echo ==========================================
    if defined GPU_GO_ENDPOINT (
        echo [INFO] API Endpoint: %GPU_GO_ENDPOINT%
    )
    echo [INFO] The agent is now running as a scheduled task.
    echo.
    goto cleanup
)

echo.
echo ==========================================
echo   Installation Successful!
echo ==========================================
echo.
echo Quick start:
echo   # Show help
echo   ggo --help
echo.
echo   # Register as agent ^(on GPU server^)
echo   ggo agent register --token ^<your-token^>
echo.
echo   # Use a shared GPU
echo   ggo use ^<short-code^>
echo.

:cleanup
:: Cleanup temp directory
if exist "%TEMP_DIR%" (
    rmdir /s /q "%TEMP_DIR%" 2>nul
)

endlocal

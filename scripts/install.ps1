#Requires -Version 5.1
<#
.SYNOPSIS
    GPU Go (ggo) Installation Script for Windows

.DESCRIPTION
    Downloads and installs the ggo binary for Windows.
    
    Usage:
    # Download and run directly (PowerShell):
    irm https://cdn.tensor-fusion.ai/archive/gpugo/install.ps1 | iex
    
    # Or with parameters:
    $env:GPU_GO_VERSION = "v1.0.0"
    irm https://cdn.tensor-fusion.ai/archive/gpugo/install.ps1 | iex

    # Agent mode: install, register, and setup auto-start service
    $env:GPU_GO_TOKEN = "your-token"
    irm https://cdn.tensor-fusion.ai/archive/gpugo/install.ps1 | iex

.PARAMETER Version
    Specific version to install (default: latest)

.PARAMETER InstallDir
    Installation directory (default: $env:ProgramFiles\ggo)

.PARAMETER Token
    Agent registration token (will auto-register and setup Windows service)

.PARAMETER Endpoint
    Custom API endpoint (optional, used with Token for agent registration)

.EXAMPLE
    # Install latest version (client mode)
    .\install.ps1

.EXAMPLE
    # Install specific version
    .\install.ps1 -Version v1.0.0

.EXAMPLE
    # Install to custom directory
    .\install.ps1 -InstallDir "C:\Tools\ggo"

.EXAMPLE
    # Agent mode: install, register, and setup auto-start
    .\install.ps1 -Token "your-token"

.EXAMPLE
    # Agent mode with custom endpoint
    .\install.ps1 -Token "your-token" -Endpoint "https://api.example.com"
#>

[CmdletBinding()]
param(
    [Parameter()]
    [string]$Version = $env:GPU_GO_VERSION,
    
    [Parameter()]
    [string]$InstallDir = $env:GGO_INSTALL_DIR,
    
    [Parameter()]
    [string]$Token = $env:GPU_GO_TOKEN,
    
    [Parameter()]
    [string]$Endpoint = $env:GPU_GO_ENDPOINT,
    
    [Parameter()]
    [bool]$AddToPath = $true
)

$ErrorActionPreference = "Stop"

# --- Configuration ---
$CDN_BASE_URL = "https://cdn.tensor-fusion.ai/archive/gpugo"
$BINARY_NAME = "ggo"
$SERVICE_NAME = "ggo-agent"
$TASK_NAME = "GpuGoAgent"

# Default install directory (use Program Files for system-wide install)
if (-not $InstallDir) {
    $InstallDir = Join-Path $env:ProgramFiles "ggo"
}

# Default version
if (-not $Version) {
    $Version = "latest"
}

# --- Helper functions ---
function Write-Info {
    param([string]$Message)
    Write-Host "[INFO] $Message" -ForegroundColor Cyan
}

function Write-Warn {
    param([string]$Message)
    Write-Host "[WARN] $Message" -ForegroundColor Yellow
}

function Write-Err {
    param([string]$Message)
    Write-Host "[ERROR] $Message" -ForegroundColor Red
}

function Test-Administrator {
    $currentUser = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($currentUser)
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Get-Architecture {
    $arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture
    switch ($arch) {
        "X64" { return "amd64" }
        "Arm64" { return "arm64" }
        default { 
            # Fallback for older PowerShell versions
            if ([Environment]::Is64BitOperatingSystem) {
                return "amd64"
            }
            throw "Unsupported architecture: $arch"
        }
    }
}

function Build-DownloadUrl {
    param(
        [string]$Arch,
        [string]$Ver
    )
    # CDN URL format: https://cdn.tensor-fusion.ai/archive/gpugo/{version}/gpugo-windows-{arch}.exe
    return "$CDN_BASE_URL/$Ver/gpugo-windows-$Arch.exe"
}

function Get-FileHash256 {
    param([string]$FilePath)
    $hash = Get-FileHash -Path $FilePath -Algorithm SHA256
    return $hash.Hash.ToLower()
}

function Test-Checksum {
    param(
        [string]$FilePath,
        [string]$ExpectedHash
    )
    
    $actualHash = Get-FileHash256 -FilePath $FilePath
    if ($actualHash -ne $ExpectedHash.ToLower()) {
        throw "Checksum verification failed. Expected: $ExpectedHash, Got: $actualHash"
    }
    Write-Info "Checksum verified"
}

function Add-ToSystemPath {
    param([string]$Directory)
    
    $currentPath = [Environment]::GetEnvironmentVariable("PATH", "Machine")
    if ($currentPath -notlike "*$Directory*") {
        $newPath = "$currentPath;$Directory"
        [Environment]::SetEnvironmentVariable("PATH", $newPath, "Machine")
        $env:PATH = "$env:PATH;$Directory"
        Write-Info "Added $Directory to system PATH"
        Write-Info "Please restart your terminal for PATH changes to take effect"
        return $true
    }
    return $false
}

function Register-Agent {
    param(
        [string]$BinaryPath,
        [string]$AgentToken
    )
    
    Write-Info ""
    Write-Info "Registering GPU Go agent..."
    
    $registerArgs = @("agent", "register", "-t", $AgentToken)
    
    if ($Endpoint) {
        $registerArgs += @("--server", $Endpoint)
    }
    
    Write-Info "Running: ggo agent register"
    
    $process = Start-Process -FilePath $BinaryPath -ArgumentList $registerArgs -Wait -NoNewWindow -PassThru
    
    if ($process.ExitCode -eq 0) {
        Write-Info "Agent registered successfully!"
        return $true
    } else {
        throw "Agent registration failed. Please check your token and try again."
    }
}

function Setup-WindowsService {
    param([string]$BinaryPath)
    
    Write-Info ""
    Write-Info "Setting up Windows scheduled task for auto-start..."
    
    # Remove existing task if exists
    $existingTask = Get-ScheduledTask -TaskName $TASK_NAME -ErrorAction SilentlyContinue
    if ($existingTask) {
        Write-Info "Removing existing scheduled task..."
        Unregister-ScheduledTask -TaskName $TASK_NAME -Confirm:$false
    }
    
    # Create scheduled task for auto-start at system startup
    $action = New-ScheduledTaskAction -Execute $BinaryPath -Argument "agent start"
    
    # Trigger at system startup
    $trigger = New-ScheduledTaskTrigger -AtStartup
    
    # Run as SYSTEM with highest privileges
    $principal = New-ScheduledTaskPrincipal -UserId "SYSTEM" -LogonType ServiceAccount -RunLevel Highest
    
    # Settings
    $settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -StartWhenAvailable -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1)
    
    # Register the task
    Register-ScheduledTask -TaskName $TASK_NAME -Action $action -Trigger $trigger -Principal $principal -Settings $settings -Description "GPU Go Agent - GPU Sharing Service" | Out-Null
    
    Write-Info "Scheduled task '$TASK_NAME' created successfully!"
    
    # Start the task immediately
    Write-Info "Starting GPU Go agent..."
    Start-ScheduledTask -TaskName $TASK_NAME
    
    Start-Sleep -Seconds 2
    
    $taskInfo = Get-ScheduledTask -TaskName $TASK_NAME
    if ($taskInfo.State -eq "Running") {
        Write-Info "Agent started successfully!"
    } else {
        Write-Warn "Agent may not have started. Check task status with: Get-ScheduledTask -TaskName $TASK_NAME"
    }
    
    Write-Info ""
    Write-Info "Service management commands (PowerShell as Administrator):"
    Write-Info "  Get-ScheduledTask -TaskName $TASK_NAME           # Check status"
    Write-Info "  Stop-ScheduledTask -TaskName $TASK_NAME          # Stop service"
    Write-Info "  Start-ScheduledTask -TaskName $TASK_NAME         # Start service"
    Write-Info "  Unregister-ScheduledTask -TaskName $TASK_NAME    # Remove service"
    
    return $true
}

function Install-Ggo {
    Write-Host ""
    Write-Host "==========================================" -ForegroundColor Green
    Write-Host "  GPU Go (ggo) Installer for Windows" -ForegroundColor Green
    Write-Host "==========================================" -ForegroundColor Green
    Write-Host ""
    
    # Check for administrator privileges (required for onboarding)
    if (-not (Test-Administrator)) {
        throw "Administrator privileges required. Please run PowerShell as Administrator."
    }
    
    # Detect architecture
    $arch = Get-Architecture
    Write-Info "Detected architecture: $arch"
    Write-Info "Installing version: $Version"
    
    if ($Token) {
        Write-Info "Agent mode: will register and setup auto-start service after installation"
    }
    
    if ($Endpoint) {
        Write-Info "Using custom API endpoint: $Endpoint"
    }
    
    # Construct download URL from CDN
    $downloadUrl = Build-DownloadUrl -Arch $arch -Ver $Version
    $checksumUrl = "$downloadUrl.sha256"
    
    Write-Info "Download URL: $downloadUrl"
    
    # Create temp directory
    $tempDir = Join-Path $env:TEMP "ggo-install-$(Get-Random)"
    New-Item -ItemType Directory -Path $tempDir -Force | Out-Null
    
    try {
        $tempBinary = Join-Path $tempDir "$BINARY_NAME.exe"
        $tempChecksum = Join-Path $tempDir "$BINARY_NAME.sha256"
        
        # Download binary
        Write-Info "Downloading binary..."
        [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
        Invoke-WebRequest -Uri $downloadUrl -OutFile $tempBinary -UseBasicParsing
        
        # Try to download and verify checksum (optional)
        try {
            Invoke-WebRequest -Uri $checksumUrl -OutFile $tempChecksum -UseBasicParsing -ErrorAction Stop
            $expectedChecksum = (Get-Content $tempChecksum -Raw).Split()[0]
            Test-Checksum -FilePath $tempBinary -ExpectedHash $expectedChecksum
        }
        catch {
            Write-Warn "Checksum file not available, skipping verification"
        }
        
        # Create install directory
        if (-not (Test-Path $InstallDir)) {
            New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
        }
        
        # Install binary
        $destPath = Join-Path $InstallDir "$BINARY_NAME.exe"
        Write-Info "Installing to $destPath"
        
        # Stop any running ggo processes
        Get-Process -Name $BINARY_NAME -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
        
        # Stop scheduled task if running
        $existingTask = Get-ScheduledTask -TaskName $TASK_NAME -ErrorAction SilentlyContinue
        if ($existingTask -and $existingTask.State -eq "Running") {
            Stop-ScheduledTask -TaskName $TASK_NAME -ErrorAction SilentlyContinue
        }
        
        # Copy binary
        Copy-Item -Path $tempBinary -Destination $destPath -Force
        
        Write-Info "Installation complete!"
        
        # Add to PATH (system-wide if admin, user otherwise)
        if ($AddToPath) {
            if (Test-Administrator) {
                Add-ToSystemPath -Directory $InstallDir
            } else {
                $currentPath = [Environment]::GetEnvironmentVariable("PATH", "User")
                if ($currentPath -notlike "*$InstallDir*") {
                    $newPath = "$currentPath;$InstallDir"
                    [Environment]::SetEnvironmentVariable("PATH", $newPath, "User")
                    $env:PATH = "$env:PATH;$InstallDir"
                    Write-Info "Added $InstallDir to user PATH"
                }
            }
        }
        
        Write-Host ""
        Write-Info "ggo has been installed to $destPath"
        
        # Show version
        Write-Host ""
        Write-Info "Installed version:"
        try {
            & $destPath --version
        }
        catch {
            # Ignore version check errors
        }
        
        # Agent mode: register and setup service
        if ($Token) {
            Write-Host ""
            Write-Host "==========================================" -ForegroundColor Yellow
            Write-Host "  Agent Mode Setup" -ForegroundColor Yellow
            Write-Host "==========================================" -ForegroundColor Yellow
            
            # Register agent
            Register-Agent -BinaryPath $destPath -AgentToken $Token
            
            # Setup Windows service
            Setup-WindowsService -BinaryPath $destPath
            
            Write-Host ""
            Write-Host "==========================================" -ForegroundColor Green
            Write-Host "  GPU Go Agent installation complete!" -ForegroundColor Green
            Write-Host "==========================================" -ForegroundColor Green
            if ($Endpoint) {
                Write-Info "API Endpoint: $Endpoint"
            }
            Write-Info "The agent is now running as a scheduled task."
            Write-Host ""
            return
        }
        
        Write-Host ""
        Write-Host "==========================================" -ForegroundColor Green
        Write-Host "  Installation Successful!" -ForegroundColor Green
        Write-Host "==========================================" -ForegroundColor Green
        Write-Host ""
        Write-Host "Quick start:" -ForegroundColor Yellow
        Write-Host "  # Show help" -ForegroundColor Gray
        Write-Host "  ggo --help" -ForegroundColor White
        Write-Host ""
        Write-Host "  # Register as agent (on GPU server)" -ForegroundColor Gray
        Write-Host "  ggo agent register --token <your-token>" -ForegroundColor White
        Write-Host ""
        Write-Host "  # Use a shared GPU" -ForegroundColor Gray
        Write-Host "  ggo use <short-link>" -ForegroundColor White
        Write-Host ""
    }
    finally {
        # Cleanup temp directory
        if (Test-Path $tempDir) {
            Remove-Item -Path $tempDir -Recurse -Force -ErrorAction SilentlyContinue
        }
    }
}

# --- Main ---
try {
    Install-Ggo
}
catch {
    Write-Err $_.Exception.Message
    $global:LASTEXITCODE = 1
    return
}

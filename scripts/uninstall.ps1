#Requires -Version 5.1
<#
.SYNOPSIS
    GPU Go (ggo) Uninstallation Script for Windows

.DESCRIPTION
    Removes the ggo binary and associated services from Windows.
    
    Usage:
    # Download and run directly (PowerShell as Administrator):
    irm https://cdn.tensor-fusion.ai/archive/gpugo/uninstall.ps1 | iex

    This script will:
    - Stop and remove the scheduled task (auto-start service)
    - Remove the ggo binary
    - Clean up config directories
    - Optionally remove from PATH

.PARAMETER InstallDir
    Installation directory (default: $env:ProgramFiles\ggo)

.PARAMETER RemoveFromPath
    Whether to remove installation directory from PATH (default: true)

.PARAMETER KeepConfig
    Whether to keep configuration files (default: false)

.EXAMPLE
    # Full uninstall
    .\uninstall.ps1

.EXAMPLE
    # Keep configuration files
    .\uninstall.ps1 -KeepConfig $true

.EXAMPLE
    # Custom install directory
    .\uninstall.ps1 -InstallDir "C:\Tools\ggo"
#>

[CmdletBinding()]
param(
    [Parameter()]
    [string]$InstallDir = $env:GGO_INSTALL_DIR,
    
    [Parameter()]
    [bool]$RemoveFromPath = $true,
    
    [Parameter()]
    [bool]$KeepConfig = $false
)

$ErrorActionPreference = "Stop"

# --- Configuration ---
$BINARY_NAME = "ggo"
$TASK_NAME = "GpuGoAgent"

# Default install directory
if (-not $InstallDir) {
    $InstallDir = Join-Path $env:ProgramFiles "ggo"
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

function Stop-GgoProcesses {
    Write-Info "Stopping any running ggo processes..."
    
    Get-Process -Name $BINARY_NAME -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
    
    # Give processes time to terminate
    Start-Sleep -Seconds 1
}

function Remove-ScheduledTask {
    Write-Info "Checking for scheduled task..."
    
    $task = Get-ScheduledTask -TaskName $TASK_NAME -ErrorAction SilentlyContinue
    
    if (-not $task) {
        Write-Info "Scheduled task not found, skipping..."
        return
    }
    
    # Stop the task if running
    if ($task.State -eq "Running") {
        Write-Info "Stopping scheduled task..."
        Stop-ScheduledTask -TaskName $TASK_NAME -ErrorAction SilentlyContinue
        Start-Sleep -Seconds 2
    }
    
    # Remove the task
    Write-Info "Removing scheduled task '$TASK_NAME'..."
    Unregister-ScheduledTask -TaskName $TASK_NAME -Confirm:$false -ErrorAction SilentlyContinue
    
    Write-Info "Scheduled task removed!"
}

function Remove-Binary {
    $binaryPath = Join-Path $InstallDir "$BINARY_NAME.exe"
    
    # Check default location
    if (-not (Test-Path $binaryPath)) {
        Write-Info "Binary not found at $binaryPath, checking common locations..."
        
        # Check common locations
        $commonPaths = @(
            (Join-Path $env:ProgramFiles "ggo\$BINARY_NAME.exe"),
            (Join-Path $env:LOCALAPPDATA "Programs\ggo\$BINARY_NAME.exe"),
            (Join-Path $env:USERPROFILE "ggo\$BINARY_NAME.exe")
        )
        
        foreach ($path in $commonPaths) {
            if (Test-Path $path) {
                $binaryPath = $path
                $InstallDir = Split-Path $path -Parent
                Write-Info "Found binary at $binaryPath"
                break
            }
        }
    }
    
    if (-not (Test-Path $binaryPath)) {
        Write-Warn "ggo binary not found"
        return
    }
    
    Write-Info "Removing binary at $binaryPath..."
    Remove-Item -Path $binaryPath -Force -ErrorAction SilentlyContinue
    
    # Remove install directory if empty
    if (Test-Path $InstallDir) {
        $items = Get-ChildItem -Path $InstallDir -ErrorAction SilentlyContinue
        if (-not $items -or $items.Count -eq 0) {
            Write-Info "Removing empty install directory..."
            Remove-Item -Path $InstallDir -Force -ErrorAction SilentlyContinue
        }
    }
    
    Write-Info "Binary removed!"
}

function Remove-FromPath {
    if (-not $RemoveFromPath) {
        return
    }
    
    Write-Info "Checking PATH..."
    
    # Check system PATH
    $systemPath = [Environment]::GetEnvironmentVariable("PATH", "Machine")
    if ($systemPath -like "*$InstallDir*") {
        Write-Info "Removing $InstallDir from system PATH..."
        $newPath = ($systemPath.Split(';') | Where-Object { $_ -ne $InstallDir -and $_ -ne "" }) -join ';'
        [Environment]::SetEnvironmentVariable("PATH", $newPath, "Machine")
        Write-Info "Removed from system PATH"
    }
    
    # Check user PATH
    $userPath = [Environment]::GetEnvironmentVariable("PATH", "User")
    if ($userPath -like "*$InstallDir*") {
        Write-Info "Removing $InstallDir from user PATH..."
        $newPath = ($userPath.Split(';') | Where-Object { $_ -ne $InstallDir -and $_ -ne "" }) -join ';'
        [Environment]::SetEnvironmentVariable("PATH", $newPath, "User")
        Write-Info "Removed from user PATH"
    }
}

function Remove-ConfigDirectories {
    if ($KeepConfig) {
        Write-Info "Keeping configuration files as requested"
        return
    }
    
    Write-Info "Removing configuration directories..."
    
    $configDirs = @(
        (Join-Path $env:APPDATA "ggo"),
        (Join-Path $env:LOCALAPPDATA "ggo"),
        (Join-Path $env:USERPROFILE ".ggo"),
        (Join-Path $env:USERPROFILE ".gpugo"),
        (Join-Path $env:USERPROFILE ".config\ggo")
    )
    
    foreach ($dir in $configDirs) {
        if (Test-Path $dir) {
            Write-Info "Removing $dir..."
            Remove-Item -Path $dir -Recurse -Force -ErrorAction SilentlyContinue
        }
    }
    
    Write-Info "Configuration directories removed!"
}

function Uninstall-Ggo {
    Write-Host ""
    Write-Host "==========================================" -ForegroundColor Red
    Write-Host "  GPU Go (ggo) Uninstaller for Windows" -ForegroundColor Red
    Write-Host "==========================================" -ForegroundColor Red
    Write-Host ""
    
    # Check for administrator privileges
    if (-not (Test-Administrator)) {
        throw "Administrator privileges required. Please run PowerShell as Administrator."
    }
    
    # Stop running processes
    Stop-GgoProcesses
    
    # Remove scheduled task
    Remove-ScheduledTask
    
    # Remove binary
    Remove-Binary
    
    # Remove from PATH
    Remove-FromPath
    
    # Remove config directories
    Remove-ConfigDirectories
    
    Write-Host ""
    Write-Host "==========================================" -ForegroundColor Green
    Write-Host "  Uninstallation Complete!" -ForegroundColor Green
    Write-Host "==========================================" -ForegroundColor Green
    Write-Host ""
    Write-Host "GPU Go (ggo) has been removed from your system." -ForegroundColor White
    Write-Host ""
    Write-Host "Please restart your terminal for PATH changes to take effect." -ForegroundColor Yellow
    Write-Host ""
}

# --- Main ---
try {
    Uninstall-Ggo
}
catch {
    Write-Err $_.Exception.Message
    $global:LASTEXITCODE = 1
    return
}

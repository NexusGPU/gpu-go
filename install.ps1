#Requires -Version 5.1
<#
.SYNOPSIS
    GPU Go (ggo) Installation Script for Windows

.DESCRIPTION
    Downloads and installs the ggo binary for Windows.
    
    Usage:
    # Download and run directly:
    iwr -useb https://get.tensor-fusion.ai/install.ps1 | iex
    
    # Or with parameters:
    $env:GGO_VERSION = "v1.0.0"
    iwr -useb https://get.tensor-fusion.ai/install.ps1 | iex

.PARAMETER Version
    Specific version to install (default: latest)

.PARAMETER InstallDir
    Installation directory (default: $env:LOCALAPPDATA\Programs\ggo)

.PARAMETER AddToPath
    Whether to add installation directory to PATH (default: true)

.EXAMPLE
    # Install latest version
    .\install.ps1

.EXAMPLE
    # Install specific version
    .\install.ps1 -Version v1.0.0

.EXAMPLE
    # Install to custom directory
    .\install.ps1 -InstallDir "C:\Tools\ggo"
#>

[CmdletBinding()]
param(
    [Parameter()]
    [string]$Version = $env:GGO_VERSION,
    
    [Parameter()]
    [string]$InstallDir = $env:GGO_INSTALL_DIR,
    
    [Parameter()]
    [bool]$AddToPath = $true
)

$ErrorActionPreference = "Stop"

# Configuration
$GitHubRepo = "NexusGPU/gpu-go"
$BinaryName = "ggo"

# Default install directory
if (-not $InstallDir) {
    $InstallDir = Join-Path $env:LOCALAPPDATA "Programs\ggo"
}

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

function Get-LatestVersion {
    Write-Info "Fetching latest version..."
    try {
        $response = Invoke-RestMethod -Uri "https://api.github.com/repos/$GitHubRepo/releases/latest" -UseBasicParsing
        return $response.tag_name
    }
    catch {
        throw "Failed to get latest version: $_"
    }
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

function Add-ToUserPath {
    param([string]$Directory)
    
    $currentPath = [Environment]::GetEnvironmentVariable("PATH", "User")
    if ($currentPath -notlike "*$Directory*") {
        $newPath = "$currentPath;$Directory"
        [Environment]::SetEnvironmentVariable("PATH", $newPath, "User")
        $env:PATH = "$env:PATH;$Directory"
        Write-Info "Added $Directory to user PATH"
        Write-Info "Please restart your terminal for PATH changes to take effect"
    }
}

function Install-Ggo {
    Write-Host ""
    Write-Host "========================================" -ForegroundColor Green
    Write-Host "  GPU Go (ggo) Installer for Windows" -ForegroundColor Green
    Write-Host "========================================" -ForegroundColor Green
    Write-Host ""
    
    # Detect architecture
    $arch = Get-Architecture
    Write-Info "Detected architecture: $arch"
    
    # Get version
    if (-not $Version) {
        $Version = Get-LatestVersion
    }
    Write-Info "Installing version: $Version"
    
    # Construct URLs
    $binaryFilename = "$BinaryName-windows-$arch.exe"
    $downloadUrl = "https://github.com/$GitHubRepo/releases/download/$Version/$binaryFilename"
    $checksumUrl = "$downloadUrl.sha256"
    
    # Create temp directory
    $tempDir = Join-Path $env:TEMP "ggo-install-$(Get-Random)"
    New-Item -ItemType Directory -Path $tempDir -Force | Out-Null
    
    try {
        $tempBinary = Join-Path $tempDir $binaryFilename
        $tempChecksum = Join-Path $tempDir "$binaryFilename.sha256"
        
        # Download binary
        Write-Info "Downloading $downloadUrl"
        [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
        Invoke-WebRequest -Uri $downloadUrl -OutFile $tempBinary -UseBasicParsing
        
        # Download checksum
        Write-Info "Downloading checksum..."
        Invoke-WebRequest -Uri $checksumUrl -OutFile $tempChecksum -UseBasicParsing
        
        # Verify checksum
        $expectedChecksum = (Get-Content $tempChecksum -Raw).Split()[0]
        Test-Checksum -FilePath $tempBinary -ExpectedHash $expectedChecksum
        
        # Create install directory
        if (-not (Test-Path $InstallDir)) {
            New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
        }
        
        # Install binary
        $destPath = Join-Path $InstallDir "$BinaryName.exe"
        Write-Info "Installing to $destPath"
        
        # Stop any running ggo processes
        Get-Process -Name $BinaryName -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
        
        # Copy binary
        Copy-Item -Path $tempBinary -Destination $destPath -Force
        
        Write-Info "Installation complete!"
        
        # Add to PATH
        if ($AddToPath) {
            Add-ToUserPath -Directory $InstallDir
        }
        
        Write-Host ""
        Write-Host "========================================" -ForegroundColor Green
        Write-Host "  Installation Successful!" -ForegroundColor Green
        Write-Host "========================================" -ForegroundColor Green
        Write-Host ""
        Write-Host "ggo has been installed to: $destPath" -ForegroundColor White
        Write-Host ""
        
        # Show version
        try {
            Write-Host "Installed version:" -ForegroundColor White
            & $destPath --version
        }
        catch {
            # Ignore version check errors
        }
        
        Write-Host ""
        Write-Host "Quick start:" -ForegroundColor Yellow
        Write-Host "  # Show help" -ForegroundColor Gray
        Write-Host "  ggo --help" -ForegroundColor White
        Write-Host ""
        Write-Host "  # Register as agent (on GPU server)" -ForegroundColor Gray
        Write-Host "  ggo agent register --token <your-token>" -ForegroundColor White
        Write-Host ""
        Write-Host "  # Use a shared GPU" -ForegroundColor Gray
        Write-Host "  ggo use <short-code>" -ForegroundColor White
        Write-Host ""
    }
    finally {
        # Cleanup temp directory
        if (Test-Path $tempDir) {
            Remove-Item -Path $tempDir -Recurse -Force -ErrorAction SilentlyContinue
        }
    }
}

# Run installation
try {
    Install-Ggo
}
catch {
    Write-Err $_.Exception.Message
    exit 1
}

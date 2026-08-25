# GlassMarble (gmb) installer for Windows PowerShell
# Usage: irm https://raw.githubusercontent.com/Syamchand123/GlassMarble/main/install.ps1 | iex

$ErrorActionPreference = "Stop"

$Repo = "Syamchand123/GlassMarble"
$BinaryName = "gmb.exe"
$AliasName = "glassmarble.exe"

# Architecture detection
$Arch = "amd64"
if ([System.Environment]::Is64BitOperatingSystem) {
    if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture -eq [System.Runtime.InteropServices.Architecture]::Arm64) {
        $Arch = "arm64"
    } else {
        $Arch = "amd64"
    }
} else {
    Write-Error "Error: 32-bit Windows is not supported. Please build from source."
    exit 1
}

# Version resolution
if (-not $env:VERSION) {
    try {
        $LatestRelease = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" -UseBasicParsing
        $Version = $LatestRelease.tag_name
    } catch {
        $Version = "v1.0.0"
    }
} else {
    $Version = $env:VERSION
}

$CleanVer = $Version.TrimStart("v")
$ArchiveName = "gmb_${CleanVer}_windows_${Arch}.zip"
$DownloadUrl = "https://github.com/$Repo/releases/download/$Version/$ArchiveName"
$ChecksumUrl = "https://github.com/$Repo/releases/download/$Version/checksums.txt"

$InstallDir = if ($env:INSTALL_DIR) { $env:INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "Programs\gmb" }
$TempDir = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid().ToString())

New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
New-Item -ItemType Directory -Path $TempDir -Force | Out-Null

try {
    Write-Host "==> Downloading GlassMarble $Version (windows/$Arch)..." -ForegroundColor Cyan
    $ZipPath = Join-Path $TempDir $ArchiveName
    Invoke-WebRequest -Uri $DownloadUrl -OutFile $ZipPath -UseBasicParsing

    # Optional checksum check
    try {
        $ChecksumPath = Join-Path $TempDir "checksums.txt"
        Invoke-WebRequest -Uri $ChecksumUrl -OutFile $ChecksumPath -UseBasicParsing
        $FileHash = (Get-FileHash -Path $ZipPath -Algorithm SHA256).Hash.ToLower()
        $ExpectedHash = (Get-Content $ChecksumPath | Select-String $ArchiveName).Line.Split(" ")[0].ToLower()
        if ($ExpectedHash -and ($FileHash -ne $ExpectedHash)) {
            Write-Error "Error: SHA256 checksum mismatch for $ArchiveName"
            exit 1
        }
        Write-Host "==> Checksum verified successfully." -ForegroundColor Green
    } catch {
        # Checksum file may not exist for unreleased builds
    }

    Write-Host "==> Extracting files..." -ForegroundColor Cyan
    Expand-Archive -Path $ZipPath -DestinationPath $TempDir -Force

    Copy-Item (Join-Path $TempDir $BinaryName) (Join-Path $InstallDir $BinaryName) -Force
    if (Test-Path (Join-Path $TempDir $AliasName)) {
        Copy-Item (Join-Path $TempDir $AliasName) (Join-Path $InstallDir $AliasName) -Force
    } else {
        Copy-Item (Join-Path $InstallDir $BinaryName) (Join-Path $InstallDir $AliasName) -Force
    }

    Write-Host "`n✓ GlassMarble installed successfully to $InstallDir" -ForegroundColor Green

    # Check and add to User PATH
    $UserPath = [Environment]::GetEnvironmentVariable("Path", [EnvironmentVariableTarget]::User)
    if ($UserPath -notlike "*$InstallDir*") {
        Write-Host "==> Adding $InstallDir to User PATH environment variable..." -ForegroundColor Yellow
        [Environment]::SetEnvironmentVariable("Path", "$UserPath;$InstallDir", [EnvironmentVariableTarget]::User)
        $env:Path += ";$InstallDir"
        Write-Host "PATH updated. Please restart your terminal for changes to take full effect." -ForegroundColor Yellow
    }

    # Verify execution
    & (Join-Path $InstallDir $BinaryName) version
} finally {
    Remove-Item -Path $TempDir -Recurse -Force -ErrorAction SilentlyContinue
}

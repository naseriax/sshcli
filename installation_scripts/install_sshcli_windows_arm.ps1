$ReleaseUrl = "https://github.com/naseriax/sshcli/releases/download/20250911.0823/sshcli_windows_arm64.exe"
$DestDir    = Join-Path $HOME "sshcli"
$AppName    = "sshcli.exe"
$FinalPath  = Join-Path $DestDir $AppName
Write-Host "Starting installation of the sshcli executable for Windows (x86_64)..."
Write-Host "Creating directory: $DestDir"
try {
    if (-not (Test-Path -Path $DestDir)) {
        New-Item -ItemType Directory -Path $DestDir -Force | Out-Null
    }
}
catch {
    Write-Error "Failed to create directory '$_'. Please check your permissions."
    exit 1
}

Write-Host "Downloading file from: $ReleaseUrl"
try {
    Invoke-WebRequest -Uri $ReleaseUrl -OutFile $FinalPath
}
catch {
    Write-Error "Download failed: $_"
    exit 1
}

Write-Host "Download complete! The executable has been saved to:"
Write-Host "$FinalPath"
Write-Host "Adding sshcli to your PATH..."
try {
    $userPath = [System.Environment]::GetEnvironmentVariable('Path', 'User')
    if ($userPath -notlike "*$DestDir*") {
        $newPath = "$userPath;$DestDir"
        [System.Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
        $env:Path += ";$DestDir"
        Write-Host "'$DestDir' has been added to your User PATH."
    }
    else {
        Write-Host "'$DestDir' is already in your PATH."
    }
}
catch {
    Write-Error "Failed to modify the PATH environment variable: $_"
    exit 1
}

Write-Host ""
Write-Host "🎉 Success! sshcli has been installed."
Write-Host ""
Write-Host "To finish the setup, please do one of the following:"
Write-Host "1) Open a new PowerShell or Command Prompt window."
Write-Host "   OR"
Write-Host "2) Your current session has been updated. You can start using 'sshcli.exe' now."
Write-Host ""


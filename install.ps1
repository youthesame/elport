$ErrorActionPreference = "Stop"
$prevProgressPreference = $ProgressPreference
$ProgressPreference = "SilentlyContinue"

try {
    $Repo = "youthesame/elport"
    $InstallDir = if ($env:ELPORT_INSTALL_DIR) { $env:ELPORT_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "elport" }
    $Tag = $env:ELPORT_VERSION

    if (-not $Tag) {
        $resp = Invoke-WebRequest -UseBasicParsing -Uri "https://github.com/$Repo/releases/latest"
        $finalUrl = $null
        if ($resp.BaseResponse -and $resp.BaseResponse.ResponseUri) {
            $finalUrl = $resp.BaseResponse.ResponseUri.AbsoluteUri
        } elseif ($resp.BaseResponse -and $resp.BaseResponse.RequestMessage -and $resp.BaseResponse.RequestMessage.RequestUri) {
            $finalUrl = $resp.BaseResponse.RequestMessage.RequestUri.AbsoluteUri
        }
        $prefix = "https://github.com/$Repo/releases/tag/"
        if (-not $finalUrl -or -not $finalUrl.StartsWith($prefix, [System.StringComparison]::OrdinalIgnoreCase)) {
            throw "Could not determine latest release tag from $finalUrl"
        }
        $Tag = $finalUrl.Substring($prefix.Length)
    }
    if (-not $Tag.StartsWith("v")) {
        $Tag = "v$Tag"
    }
    if ($Tag -notmatch '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$') {
        throw "Invalid release tag: $Tag"
    }
    if ($Tag.StartsWith("v0.1.")) {
        throw "Latest release $Tag is the Python v0.1.x line; set `$env:ELPORT_VERSION to a Go release tag"
    }

    $Arch = "amd64"
    try {
        $osArch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()
        if ($osArch -eq "arm64") { $Arch = "arm64" }
    } catch {
        if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { $Arch = "arm64" }
    }

    $Ver = $Tag.Substring(1)
    $Archive = "elport_${Ver}_windows_${Arch}.zip"
    $BaseUrl = "https://github.com/$Repo/releases/download/$Tag"
    $Tmp = Join-Path $env:TEMP ("elport-install-" + [Guid]::NewGuid().ToString("N").Substring(0, 8))
    New-Item -ItemType Directory -Path $Tmp | Out-Null

    try {
        $zipPath = Join-Path $Tmp $Archive
        $sumPath = Join-Path $Tmp "checksums.txt"
        Invoke-WebRequest -UseBasicParsing -Uri "$BaseUrl/$Archive" -OutFile $zipPath
        Invoke-WebRequest -UseBasicParsing -Uri "$BaseUrl/checksums.txt" -OutFile $sumPath

        $expectedLine = Get-Content $sumPath | Where-Object { $_ -match ("\s+" + [regex]::Escape($Archive) + "$") } | Select-Object -First 1
        if (-not $expectedLine) {
            throw "Checksum entry not found for $Archive"
        }
        $expected = ($expectedLine.Trim().Split()[0]).ToLowerInvariant()
        $actual = (Get-FileHash -Path $zipPath -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($actual -ne $expected) {
            throw "SHA-256 mismatch for $Archive"
        }

        Expand-Archive -Force -Path $zipPath -DestinationPath $Tmp
        New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
        Copy-Item -Force -Path (Join-Path $Tmp "elport.exe") -Destination (Join-Path $InstallDir "elport.exe")

        if (-not $env:ELPORT_NO_MODIFY_PATH) {
            $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
            $entries = @()
            if ($userPath) {
                $entries = $userPath.Split(";") | ForEach-Object { $_.Trim().TrimEnd("\") } | Where-Object { $_ -ne "" }
            }
            $normalizedDir = $InstallDir.Trim().TrimEnd("\")
            $alreadyInPath = $entries | Where-Object { $_.Equals($normalizedDir, [System.StringComparison]::OrdinalIgnoreCase) }
            if (-not $alreadyInPath) {
                $newPath = if ($userPath) { "$userPath;$InstallDir" } else { $InstallDir }
                [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
                Write-Output "Added $InstallDir to your user PATH (restart your terminal to use 'elport')."
            }
        }
        Write-Output "Installed elport $Tag to $(Join-Path $InstallDir 'elport.exe')"
    } finally {
        Remove-Item -Recurse -Force -Path $Tmp -ErrorAction SilentlyContinue
    }
} finally {
    $ProgressPreference = $prevProgressPreference
}

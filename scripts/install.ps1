<#
  Hyper Stack installer for Windows (no administrator rights needed for this step).

  Public repo, one line in PowerShell:
    irm https://github.com/HyperDesignCo/hyper-stack/releases/latest/download/install.ps1 | iex

  Private repo (needs the GitHub CLI signed in, gh auth login):
    gh release download --repo HyperDesignCo/hyper-stack --pattern install.ps1 --output install.ps1 --clobber
    powershell -ExecutionPolicy Bypass -File .\install.ps1

  Options:  -Version v0.1.0   install a specific release (default: latest)
            -Autostart        start the tray icon when you log in
            -Install          also run "hyper install" (downloads about 300 MB, one UAC prompt)
            -From <folder>    install from a local folder holding the release files (offline or testing)
#>
param(
    [string]$Version = 'latest',
    [switch]$Autostart,
    [switch]$Install,
    [string]$From = ''
)
$ErrorActionPreference = 'Stop'
$repo = 'HyperDesignCo/hyper-stack'
$zipName = 'hyper-windows-amd64.zip'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

if ($env:OS -ne 'Windows_NT') { throw 'This installer is for Windows.' }

$tmp = Join-Path ([IO.Path]::GetTempPath()) ('hyper-stack-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
    $haveGh = $false
    if ($From) {
        foreach ($f in $zipName, 'checksums.txt') {
            if (-not (Test-Path (Join-Path $From $f))) { throw "$f not found in $From." }
            Copy-Item (Join-Path $From $f) $tmp
        }
    } elseif (Get-Command gh -ErrorAction SilentlyContinue) {
        gh auth status *> $null
        $haveGh = ($LASTEXITCODE -eq 0)
    }

    if (-not $From) { Write-Host "Downloading Hyper Stack ($Version)..." }
    if ($From) {
        Write-Host "Installing from $From"
    } elseif ($haveGh) {
        $tagArgs = @(); if ($Version -ne 'latest') { $tagArgs = @($Version) }
        gh release download @tagArgs --repo $repo --pattern $zipName --pattern checksums.txt --dir $tmp --clobber
        if ($LASTEXITCODE) { throw "gh could not download the release from $repo." }
    } else {
        $base = if ($Version -eq 'latest') { "https://github.com/$repo/releases/latest/download" } else { "https://github.com/$repo/releases/download/$Version" }
        try {
            Invoke-WebRequest "$base/$zipName" -OutFile (Join-Path $tmp $zipName) -UseBasicParsing
            Invoke-WebRequest "$base/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt') -UseBasicParsing
        } catch {
            throw "Download failed: $($_.Exception.Message). If the repository is private, install the GitHub CLI and run 'gh auth login', then run this script again."
        }
    }

    # Verify the download against the published checksum before running anything from it.
    $expected = (Get-Content (Join-Path $tmp 'checksums.txt') | Where-Object { $_ -match [regex]::Escape($zipName) } | Select-Object -First 1) -split '\s+' | Select-Object -First 1
    $actual = (Get-FileHash (Join-Path $tmp $zipName) -Algorithm SHA256).Hash.ToLower()
    if (-not $expected -or $expected.ToLower() -ne $actual) {
        throw "Checksum mismatch for $zipName (expected $expected, got $actual). Nothing was installed."
    }

    Expand-Archive (Join-Path $tmp $zipName) -DestinationPath $tmp -Force
    $exe = Join-Path $tmp 'hyper.exe'
    if (-not (Test-Path $exe)) { throw 'The archive does not contain hyper.exe.' }

    $siArgs = @('self-install'); if ($Autostart) { $siArgs += '--autostart' }
    & $exe @siArgs
    if ($LASTEXITCODE) { throw 'hyper self-install failed.' }

    if ($Install) {
        Write-Host "`nRunning hyper install (this downloads about 300 MB)..."
        & (Join-Path $env:LOCALAPPDATA 'HyperStack\app\hyper.exe') install
        if ($LASTEXITCODE) { throw 'hyper install failed.' }
    }
} finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}

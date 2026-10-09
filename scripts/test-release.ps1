# Builds a release locally and runs install.ps1 against it (no network, nothing published).
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

"=== build"
& "$root\scripts\build.ps1" -Version 0.1.0

"`n=== install from the local release"
& "$root\scripts\install.ps1" -From "$root\dist"
$installed = "$env:LOCALAPPDATA\HyperStack\app\hyper.exe"
"installed version: $(& $installed --version)"

"`n=== tampered archive must be refused"
$bad = Join-Path $env:TEMP 'hyper-tampered'
Remove-Item -Recurse -Force $bad -ErrorAction SilentlyContinue
Copy-Item "$root\dist" $bad -Recurse
Add-Content "$bad\hyper-windows-amd64.zip" 'x'
try {
    & "$root\scripts\install.ps1" -From $bad
    "FAIL: installer accepted a tampered archive"
} catch {
    "refused as expected: $($_.Exception.Message)"
}
Remove-Item -Recurse -Force $bad

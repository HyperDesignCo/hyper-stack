<#
  Builds the Windows release locally, exactly like the CI release job does.
  Output: dist\hyper-windows-amd64.zip, dist\hyper-darwin-arm64, dist\hyper-darwin-amd64,
          dist\checksums.txt, dist\install.ps1, dist\install.sh

  Usage:  .\scripts\build.ps1 -Version 0.1.0
#>
param(
    [string]$Version = '0.0.0-dev'
)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

if (-not (Get-Command go -ErrorAction SilentlyContinue)) { $env:Path += ';C:\Program Files\Go\bin' }

$commit = 'none'
try { $commit = (git rev-parse --short HEAD 2>$null); if (-not $commit) { $commit = 'none' } } catch { }
$date = (Get-Date).ToUniversalTime().ToString('yyyy-MM-dd')
$pkg = 'github.com/hyperdesign/hyper-stack/internal/cli'

go vet ./...
if ($LASTEXITCODE) { throw 'go vet failed' }
go test ./...
if ($LASTEXITCODE) { throw 'tests failed' }

$dist = Join-Path $root 'dist'
Remove-Item -Recurse -Force $dist -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force $dist | Out-Null

$env:GOOS = 'windows'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
go build -trimpath -ldflags "-s -w -X $pkg.Version=$Version -X $pkg.Commit=$commit -X $pkg.Date=$date" -o "$dist\hyper.exe" .\cmd\hyper
if ($LASTEXITCODE) { throw 'build failed' }
Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED

Compress-Archive -Path "$dist\hyper.exe" -DestinationPath "$dist\hyper-windows-amd64.zip" -Force
Remove-Item "$dist\hyper.exe"
foreach ($arch in 'arm64', 'amd64') {
    $env:GOOS = 'darwin'; $env:GOARCH = $arch; $env:CGO_ENABLED = '0'
    go build -trimpath -ldflags "-s -w -X $pkg.Version=$Version -X $pkg.Commit=$commit -X $pkg.Date=$date" -o "$dist\hyper-darwin-$arch" .\cmd\hyper
    if ($LASTEXITCODE) { throw "macOS $arch build failed" }
    Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED
}
Copy-Item "$root\scripts\install.ps1" "$dist\install.ps1"
Copy-Item "$root\scripts\install.sh" "$dist\install.sh"

$lines = foreach ($f in 'hyper-windows-amd64.zip', 'hyper-darwin-arm64', 'hyper-darwin-amd64', 'install.ps1', 'install.sh') {
    $h = (Get-FileHash "$dist\$f" -Algorithm SHA256).Hash.ToLower()
    "$h  $f"
}
# LF line endings, so shasum -c and install.sh read it on a Mac
[IO.File]::WriteAllText("$dist\checksums.txt", (($lines -join "`n") + "`n"))

Get-ChildItem $dist | Select-Object Name, @{n = 'KB'; e = { [math]::Round($_.Length / 1KB) } }
Write-Host "`nBuilt hyper $Version (commit $commit, $date)"

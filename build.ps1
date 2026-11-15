# Build the distributable mousemover.exe into dist\.
# Windows PowerShell 5.1 compatible. Usage:
#   powershell -ExecutionPolicy Bypass -File .\build.ps1
# Keep the flags in sync with build.sh.

$ErrorActionPreference = 'Stop'

$root = $PSScriptRoot
$output = Join-Path $root 'dist\mousemover.exe'

$envNames = @('CGO_ENABLED', 'GOOS', 'GOARCH')
$saved = @{}
foreach ($name in $envNames) {
    $saved[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
}

$exitCode = 0
Push-Location $root
try {
    $env:CGO_ENABLED = '0'
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'

    Write-Host '==> go vet ./...'
    go vet ./...
    if ($LASTEXITCODE -ne 0) { throw "go vet failed (exit $LASTEXITCODE)" }

    Write-Host '==> go test ./...'
    go test ./...
    if ($LASTEXITCODE -ne 0) { throw "go test failed (exit $LASTEXITCODE)" }

    New-Item -ItemType Directory -Force -Path (Join-Path $root 'dist') | Out-Null

    Write-Host '==> go build'
    go build -ldflags "-s -w -H=windowsgui" -o dist/mousemover.exe ./cmd/mousemover
    if ($LASTEXITCODE -ne 0) { throw "go build failed (exit $LASTEXITCODE)" }

    Write-Host "Built: $output"
}
catch {
    Write-Host "Build failed: $_" -ForegroundColor Red
    $exitCode = 1
}
finally {
    Pop-Location
    foreach ($name in $envNames) {
        [Environment]::SetEnvironmentVariable($name, $saved[$name], 'Process')
    }
}

exit $exitCode

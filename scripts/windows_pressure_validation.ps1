# Native Windows validation for the pressure API test family (issue #79),
# tracked by issue #99.
#
# This is NOT a gate. The family is reported to flake on a Windows host, so a
# failing run is the evidence this script exists to capture. Every path writes
# the log and the environment record before it returns a status, because the
# interesting run is the one that fails.
#
# The contract this script keeps is pinned by internal/ciguard so `make verify`
# catches drift on Linux before a dispatch wastes a Windows runner.

[CmdletBinding()]
param(
    [int]$Count = 100,
    [string]$OutputDir = '',
    [switch]$SkipGate
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

if ($Count -lt 1) {
    throw "Count must be at least 1, got $Count"
}
# RUNNER_TEMP exists on a GitHub runner and nowhere else. A developer running
# this by hand must not need it, so the fallback resolves here rather than in
# the parameter default, where a null would fail before the script starts.
if ([string]::IsNullOrWhiteSpace($OutputDir)) {
    $base = $env:RUNNER_TEMP
    if ([string]::IsNullOrWhiteSpace($base)) {
        $base = [System.IO.Path]::GetTempPath()
    }
    $OutputDir = Join-Path $base 'windows pressure validation'
}
New-Item -ItemType Directory -Force -Path $OutputDir | Out-Null

$familyLog = Join-Path $OutputDir 'pressure-family.log'
$gateLog = Join-Path $OutputDir 'windows-gate.log'
$environmentRecord = Join-Path $OutputDir 'windows-environment.txt'

# Record the environment first. Issue #79 asks for the Windows version, the Go
# version, and the commit, and a crash later must not lose them.
$os = Get-CimInstance Win32_OperatingSystem
$goVersion = & go version
$commit = & git rev-parse HEAD
@(
    "windows_caption: $($os.Caption)"
    "windows_version: $($os.Version)"
    "windows_build: $($os.BuildNumber)"
    "go_version: $goVersion"
    "commit: $commit"
    "repetitions: $Count"
    "logical_processors: $($env:NUMBER_OF_PROCESSORS)"
) | Set-Content -LiteralPath $environmentRecord -Encoding utf8

Write-Host "Recording $Count repetitions of the pressure API family to $familyLog"

& go test -race -shuffle=on -count=$Count ./pkg/scanapp -run '^TestPollPressureAPI_' *> $familyLog
$familyStatus = $LASTEXITCODE
Get-Content -LiteralPath $familyLog -ErrorAction SilentlyContinue | Write-Host

$gateStatus = 0
if ($SkipGate) {
    'skipped by -SkipGate' | Set-Content -LiteralPath $gateLog -Encoding utf8
} else {
    # Issue #79 records the full gate after the family loop, because a flake that
    # only appears under the gate's load would otherwise go unseen.
    & ./scripts/windows_gate.ps1 *> $gateLog
    $gateStatus = $LASTEXITCODE
    Get-Content -LiteralPath $gateLog -ErrorAction SilentlyContinue | Write-Host
}

$exitCode = $familyStatus
if ($exitCode -eq 0) {
    $exitCode = $gateStatus
}

Write-Host "pressure family exit: $familyStatus"
Write-Host "windows gate exit: $gateStatus"
Write-Host "evidence directory: $OutputDir"
exit $exitCode

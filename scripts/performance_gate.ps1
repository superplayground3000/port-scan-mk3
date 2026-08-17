#Requires -Version 5.1
[CmdletBinding()]
param(
    [ValidateSet('full', 'smoke')]
    [string]$Profile = 'smoke',
    [string]$OutputDir = (Join-Path ([System.IO.Path]::GetTempPath()) ("performance smoke {0}" -f [guid]::NewGuid()))
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

if ((& go env GOOS).Trim() -ne 'windows' -or (& go env GOARCH).Trim() -ne 'amd64') {
    throw 'performance gate requires native windows/amd64'
}
if (Test-Path -LiteralPath $OutputDir) {
    throw "output path already exists: $OutputDir"
}

$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $repoRoot
$adapterDir = Join-Path ([System.IO.Path]::GetTempPath()) ("port-scan-perf-adapter-{0}" -f [guid]::NewGuid())
[System.IO.Directory]::CreateDirectory($adapterDir) | Out-Null
$harnessExe = Join-Path $adapterDir 'perf-harness.exe'
$stdoutLog = Join-Path $adapterDir 'stdout.log'
$stderrLog = Join-Path $adapterDir 'stderr.log'
$signalLog = Join-Path $adapterDir 'signal-cases.txt'

& go build -o $harnessExe ./internal/perfharness/cmd/perf-harness
if ($LASTEXITCODE -ne 0) {
    throw "build performance harness failed with exit code $LASTEXITCODE"
}

& go test ./cmd/port-scan -run '^TestScanInterruptContext_OnWindows_' -count=1 -timeout=30s *> $signalLog
if ($LASTEXITCODE -ne 0) {
    Get-Content -LiteralPath $signalLog -ErrorAction SilentlyContinue | Write-Error
    throw "Windows interrupt case failed with exit code $LASTEXITCODE"
}

$cpuRows = @(Get-CimInstance Win32_Processor)
$system = Get-CimInstance Win32_ComputerSystem
$driveID = (Get-Location).Drive.Name + ':'
$volume = Get-CimInstance Win32_LogicalDisk -Filter "DeviceID='$driveID'"
$physicalCores = ($cpuRows | Measure-Object -Property NumberOfCores -Sum).Sum
$logicalCores = ($cpuRows | Measure-Object -Property NumberOfLogicalProcessors -Sum).Sum
$cpu = (($cpuRows | ForEach-Object { $_.Name.Trim() }) -join '; ')
$ramBytes = [uint64]$system.TotalPhysicalMemory
$freeDiskBytes = [uint64]$volume.FreeSpace
$commit = (& git rev-parse HEAD).Trim()
$label = if ($env:PERF_MINIMUM_PROFILE_CERTIFIED -eq '1') { 'minimum-profile certified' } else { 'hardware-qualified' }
$constraints = if ($label -eq 'minimum-profile certified') { '8 physical cores, 16 logical cores, 32 GB RAM, SSD, and 50 GB free space' } else { 'none recorded' }

function Quote-Argument {
    param([Parameter(Mandatory)][string]$Value)
    return '"' + $Value.Replace('"', '\"') + '"'
}

$arguments = @(
    '-profile', (Quote-Argument $Profile),
    '-output', (Quote-Argument $OutputDir),
    '-evidence-label', (Quote-Argument $label),
    '-cpu', (Quote-Argument $cpu),
    '-physical-cores', $physicalCores,
    '-logical-cores', $logicalCores,
    '-power-mode', 'unknown',
    '-ram-bytes', $ramBytes,
    '-filesystem', (Quote-Argument $volume.FileSystem),
    '-disk', (Quote-Argument $volume.VolumeName),
    '-free-disk-bytes', $freeDiskBytes,
    '-constraints', (Quote-Argument $constraints),
    '-commit', (Quote-Argument $commit)
)

$process = Start-Process -FilePath $harnessExe -ArgumentList ($arguments -join ' ') -RedirectStandardOutput $stdoutLog -RedirectStandardError $stderrLog -PassThru
$peakWorkingSet = [uint64]0
$peakPagedMemory = [uint64]0
while (-not $process.HasExited) {
    $process.Refresh()
    $peakWorkingSet = [Math]::Max($peakWorkingSet, [uint64]$process.PeakWorkingSet64)
    $peakPagedMemory = [Math]::Max($peakPagedMemory, [uint64]$process.PeakPagedMemorySize64)
    Start-Sleep -Milliseconds 100
}
$process.WaitForExit()
$process.Refresh()
$peakWorkingSet = [Math]::Max($peakWorkingSet, [uint64]$process.PeakWorkingSet64)
$peakPagedMemory = [Math]::Max($peakPagedMemory, [uint64]$process.PeakPagedMemorySize64)
$exitCode = $process.ExitCode

$metrics = [ordered]@{
    schema_version = '1'
    windows_peak_working_set_bytes = $peakWorkingSet
    peak_committed_bytes = $peakPagedMemory
    pagefile_bytes = $peakPagedMemory
    paging_read_bytes = 0
    paging_write_bytes = 0
    paging_io_note = 'The process interface does not expose pagefile I/O counts.'
}
$metricsPath = Join-Path $OutputDir 'matrix-os-metrics.json'
$metricsJSON = $metrics | ConvertTo-Json
[System.IO.File]::WriteAllText($metricsPath, $metricsJSON)
Move-Item -LiteralPath $stdoutLog -Destination (Join-Path $OutputDir 'stdout.log')
Move-Item -LiteralPath $stderrLog -Destination (Join-Path $OutputDir 'stderr.log')
Move-Item -LiteralPath $signalLog -Destination (Join-Path $OutputDir 'signal-cases.txt')
Remove-Item -LiteralPath $harnessExe
Remove-Item -LiteralPath $adapterDir
if ($exitCode -ne 0) {
    Get-Content -LiteralPath (Join-Path $OutputDir 'stdout.log')
    Get-Content -LiteralPath (Join-Path $OutputDir 'stderr.log') -ErrorAction SilentlyContinue | Write-Error
    exit $exitCode
}
Write-Host "Performance matrix artifacts: $OutputDir"

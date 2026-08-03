#Requires -Version 5.1
<#
.SYNOPSIS
    Native Windows validation gate (issue #63).

.DESCRIPTION
    The bash gate (scripts/verify.sh) and the Docker e2e suite (e2e/run_e2e.sh)
    both prove things about LINUX: verify.sh needs a POSIX shell, and the e2e
    Compose stack builds and runs Linux binaries inside containers. Neither has
    ever executed a Windows .exe. This script is the Windows-native half of the
    same contract, and it is the ONLY place that logic lives — .github/workflows/
    ci.yml just calls it (see .claude/rules/90-letter-to-future-sessions.md:
    "logic lives in the scripts; keep the workflow thin").

    It automates the highest-value parts of the manual plan in issue #60 without
    Docker:

      1. environment report
      2. ASCII TEMP/TMP/GOTMPDIR (MSYS2 GCC breaks on non-ASCII temp paths)
      3. race prerequisites: a 64-bit MinGW-w64 gcc, CGO on, and a probe that
         proves the race detector actually fires
      4. go vet + go build
      5. go test -race -shuffle=on ./...   (the same test line verify.sh runs)
      6. build and launch every .exe under cmd/
      7. loopback-only generate-buckets -> scan pipeline: open vs closed
      8. loopback-only generate-buckets -> scan (aborted) -> scan -resume:
         append-reopen with one header and no lost or duplicated rows
      9. output paths containing SPACES, plus immediate rename/delete checks
         that prove every file handle was released at process exit

    Everything it scans is 127.0.0.0/8, created by this script (constitution V:
    never scan a real external host).

.PARAMETER KeepWorkspace
    Keep the scratch workspace instead of deleting it. Deleting it is itself an
    assertion (a leaked handle makes the delete fail), so this is for debugging
    a failure only.

.EXAMPLE
    pwsh -File .\scripts\windows_gate.ps1

.NOTES
    Prerequisites are documented in docs/MAINTENANCE.md section 2.1. RUNTIME
    prerequisites (Go + Windows) are NOT the same as RACE-TEST prerequisites
    (64-bit MinGW-w64 + cgo). A missing race compiler FAILS this gate; it never
    downgrades to a non-race run.
#>
[CmdletBinding()]
param(
    [switch]$KeepWorkspace
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

# PowerShell 7.3+ can turn a native command's stderr into a terminating error.
# Native exit codes are checked explicitly here, so switch that off where it
# exists; otherwise a chatty-but-successful `go` invocation would fail the gate.
if (Test-Path -LiteralPath 'variable:\PSNativeCommandUseErrorActionPreference') {
    $PSNativeCommandUseErrorActionPreference = $false
}

# ---------------------------------------------------------------------------
# Gate constants. The Go contract test internal/ciguard/windows_gate_test.go
# reads both of these, so they are the single source of truth:
#   - $gateCommands must equal the directory listing of cmd/
#   - $gateWorkspaceName must contain a space
# ---------------------------------------------------------------------------
$gateCommands = @('cidr-compare', 'csv-transform', 'enrich-targets', 'port-scan', 'preprocess')
$gateWorkspaceName = 'win gate ws'

$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $repoRoot

$script:stepIndex = 0

function Write-Step {
    param([Parameter(Mandatory)][string]$Name)
    $script:stepIndex++
    Write-Host ''
    Write-Host ("=== {0}. {1} ===" -f $script:stepIndex, $Name)
}

function Assert-True {
    param(
        [Parameter(Mandatory)][bool]$Condition,
        [Parameter(Mandatory)][string]$Message
    )
    if (-not $Condition) {
        throw "windows gate assertion failed: $Message"
    }
}

# Invoke-Native runs a native command, tees every stream to a log file, and
# fails the gate on an unexpected exit code. ExpectFailure inverts the check for
# the deliberately-aborted scan.
function Invoke-Native {
    param(
        [Parameter(Mandatory)][string]$Exe,
        [Parameter(Mandatory)][string[]]$Arguments,
        [Parameter(Mandatory)][string]$LogPath,
        [switch]$ExpectFailure
    )
    & $Exe @Arguments *> $LogPath
    $code = $LASTEXITCODE
    $log = if (Test-Path -LiteralPath $LogPath) { Get-Content -LiteralPath $LogPath -Raw } else { '' }
    if ($ExpectFailure) {
        if ($code -eq 0) {
            Write-Host $log
            throw "windows gate assertion failed: '$Exe $($Arguments -join ' ')' was expected to fail but exited 0"
        }
    }
    elseif ($code -ne 0) {
        Write-Host $log
        throw "windows gate: '$Exe $($Arguments -join ' ')' exited $code"
    }
    return $code
}

function Test-AsciiPath {
    param(
        [AllowNull()]
        [AllowEmptyString()]
        [string]$Path
    )
    if ([string]::IsNullOrEmpty($Path)) { return $false }
    # -cmatch is case-sensitive and anchored; \x20-\x7E is printable ASCII.
    return ($Path -cmatch '^[\x20-\x7E]+$')
}

function New-LoopbackListener {
    $listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, 0)
    $listener.Start()
    return $listener
}

function Get-ListenerPort {
    param([Parameter(Mandatory)][System.Net.Sockets.TcpListener]$Listener)
    return ([System.Net.IPEndPoint]$Listener.LocalEndpoint).Port
}

# Get-ReservedClosedPort binds a loopback port and releases it immediately, so
# a connect() to it is refused. Used both for "closed port" targets and for the
# unreachable pressure API endpoint.
function Get-ReservedClosedPort {
    $listener = New-LoopbackListener
    $port = Get-ListenerPort -Listener $listener
    $listener.Stop()
    return $port
}

function Write-TextFile {
    param(
        [Parameter(Mandatory)][string]$Path,
        [Parameter(Mandatory)][string]$Text
    )
    # WriteAllText emits UTF-8 with no BOM; Set-Content's default encoding
    # differs between Windows PowerShell and pwsh and can prepend a BOM that the
    # CSV header parser would then reject.
    [System.IO.File]::WriteAllText($Path, $Text)
}

function Get-CsvDataRows {
    param([Parameter(Mandatory)][string]$Path)
    $lines = @(Get-Content -LiteralPath $Path | Where-Object { $_ -ne '' })
    if ($lines.Count -le 1) { return @() }
    return @($lines[1..($lines.Count - 1)])
}

function Get-SingleFile {
    param(
        [Parameter(Mandatory)][string]$Directory,
        [Parameter(Mandatory)][string]$Filter,
        [Parameter(Mandatory)][string]$What
    )
    $files = @(Get-ChildItem -LiteralPath $Directory -Filter $Filter -File)
    Assert-True ($files.Count -eq 1) "$What : expected exactly 1 file matching '$Filter' in '$Directory', found $($files.Count)"
    return $files[0]
}

# Assert-HandleReleased renames a file and renames it back, then reports the
# path. On Windows an open handle makes the rename fail with a sharing
# violation, so this is a direct test that the process closed its outputs before
# exiting — the thing the Linux gates can never check.
function Assert-HandleReleased {
    param(
        [Parameter(Mandatory)][string]$Path,
        [Parameter(Mandatory)][string]$What
    )
    $dir = Split-Path -Parent $Path
    $name = Split-Path -Leaf $Path
    $probeName = "$name.handle-probe"
    $probePath = Join-Path $dir $probeName
    try {
        Rename-Item -LiteralPath $Path -NewName $probeName
    }
    catch {
        throw "windows gate assertion failed: $What : '$Path' could not be renamed immediately after the process exited, so a file handle was still open: $($_.Exception.Message)"
    }
    Rename-Item -LiteralPath $probePath -NewName $name
    Write-Host "  handle released: $What ($name)"
}

# ===========================================================================
Write-Step 'environment'
Write-Host "PowerShell : $($PSVersionTable.PSVersion)"
Write-Host "OS         : $([System.Environment]::OSVersion.VersionString)"
Write-Host "repo root  : $repoRoot"
& go version
Assert-True ($LASTEXITCODE -eq 0) "'go version' failed; the Go toolchain is a RUNTIME prerequisite"
& go env GOOS GOARCH GOVERSION
Assert-True ($LASTEXITCODE -eq 0) "'go env' failed"

$goos = (& go env GOOS).Trim()
Assert-True ($goos -eq 'windows') "this gate must run natively on Windows, but 'go env GOOS' reported '$goos' (cross-compilation is not proof of native execution)"

# ===========================================================================
Write-Step 'ASCII temporary directories (MSYS2 GCC requirement)'
# MSYS2/MinGW GCC fails to create its own temp files when TEMP contains
# non-ASCII characters, which is common on localized Windows installs where the
# user profile carries the operator's name. Redirect all three variables to an
# ASCII path rather than letting the race build fail with an unrelated error.
$tempCandidates = @{ TEMP = $env:TEMP; TMP = $env:TMP; GOTMPDIR = $env:GOTMPDIR }
$needsAsciiTemp = $false
foreach ($key in @('TEMP', 'TMP')) {
    if (-not (Test-AsciiPath $tempCandidates[$key])) { $needsAsciiTemp = $true }
}
if ($tempCandidates['GOTMPDIR'] -and -not (Test-AsciiPath $tempCandidates['GOTMPDIR'])) { $needsAsciiTemp = $true }

if ($needsAsciiTemp) {
    $asciiTemp = Join-Path $env:SystemDrive 'port-scan-gate-tmp'
    New-Item -ItemType Directory -Force -Path $asciiTemp | Out-Null
    $env:TEMP = $asciiTemp
    $env:TMP = $asciiTemp
    $env:GOTMPDIR = $asciiTemp
    Write-Host "non-ASCII temp path detected; redirected TEMP/TMP/GOTMPDIR to $asciiTemp"
}
else {
    Write-Host "TEMP/TMP are ASCII: $env:TEMP"
}

# ===========================================================================
Write-Step 'race prerequisites (64-bit MinGW-w64 + cgo)'
# -race on windows/amd64 is implemented through cgo and needs a real C compiler.
# Without one `go test -race` fails to build; the danger is a future edit
# "handling" that by dropping -race, so the requirement is asserted up front and
# the failure is loud.
$env:CGO_ENABLED = '1'

$gcc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $gcc) {
    # Common locations on GitHub-hosted and developer Windows machines.
    $candidateDirs = @(
        'C:\mingw64\bin',
        'C:\ProgramData\mingw64\mingw64\bin',
        'C:\msys64\mingw64\bin',
        'C:\Strawberry\c\bin'
    )
    foreach ($dir in $candidateDirs) {
        if (Test-Path -LiteralPath (Join-Path $dir 'gcc.exe')) {
            $env:PATH = "$dir;$env:PATH"
            $gcc = Get-Command gcc -ErrorAction SilentlyContinue
            break
        }
    }
}
if (-not $gcc) {
    throw "windows gate: no C compiler on PATH. The race detector on windows/amd64 needs a 64-bit MinGW-w64 gcc (x86_64-w64-mingw32). Install it (e.g. 'choco install mingw' or MSYS2 'pacman -S mingw-w64-x86_64-gcc') and re-run. This gate FAILS instead of running the tests without -race; see docs/MAINTENANCE.md section 2.1."
}
$gccTriple = (& gcc -dumpmachine)
if ($LASTEXITCODE -ne 0) {
    throw "windows gate: 'gcc -dumpmachine' failed, so the race compiler cannot be trusted."
}
$gccTriple = "$gccTriple".Trim()
Write-Host "gcc        : $($gcc.Source) ($gccTriple)"
if ($gccTriple -notmatch '^x86_64') {
    throw "windows gate: gcc is '$gccTriple' but a 64-bit x86_64-w64-mingw32 compiler is required for -race on windows/amd64. A 32-bit (i686) compiler cannot build the race runtime."
}

# ===========================================================================
Write-Step 'race detector arming probe'
# Asserting "gcc exists" is not the same as "the detector fires". Build a module
# with a known data race OUTSIDE this repository (a racy test inside pkg/ would
# make `make verify` red on every platform) and require the detector to report
# it. If this probe passes cleanly, -race is not actually instrumenting.
$probeDir = Join-Path ([System.IO.Path]::GetTempPath()) ("port-scan-race-probe-" + [System.Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force -Path $probeDir | Out-Null
try {
    Write-TextFile -Path (Join-Path $probeDir 'go.mod') -Text "module raceprobe`r`n`r`ngo 1.24.0`r`n"
    $probeSource = @'
package raceprobe

import (
	"sync"
	"testing"
)

// TestRaceProbe is deliberately racy: two goroutines increment the same
// unsynchronized variable. `go test -race` MUST report a data race here.
func TestRaceProbe(t *testing.T) {
	shared := 0
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100000; j++ {
				shared++
			}
		}()
	}
	wg.Wait()
	_ = shared
}
'@
    Write-TextFile -Path (Join-Path $probeDir 'probe_test.go') -Text $probeSource

    $probeLog = Join-Path $probeDir 'probe.log'
    $probeCode = 0
    $probeOutput = ''
    $probeArmed = $false
    # Two attempts: the detector needs the two goroutines to actually overlap,
    # and a single-core-starved runner could in principle serialise them. Two
    # independent misses would mean -race is not instrumenting, which is the
    # condition worth failing on.
    foreach ($attempt in 1..2) {
        Push-Location -LiteralPath $probeDir
        try {
            & go test -race -count=1 ./... *> $probeLog
            $probeCode = $LASTEXITCODE
        }
        finally {
            Pop-Location
        }
        $probeOutput = Get-Content -LiteralPath $probeLog -Raw
        if ($probeCode -ne 0 -and $probeOutput -match 'DATA RACE') {
            $probeArmed = $true
            break
        }
        Write-Host "  probe attempt $attempt did not report a data race (exit $probeCode)"
    }
    if (-not $probeArmed) {
        Write-Host $probeOutput
        throw "windows gate: the race detector did not report the known data race in the probe module (exit $probeCode). -race is not actually instrumenting this build, so a race-free result from the real test run would be meaningless."
    }
    Write-Host 'race detector is armed (probe reported WARNING: DATA RACE as expected)'
}
finally {
    Remove-Item -LiteralPath $probeDir -Recurse -Force -ErrorAction SilentlyContinue
}

# ===========================================================================
Write-Step 'go vet + go build (native)'
& go vet ./...
Assert-True ($LASTEXITCODE -eq 0) "'go vet ./...' failed on native Windows"
& go build ./...
Assert-True ($LASTEXITCODE -eq 0) "'go build ./...' failed on native Windows"

# ===========================================================================
Write-Step 'go test -race -shuffle=on ./... (native)'
# Same test line as scripts/verify.sh, so a Windows-only concurrency bug cannot
# hide behind the Linux run.
& go test -race -shuffle=on -count=1 ./...
Assert-True ($LASTEXITCODE -eq 0) "'go test -race -shuffle=on ./...' failed on native Windows"

# ===========================================================================
Write-Step 'build and launch every command under cmd/ as a native .exe'
$workspace = Join-Path ([System.IO.Path]::GetTempPath()) $gateWorkspaceName
if (Test-Path -LiteralPath $workspace) {
    Remove-Item -LiteralPath $workspace -Recurse -Force
}
New-Item -ItemType Directory -Force -Path $workspace | Out-Null
Write-Host "workspace  : $workspace   (the space in the name is deliberate)"

$distDir = Join-Path $workspace 'dist'
New-Item -ItemType Directory -Force -Path $distDir | Out-Null

$cmdDirs = @(Get-ChildItem -LiteralPath (Join-Path $repoRoot 'cmd') -Directory | ForEach-Object { $_.Name })
$missing = @($cmdDirs | Where-Object { $gateCommands -notcontains $_ })
Assert-True ($missing.Count -eq 0) "cmd/ contains command(s) the gate does not cover: $($missing -join ', '). Add them to `$gateCommands."

$exePaths = @{}
foreach ($command in $gateCommands) {
    $exePath = Join-Path $distDir "$command.exe"
    & go build -o $exePath "./cmd/$command"
    Assert-True ($LASTEXITCODE -eq 0) "go build of cmd/$command failed"
    Assert-True (Test-Path -LiteralPath $exePath) "cmd/$command produced no .exe at $exePath"
    $exePaths[$command] = $exePath
}

foreach ($command in $gateCommands) {
    $exePath = $exePaths[$command]
    $launchLog = Join-Path $workspace "launch-$command.log"
    # -h is the cheapest "does this image load and run" probe. The commands
    # disagree on the exit code they use for help (port-scan 0, the flag-based
    # ones 1 or 2), so the contract asserted here is: the process started, the
    # Go runtime ran far enough to write output, and it did not die the way a
    # bad image or missing DLL dies (0xC0000135 surfaces as a large negative
    # exit code, never 0..2).
    & $exePath -h *> $launchLog
    $code = $LASTEXITCODE
    $launchOut = Get-Content -LiteralPath $launchLog -Raw
    Assert-True ($code -ge 0 -and $code -le 2) "$command.exe -h exited $code; that is not a usage exit code and suggests the image failed to load (missing DLL / bad image)"
    Assert-True (-not [string]::IsNullOrWhiteSpace($launchOut)) "$command.exe -h produced no output at all, so the binary did not really run"
    Write-Host ("  launched {0,-16} exit={1} output={2} bytes" -f "$command.exe", $code, $launchOut.Length)
    Assert-HandleReleased -Path $launchLog -What "$command.exe launch log"
}

$portScanExe = $exePaths['port-scan']

# ===========================================================================
Write-Step 'loopback pipeline: generate-buckets -> scan (open vs closed)'
# One real listener is the only "service" in play. 127.0.0.1/32 is exempt from
# broadcast filtering, so the /32 expands to exactly the one host we control.
$openListener = New-LoopbackListener
try {
    $openPort = Get-ListenerPort -Listener $openListener
    $closedPorts = @(1..11 | ForEach-Object { Get-ReservedClosedPort })
    $allPorts = @($openPort) + $closedPorts
    Write-Host "open port  : $openPort"
    Write-Host "closed     : $($closedPorts -join ', ')"

    $cidrFile = Join-Path $workspace 'cidr.csv'
    $portFile = Join-Path $workspace 'ports.csv'
    Write-TextFile -Path $cidrFile -Text "fab_name,ip,ip_cidr,cidr_name`r`nfab1,127.0.0.1,127.0.0.1/32,loopback`r`n"
    $portFileText = (($allPorts | ForEach-Object { "$_/tcp" }) -join "`r`n") + "`r`n"
    Write-TextFile -Path $portFile -Text $portFileText

    # Every output directory below contains a space on purpose.
    $outA = Join-Path $workspace 'out a'
    New-Item -ItemType Directory -Force -Path $outA | Out-Null
    $bucketsA = Join-Path $outA 'buckets.json'
    $anchorA = Join-Path $outA 'scan_results.csv'

    Invoke-Native -Exe $portScanExe -Arguments @(
        'generate-buckets',
        '-cidr-file', $cidrFile,
        '-port-file', $portFile,
        '-buckets-out', $bucketsA,
        '-log-level', 'error'
    ) -LogPath (Join-Path $workspace 'generate-buckets-a.log') | Out-Null
    Assert-True (Test-Path -LiteralPath $bucketsA) 'generate-buckets wrote no bucket snapshot'
    Assert-HandleReleased -Path $bucketsA -What 'bucket snapshot after generate-buckets'

    Invoke-Native -Exe $portScanExe -Arguments @(
        'scan',
        '-cidr-file', $cidrFile,
        '-resume', $bucketsA,
        '-output', $anchorA,
        '-disable-api',
        '-workers', '4',
        '-delay', '0ms',
        '-timeout', '500ms',
        '-bucket-rate', '1000',
        '-bucket-capacity', '1000',
        '-log-level', 'error'
    ) -LogPath (Join-Path $workspace 'scan-a.log') | Out-Null

    $scanA = Get-SingleFile -Directory $outA -Filter 'scan_results-*.csv' -What 'scan (open/closed)'
    $openedA = Get-SingleFile -Directory $outA -Filter 'opened_results-*.csv' -What 'scan (open/closed)'
    Assert-HandleReleased -Path $scanA.FullName -What 'scan_results after scan'
    Assert-HandleReleased -Path $openedA.FullName -What 'opened_results after scan'

    $rowsA = @(Get-CsvDataRows -Path $scanA.FullName)
    Assert-True ($rowsA.Count -eq $allPorts.Count) "scan wrote $($rowsA.Count) data rows, expected $($allPorts.Count)"

    $openRows = @($rowsA | Where-Object { ($_ -split ',')[3] -eq 'open' })
    Assert-True ($openRows.Count -eq 1) "expected exactly 1 open row, got $($openRows.Count): $($openRows -join ' | ')"
    Assert-True ((($openRows[0] -split ',')[2]) -eq "$openPort") "the open row is for port $((($openRows[0] -split ',')[2])) but the listener is on $openPort"

    foreach ($closed in $closedPorts) {
        $row = @($rowsA | Where-Object { ($_ -split ',')[2] -eq "$closed" })
        Assert-True ($row.Count -eq 1) "expected exactly 1 row for unused port $closed, got $($row.Count)"
        Assert-True ((($row[0] -split ',')[3]) -ne 'open') "unused loopback port $closed was reported open"
    }

    $openedRows = @(Get-CsvDataRows -Path $openedA.FullName)
    Assert-True ($openedRows.Count -eq 1) "opened_results must hold exactly the 1 open row, got $($openedRows.Count)"
    Write-Host "  open/closed contract holds over $($allPorts.Count) loopback targets"

    # =======================================================================
    Write-Step 'loopback pipeline: abort -> scan -resume (append-reopen)'
    # Windows has no real SIGINT (os.Process.Signal(os.Interrupt) is
    # unsupported there), so the interruption is produced the way e2e's
    # failure-injection scenarios produce it: point -pressure-api at a refused
    # loopback port. The poller fails 3 times and the run aborts non-zero with a
    # resume snapshot on disk, mid-scan. The resumed run must then APPEND to the
    # same file: one header, every target exactly once.
    $deadPressurePort = Get-ReservedClosedPort
    $outB = Join-Path $workspace 'out b'
    New-Item -ItemType Directory -Force -Path $outB | Out-Null
    $bucketsB = Join-Path $outB 'buckets.json'
    $anchorB = Join-Path $outB 'scan_results.csv'

    Invoke-Native -Exe $portScanExe -Arguments @(
        'generate-buckets',
        '-cidr-file', $cidrFile,
        '-port-file', $portFile,
        '-buckets-out', $bucketsB,
        '-log-level', 'error'
    ) -LogPath (Join-Path $workspace 'generate-buckets-b.log') | Out-Null

    # bucket-rate 2 with 12 targets means the scan needs ~6s, while the pressure
    # poller turns fatal after 3 failed polls (~1.5s at 500ms) — the abort
    # always lands mid-scan, structurally rather than by timing luck.
    $abortCode = Invoke-Native -Exe $portScanExe -Arguments @(
        'scan',
        '-cidr-file', $cidrFile,
        '-resume', $bucketsB,
        '-output', $anchorB,
        '-pressure-api', "http://127.0.0.1:$deadPressurePort/api/pressure",
        '-pressure-interval', '500ms',
        '-workers', '1',
        '-bucket-rate', '2',
        '-bucket-capacity', '2',
        '-delay', '0ms',
        '-timeout', '200ms',
        '-log-level', 'error'
    ) -LogPath (Join-Path $workspace 'scan-b-abort.log') -ExpectFailure
    Write-Host "  aborted run exited $abortCode (expected non-zero)"

    Assert-True (Test-Path -LiteralPath $bucketsB) 'the aborted run left no resumable snapshot'
    $partialB = Get-SingleFile -Directory $outB -Filter 'scan_results-*.csv' -What 'aborted scan'
    Assert-HandleReleased -Path $partialB.FullName -What 'partial scan_results after the aborted run'
    Assert-HandleReleased -Path $bucketsB -What 'resume snapshot after the aborted run'
    $partialRows = @(Get-CsvDataRows -Path $partialB.FullName).Count
    Write-Host "  partial file $($partialB.Name) holds $partialRows of $($allPorts.Count) rows"

    Invoke-Native -Exe $portScanExe -Arguments @(
        'scan',
        '-cidr-file', $cidrFile,
        '-resume', $bucketsB,
        '-output', $anchorB,
        '-disable-api',
        '-workers', '4',
        '-delay', '0ms',
        '-timeout', '500ms',
        '-bucket-rate', '1000',
        '-bucket-capacity', '1000',
        '-log-level', 'error'
    ) -LogPath (Join-Path $workspace 'scan-b-resume.log') | Out-Null

    $finalB = Get-SingleFile -Directory $outB -Filter 'scan_results-*.csv' -What 'resumed scan'
    Assert-True ($finalB.Name -eq $partialB.Name) "resume wrote a new file '$($finalB.Name)' instead of appending to '$($partialB.Name)'"
    Assert-HandleReleased -Path $finalB.FullName -What 'scan_results after the resumed run'

    $allLines = @(Get-Content -LiteralPath $finalB.FullName | Where-Object { $_ -ne '' })
    $headerLines = @($allLines | Where-Object { $_ -like 'ip,ip_cidr,port,status*' })
    Assert-True ($headerLines.Count -eq 1) "append-reopen duplicated the CSV header: found $($headerLines.Count) header lines"

    $finalRows = @(Get-CsvDataRows -Path $finalB.FullName)
    Assert-True ($finalRows.Count -eq $allPorts.Count) "resume produced $($finalRows.Count) data rows, expected $($allPorts.Count) (rows were lost or duplicated across the abort)"
    $uniqueKeys = @($finalRows | ForEach-Object { $parts = $_ -split ','; "$($parts[0]):$($parts[2])" } | Sort-Object -Unique)
    Assert-True ($uniqueKeys.Count -eq $allPorts.Count) "expected $($allPorts.Count) distinct ip:port rows after resume, got $($uniqueKeys.Count)"
    Write-Host "  append-reopen holds: 1 header, $($finalRows.Count) distinct rows in $($finalB.Name)"
}
finally {
    $openListener.Stop()
}

# ===========================================================================
Write-Step 'delete the space-containing workspace (final handle check)'
if ($KeepWorkspace) {
    Write-Host "workspace kept at $workspace (-KeepWorkspace)"
}
else {
    # Recursive delete of a path with a space is the last handle assertion: if
    # any .exe still held an output or log file open, this throws.
    Remove-Item -LiteralPath $workspace -Recurse -Force
    Assert-True (-not (Test-Path -LiteralPath $workspace)) "the workspace '$workspace' could not be deleted; a file handle is still open"
    Write-Host "workspace deleted: $workspace"
}

Write-Host ''
Write-Host 'RESULT: native Windows gate passed.'
exit 0

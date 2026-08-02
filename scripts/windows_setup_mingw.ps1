#Requires -Version 5.1
<#
.SYNOPSIS
    Provision the native-Windows gate's build prerequisites (issue #63).

.DESCRIPTION
    scripts/windows_gate.ps1 needs two things that a bare `windows-latest` image
    does not reliably guarantee, and that issue #63 requires the repo to set up
    explicitly ("clear setup for x64 MinGW-w64 and ASCII temporary directories"):

      1. a 64-bit MinGW-w64 gcc (x86_64-w64-mingw32) on PATH — the race detector
         on windows/amd64 is implemented through cgo and cannot build without one;
      2. ASCII TEMP/TMP/GOTMPDIR — MSYS2 GCC fails to create its own temp files
         when those paths contain non-ASCII characters.

    Relying on whatever gcc the runner image happens to preinstall is exactly the
    image-dependence issue #63 forbids: it breaks silently the day GitHub changes
    the image. This script provisions the compiler itself (via Chocolatey, which
    ships on windows-latest) when a suitable one is not already present, and it
    FAILS LOUDLY (throws) if it cannot end with a genuine 64-bit gcc — it never
    lets the gate degrade to a non-race run.

    Both effects are exported to the GitHub Actions environment so the *next*
    workflow step (the gate) inherits them:
      - the compiler's directory is appended to $env:GITHUB_PATH;
      - TEMP/TMP/GOTMPDIR are written to $env:GITHUB_ENV.
    Run outside CI (no GITHUB_ENV/GITHUB_PATH) it still sets them for the current
    process, so a developer can run it once and then run the gate in the same
    shell.

.NOTES
    Companion to scripts/windows_gate.ps1. Prerequisites are documented in
    docs/MAINTENANCE.md section 2.1. The contract that this script keeps
    provisioning MinGW + ASCII temp is asserted by
    internal/ciguard/windows_setup_test.go inside `make verify` on every platform,
    so deleting the provisioning fails a normal `go test` rather than silently
    reverting the job to image-dependence.
#>
[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

if (Test-Path -LiteralPath 'variable:\PSNativeCommandUseErrorActionPreference') {
    $PSNativeCommandUseErrorActionPreference = $false
}

function Test-AsciiPath {
    param([AllowNull()][AllowEmptyString()][string]$Path)
    if ([string]::IsNullOrEmpty($Path)) { return $false }
    return ($Path -cmatch '^[\x20-\x7E]+$')
}

# Export-EnvVar sets a variable for THIS process and, when running under GitHub
# Actions, persists it to later steps via $GITHUB_ENV.
function Export-EnvVar {
    param(
        [Parameter(Mandatory)][string]$Name,
        [Parameter(Mandatory)][string]$Value
    )
    Set-Item -LiteralPath "env:$Name" -Value $Value
    if ($env:GITHUB_ENV) {
        "$Name=$Value" | Out-File -FilePath $env:GITHUB_ENV -Encoding utf8 -Append
    }
}

# Add-PathEntry prepends a directory to PATH for THIS process and, under GitHub
# Actions, to later steps via $GITHUB_PATH.
function Add-PathEntry {
    param([Parameter(Mandatory)][string]$Dir)
    $env:PATH = "$Dir;$env:PATH"
    if ($env:GITHUB_PATH) {
        $Dir | Out-File -FilePath $env:GITHUB_PATH -Encoding utf8 -Append
    }
}

# Get-X64Gcc returns the full path of a gcc.exe whose target triple starts with
# x86_64, or $null. It tests EACH candidate's own triple instead of stopping at
# the first gcc on PATH: a 32-bit (i686) gcc that happens to be first on PATH
# must NOT mask a 64-bit MinGW-w64 in one of the well-known install dirs (that
# would make the script throw even though a usable compiler is present, or right
# after Chocolatey installs one). A 32-bit compiler is rejected on purpose —
# it cannot build the race runtime for windows/amd64.
function Get-X64Gcc {
    $candidates = New-Object System.Collections.Generic.List[string]
    $onPath = Get-Command gcc -ErrorAction SilentlyContinue
    if ($onPath) { $candidates.Add($onPath.Source) }
    $candidateDirs = @(
        'C:\mingw64\bin',
        'C:\ProgramData\mingw64\mingw64\bin',
        'C:\ProgramData\chocolatey\lib\mingw\tools\install\mingw64\bin',
        'C:\msys64\mingw64\bin',
        'C:\Strawberry\c\bin'
    )
    foreach ($dir in $candidateDirs) {
        $exe = Join-Path $dir 'gcc.exe'
        if (Test-Path -LiteralPath $exe) { $candidates.Add($exe) }
    }
    foreach ($exe in $candidates) {
        $triple = (& $exe -dumpmachine 2>$null)
        if ($LASTEXITCODE -eq 0 -and "$triple".Trim() -match '^x86_64') {
            return $exe
        }
    }
    return $null
}

Write-Host '=== provision ASCII temporary directories ==='
# Always route the gate's temp at an ASCII path we own. Doing it unconditionally
# (rather than only when the current TEMP is non-ASCII) means the gate step is
# deterministic regardless of the runner's user profile.
$asciiTemp = Join-Path $env:SystemDrive 'port-scan-gate-tmp'
New-Item -ItemType Directory -Force -Path $asciiTemp | Out-Null
if (-not (Test-AsciiPath $asciiTemp)) {
    throw "windows setup: computed temp path '$asciiTemp' is not ASCII; set `$env:SystemDrive to an ASCII drive."
}
Export-EnvVar -Name 'TEMP' -Value $asciiTemp
Export-EnvVar -Name 'TMP' -Value $asciiTemp
Export-EnvVar -Name 'GOTMPDIR' -Value $asciiTemp
Write-Host "TEMP/TMP/GOTMPDIR -> $asciiTemp"

Write-Host ''
Write-Host '=== provision 64-bit MinGW-w64 (x86_64-w64-mingw32) ==='
$gccPath = Get-X64Gcc
if (-not $gccPath) {
    Write-Host 'no 64-bit gcc found; installing MinGW-w64 with Chocolatey...'
    $choco = Get-Command choco -ErrorAction SilentlyContinue
    if (-not $choco) {
        throw "windows setup: no 64-bit gcc and Chocolatey is unavailable to install MinGW-w64. Install a x86_64-w64-mingw32 gcc (e.g. MSYS2 'pacman -S mingw-w64-x86_64-gcc') and re-run."
    }
    & choco install mingw -y --no-progress
    if ($LASTEXITCODE -ne 0) {
        throw "windows setup: 'choco install mingw' failed (exit $LASTEXITCODE); cannot provision the 64-bit race compiler."
    }
    # choco updates the machine PATH but not this process; re-read it so Get-X64Gcc
    # can see the freshly installed compiler.
    $machinePath = [System.Environment]::GetEnvironmentVariable('Path', 'Machine')
    if ($machinePath) { $env:PATH = "$machinePath;$env:PATH" }
    $gccPath = Get-X64Gcc
}
if (-not $gccPath) {
    throw "windows setup: still no 64-bit x86_64-w64-mingw32 gcc after provisioning. The race detector on windows/amd64 REQUIRES one; this setup FAILS rather than letting the gate run without -race. See docs/MAINTENANCE.md section 2.1."
}

# Prepend the 64-bit compiler's directory to PATH so the gate step resolves `gcc`
# to it even when a 32-bit gcc is earlier on PATH, and turn cgo on there.
$gccDir = Split-Path -Parent $gccPath
Add-PathEntry -Dir $gccDir
Export-EnvVar -Name 'CGO_ENABLED' -Value '1'
$triple = (& $gccPath -dumpmachine).Trim()
Write-Host "gcc: $gccPath ($triple)"
Write-Host ''
Write-Host 'RESULT: Windows race prerequisites provisioned (64-bit MinGW-w64 + ASCII temp).'

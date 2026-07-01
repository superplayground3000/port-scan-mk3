<#
.SYNOPSIS
  Repeat a port scan over a user-defined set of ip:port targets, with two operator
  knobs: scan speed and whether to run the pre-scan ping.

.DESCRIPTION
  The Windows counterpart of linux/scan-loop.sh. It is a thin, dependency-free
  wrapper around this project's port-scan.exe binary: it turns a -Targets list into
  the cidr-file / port-file CSVs the binary expects, then drives the scan in a loop.
  All scan logic (TCP connect, pre-scan ICMP ping, leaky-bucket rate limiting) lives
  in the binary; this script only constructs the command line and repeats it.

  Target model: the scan covers the cross product of {distinct IPs} x {distinct
  ports} drawn from -Targets, matching port-scan's cidr-file x port-file contract.
  List one port per host and you get exactly those pairs.

  Each repeat writes its own batch under <Out>\rNN\ so rounds never collide.

.PARAMETER Targets
  Required. Comma-separated ip:port pairs, e.g. "10.0.0.1:80,10.0.0.2:443".

.PARAMETER Rate
  Scan speed: leaky-bucket tokens/sec (also burst). Default 100.
  Lower = slower/gentler; higher = faster.

.PARAMETER Workers
  Concurrent scan workers. Default 10.

.PARAMETER Ping
  Run the pre-scan ICMP ping; unreachable hosts are skipped. This is the default.

.PARAMETER NoPing
  Skip the pre-scan ping; every host is dialled directly. Overrides -Ping.

.PARAMETER PingTimeout
  Pre-scan ping budget per host, e.g. 100ms, 1s. Default 100ms.

.PARAMETER Timeout
  TCP dial timeout, e.g. 100ms, 2s. Default 100ms.

.PARAMETER Count
  Repeat the whole scan N times. Default 1.

.PARAMETER Interval
  Seconds to wait between repeats. Default 0.

.PARAMETER Out
  Output base directory. Default .\scan-out.

.PARAMETER Bin
  Path to the port-scan binary. Default: port-scan (on PATH).

.EXAMPLE
  .\scan-loop.ps1 -Targets "10.0.0.1:80,10.0.0.2:443" -Rate 50 -NoPing -Count 3 -Interval 5
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$Targets,
    [int]$Rate = 100,
    [int]$Workers = 10,
    [switch]$Ping,
    [switch]$NoPing,
    [string]$PingTimeout = '100ms',
    [string]$Timeout = '100ms',
    [int]$Count = 1,
    [int]$Interval = 0,
    [string]$Out = '.\scan-out',
    [string]$Bin = 'port-scan'
)

$ErrorActionPreference = 'Stop'

function Die([string]$msg) {
    Write-Error "scan-loop: $msg"
    exit 2
}

if ($Count -lt 1) { Die '-Count must be >= 1' }

# Pre-scan ping is on unless -NoPing is given. -Ping is accepted for symmetry with
# the Linux wrapper and may be passed to force the default explicitly.
if ($Ping.IsPresent -and $NoPing.IsPresent) { Die '-Ping and -NoPing are mutually exclusive' }
$pingEnabled = -not $NoPing.IsPresent

# --- split targets into unique IPs and unique ports (order-preserving) ---
$ips = [System.Collections.Generic.List[string]]::new()
$ports = [System.Collections.Generic.List[string]]::new()
$seenIp = @{}
$seenPort = @{}
foreach ($pair in ($Targets -split ',')) {
    $p = ($pair -replace '\s', '')
    if ([string]::IsNullOrEmpty($p)) { continue }
    $idx = $p.LastIndexOf(':')
    if ($idx -lt 1 -or $idx -eq ($p.Length - 1)) { Die "bad target '$p' (want ip:port)" }
    $ip = $p.Substring(0, $idx)
    $port = $p.Substring($idx + 1)
    if ($port -notmatch '^[0-9]+$') { Die "bad port in '$p'" }
    if (-not $seenIp.ContainsKey($ip)) { $seenIp[$ip] = $true; $ips.Add($ip) }
    if (-not $seenPort.ContainsKey($port)) { $seenPort[$port] = $true; $ports.Add($port) }
}
if ($ips.Count -eq 0) { Die 'no valid targets parsed' }

# --- materialise the CSVs the binary consumes ---
New-Item -ItemType Directory -Force -Path $Out | Out-Null
$cidrFile = Join-Path $Out '_targets-cidr.csv'
$portFile = Join-Path $Out '_targets-port.csv'
$cidrLines = @('ip,ip_cidr') + ($ips | ForEach-Object { "$_,$_/32" })
Set-Content -Path $cidrFile -Value $cidrLines -Encoding ascii
Set-Content -Path $portFile -Value ($ports | ForEach-Object { "$_/tcp" }) -Encoding ascii

$pingLabel = if ($pingEnabled) { 'on' } else { 'off' }
Write-Output "scan-loop: $($ips.Count) host(s) x $($ports.Count) port(s), rate=$Rate workers=$Workers ping=$pingLabel count=$Count"

# --- repeat loop ---
for ($n = 1; $n -le $Count; $n++) {
    $roundDir = Join-Path $Out ("r{0:D2}" -f $n)
    New-Item -ItemType Directory -Force -Path $roundDir | Out-Null
    Write-Output "scan-loop: round $n/$Count -> $roundDir"

    $scanArgs = @(
        'scan',
        '-cidr-file', $cidrFile,
        '-port-file', $portFile,
        '-output', (Join-Path $roundDir 'scan_results.csv'),
        '-bucket-rate', $Rate,
        '-bucket-capacity', $Rate,
        '-workers', $Workers,
        '-timeout', $Timeout,
        '-pre-scan-ping-timeout', $PingTimeout,
        '-disable-api',
        '-quiet'
    )
    if (-not $pingEnabled) { $scanArgs += '-disable-pre-scan-ping' }

    & $Bin @scanArgs
    if ($LASTEXITCODE -ne 0) { Die "port-scan exited $LASTEXITCODE on round $n" }

    if ($n -lt $Count -and $Interval -gt 0) { Start-Sleep -Seconds $Interval }
}

Write-Output "scan-loop: done ($Count round(s) under $Out)"

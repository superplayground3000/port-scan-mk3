#!/usr/bin/env bash
set -euo pipefail

profile="${1:-full}"
output_dir="${2:-performance-out/run-$(date -u +%Y%m%dT%H%M%SZ)-$$}"

if [[ "$(go env GOOS)" != "linux" || "$(go env GOARCH)" != "amd64" ]]; then
  echo "performance gate requires native linux/amd64" >&2
  exit 1
fi
if [[ "$profile" != "full" && "$profile" != "smoke" ]]; then
  echo "profile must be full or smoke" >&2
  exit 2
fi
if [[ -e "$output_dir" ]]; then
  echo "output path already exists: $output_dir" >&2
  exit 1
fi
if [[ ! -x /usr/bin/time ]]; then
  echo "/usr/bin/time is required for Linux process metrics" >&2
  exit 1
fi

parent_dir="$(dirname "$output_dir")"
mkdir -p "$parent_dir"
free_disk_bytes="$(df -PB1 "$parent_dir" | awk 'NR==2 {print $4}')"
required_disk_bytes=2000000000
if [[ "$profile" == "full" ]]; then
  required_disk_bytes=50000000000
fi
if (( free_disk_bytes < required_disk_bytes )); then
  echo "insufficient free space: have $free_disk_bytes bytes, require $required_disk_bytes bytes" >&2
  exit 1
fi

adapter_tmp="$(mktemp -d)"
time_log="$adapter_tmp/matrix-os-metrics.txt"
signal_log="$adapter_tmp/signal-cases.txt"
stdout_log="$adapter_tmp/stdout.log"
stderr_log="$adapter_tmp/stderr.log"
go test ./cmd/port-scan -run '^TestScanInterruptContext_OnLinux_' -count=1 -timeout=30s >"$signal_log" 2>&1
cpu="$(lscpu | awk -F: '/Model name/ {sub(/^[[:space:]]+/, "", $2); print $2; exit}')"
physical_cores="$(lscpu -p=CORE,SOCKET | awk '!/^#/ {seen[$1 ":" $2]=1} END {print length(seen)}')"
logical_cores="$(nproc)"
ram_bytes="$(awk '/MemTotal/ {print $2 * 1024}' /proc/meminfo | cut -d. -f1)"
filesystem="$(stat -f -c %T "$parent_dir")"
disk="$(lsblk -dn -o MODEL 2>/dev/null | awk 'NF {print; exit}')"
power_mode="unknown"
if [[ -r /sys/devices/system/cpu/cpu0/cpufreq/scaling_governor ]]; then
  power_mode="$(< /sys/devices/system/cpu/cpu0/cpufreq/scaling_governor)"
fi
commit="$(git rev-parse HEAD)"
evidence_label="hardware-qualified"
constraints="none recorded"
if [[ "${PERF_MINIMUM_PROFILE_CERTIFIED:-0}" == "1" ]]; then
  evidence_label="minimum-profile certified"
  constraints="8 physical cores, 16 logical cores, 32 GB RAM, SSD, and 50 GB free space"
fi

set +e
/usr/bin/time -v -o "$time_log" \
  go run ./internal/perfharness/cmd/perf-harness \
    -profile "$profile" \
    -output "$output_dir" \
    -evidence-label "$evidence_label" \
    -cpu "$cpu" \
    -physical-cores "$physical_cores" \
    -logical-cores "$logical_cores" \
    -power-mode "$power_mode" \
    -ram-bytes "$ram_bytes" \
    -filesystem "$filesystem" \
    -disk "${disk:-unknown}" \
    -free-disk-bytes "$free_disk_bytes" \
    -constraints "$constraints" \
    -commit "$commit" \
    > >(tee "$stdout_log") \
    2> >(tee "$stderr_log" >&2)
matrix_status=$?
stream_status=0
wait || stream_status=$?
if (( matrix_status == 0 && stream_status != 0 )); then
  matrix_status=$stream_status
fi
set -e

mv "$time_log" "$output_dir/matrix-os-metrics.txt"
mv "$signal_log" "$output_dir/signal-cases.txt"
mv "$stdout_log" "$output_dir/stdout.log"
mv "$stderr_log" "$output_dir/stderr.log"
rmdir "$adapter_tmp"
echo "Performance matrix artifacts: $output_dir"
exit "$matrix_status"

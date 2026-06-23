#!/usr/bin/env bash
# smoke-test.sh — property check run from the HOST. Executes the full CLI matrix inside the
# long-lived scanner container. Bounded by `timeout` so it never hangs (the matrix includes
# several deliberate multi-second pressure/resume scans, hence > the 60s default).
set -uo pipefail

echo "=== port-scan-mk3 CLI matrix smoke test ==="
RC=0
timeout 300s docker compose exec -T scanner bash /lab/scripts/run-matrix.sh || RC=$?

if [ "$RC" -ne 0 ]; then
  echo "smoke test FAILED (rc=$RC) — recent service logs:" >&2
  docker compose logs --tail=50
  exit 1
fi
echo "smoke test PASSED — all 36 CLI matrix cases observed expected output."
exit 0

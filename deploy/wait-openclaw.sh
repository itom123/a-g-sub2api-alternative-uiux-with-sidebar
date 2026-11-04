#!/bin/bash
set -euo pipefail
output="${1:?channel status output file required}"
for attempt in {1..40}; do
  "${OPENCLAW_BIN:-openclaw}" channels status --probe --json > "$output" || true
  if python3 - "$output" <<'PY'
import json,sys
try:
    status=json.load(open(sys.argv[1]))
    ready=status.get('channels',{}).get('telegram',{}).get('probe',{}).get('ok') is True
except (OSError,ValueError,AttributeError):
    ready=False
sys.exit(0 if ready else 1)
PY
  then exit 0; fi
  sleep 2
done
echo 'OpenClaw did not become ready before the startup deadline.' >&2
exit 1

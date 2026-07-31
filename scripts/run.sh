#!/usr/bin/env bash
# Run the comparison matrix: 4 regimes x 2 arms, one Job at a time.
#
# Strictly sequential. Two drivers running concurrently would contend for node
# CPU and for the single target, and the resulting numbers would measure the
# contention rather than the client libraries. The cost is wall-clock: at the
# default 5m ramp + 20m plateau that is 8 x 25m ~= 3h20m.
#
# Usage:
#   scripts/run.sh                      # full matrix, production settings
#   RAMP=30s PLATEAU=1m scripts/run.sh  # quick end-to-end rehearsal
#   REGIMES="h2 grpc" scripts/run.sh    # subset
set -euo pipefail

# Resolve the Python interpreter once.
#
# Bare `python` does not exist on Debian/Ubuntu or macOS (only `python3` does),
# and where it does exist it may be Python 2 — but on Windows/Git Bash the
# reverse is true: `python3` is often a Store stub and `python` is the real
# interpreter. Probe for python3 first, fall back to python, and fail loudly
# rather than dying at the first use under `set -e`.
PY="${PYTHON:-$(command -v python3 || command -v python || true)}"
if [ -z "$PY" ]; then
  echo "error: need Python 3 on PATH (set PYTHON=/path/to/python3)" >&2
  exit 1
fi

# envsubst renders the per-cell driver Job. It ships with GNU gettext and is
# absent by default on macOS and on minimal Linux images, so check up front
# instead of failing eight cells in.
if ! command -v envsubst >/dev/null 2>&1; then
  echo "error: envsubst not found (brew install gettext / apt install gettext-base)" >&2
  exit 1
fi

NS="${NS:-poseidon-bench}"
REGIMES="${REGIMES:-h1 h2 h3 grpc}"
ARMS="${ARMS:-poseidon standard}"
RPS="${RPS:-200}"
RAMP="${RAMP:-5m}"
PLATEAU="${PLATEAU:-20m}"
WORKERS="${WORKERS:-64}"
CONNS="${CONNS:-8}"
SEED="${SEED:-1}"
OUTDIR="${OUTDIR:-results}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"
mkdir -p "$OUTDIR"

# A run is only interpretable if both arms did the same work, so the same
# seed, rate, and duration are used for every cell — they are set once here
# rather than per-invocation.
echo "==> matrix: [$REGIMES] x [$ARMS] @ ${RPS}rps  ramp=$RAMP plateau=$PLATEAU seed=$SEED"

# Timeout generously exceeds ramp+plateau so a slow drain never truncates a run.
timeout_for_run() {
  "$PY" -c "
import re,sys
def secs(s):
    m=re.fullmatch(r'(\d+)([smh])', s)
    n,u=int(m.group(1)),m.group(2)
    return n*{'s':1,'m':60,'h':3600}[u]
print(int((secs('$RAMP')+secs('$PLATEAU'))*1.5)+120)
"
}
TIMEOUT="$(timeout_for_run)"

for regime in $REGIMES; do
  for arm in $ARMS; do
    job="driver-${regime}-${arm}"
    echo
    echo "==> $regime / $arm"

    kubectl -n "$NS" delete job "$job" --ignore-not-found >/dev/null 2>&1 || true

    REGIME="$regime" ARM="$arm" RPS="$RPS" RAMP="$RAMP" PLATEAU="$PLATEAU" \
      WORKERS="$WORKERS" CONNS="$CONNS" SEED="$SEED" \
      envsubst < deploy/k8s/20-driver-job.yaml | kubectl -n "$NS" apply -f -

    echo "    waiting (timeout ${TIMEOUT}s)"
    if ! kubectl -n "$NS" wait --for=condition=complete "job/$job" --timeout="${TIMEOUT}s"; then
      echo "    FAILED — recent logs:" >&2
      kubectl -n "$NS" logs "job/$job" --tail=30 >&2 || true
      echo "    continuing; report.py will flag this cell as missing" >&2
      continue
    fi

    # The report is a single marker-prefixed line. It must NOT be extracted as
    # a "from { to }" range: kubectl merges stdout and stderr, and a log line
    # landing between two lines of a pretty-printed document corrupts it —
    # observed in practice, on one cell out of eight.
    kubectl -n "$NS" logs "job/$job" \
      | grep '^POSEIDON_REPORT_JSON ' \
      | sed 's/^POSEIDON_REPORT_JSON //' \
      | tail -1 > "$OUTDIR/${regime}-${arm}.json"

    if [ ! -s "$OUTDIR/${regime}-${arm}.json" ]; then
      echo "    WARNING: empty report for $regime/$arm" >&2
    else
      "$PY" -c "
import json;d=json.load(open('$OUTDIR/${regime}-${arm}.json'))
print(f\"    reqs={d['plateau_requests']} rps={d['achieved_rps']:.1f} errs={d['plateau_errors']} \"
      f\"allocs/req={d['allocs_per_req']:.1f} mcores={d['cpu_millicores']:.1f}\")
"
    fi
  done
done

echo
echo "==> generating comparison table"
"$PY" scripts/report.py "$OUTDIR"

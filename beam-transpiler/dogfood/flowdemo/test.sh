#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

mkdir -p dist
got=$(../../bin/zc run --once . 2>dist/run.err)
want=$'processed=4\nsuccess=3\nfailed=1\nlast_error=negative score\nrestart=ok\nsuccess_file=true\nfailure_file=true\nhealth=ok\nstatus_has_processed=true'

if [ "$got" != "$want" ]; then
  echo "FAIL  flowdemo"
  echo "got:"; printf '%s\n' "$got" | sed 's/^/  /'
  echo "want:"; printf '%s\n' "$want" | sed 's/^/  /'
  echo "stderr:"; sed 's/^/  /' dist/run.err
  exit 1
fi

if ! grep -q "child_terminated" dist/run.err; then
  echo "FAIL  flowdemo restart evidence missing"
  sed 's/^/  /' dist/run.err
  exit 1
fi

echo "PASS  flowdemo"

#!/bin/sh
# Home fork: takes the documentation's pictures from the demo instance.
#
#   tools/home/screenshots/run.sh [demo-container] [login-file] [picture names...]
#
# The demo instance is a separate Mediarium (never a real household's) with a
# demo library; login-file holds "username: ..." and "password: ..." lines.
# The browser runs in the official Playwright image, sharing the demo
# container's network, so it reaches Mediarium at localhost:8264. Playwright's
# package is kept in a Docker volume, so it is fetched only the first time.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../../.." && pwd)
demo=${1:-mediarium-docs}
login=${2:-./docs-demo/login.txt}
[ $# -ge 2 ] && shift 2 || shift $#
user=$(sed -n 's/^username: //p' "$login")
pass=$(sed -n 's/^password: //p' "$login")
# A household member's login for the "viewer" pictures: login-<name>.txt next to it
viewer=$(ls "$(dirname "$login")"/login-*.txt 2>/dev/null | head -1 || true)
vuser=$([ -n "$viewer" ] && sed -n 's/^username: //p' "$viewer" || true)
vpass=$([ -n "$viewer" ] && sed -n 's/^password: //p' "$viewer" || true)
mkdir -p "$repo/docs/home/images"

MSYS_NO_PATHCONV=1 docker run --rm --network "container:$demo" \
   -e DEMO_USER="$user" -e DEMO_PASS="$pass" -e VIEWER_USER="$vuser" -e VIEWER_PASS="$vpass" -e BASE=http://localhost:8264 -e OUT=/out \
  -v "$repo/tools/home/screenshots:/shots:ro" -v "$repo/docs/home/images:/out" \
  -v mediarium-shots-npm:/work \
  mcr.microsoft.com/playwright:v1.47.2-jammy \
  sh -c 'cd /work && { [ -d node_modules/playwright ] || npm i --silent --no-audit --no-fund playwright@1.47.2 >/dev/null; } && cp /shots/shots.mjs /shots/shots.json . && node shots.mjs "$@"' sh "$@"

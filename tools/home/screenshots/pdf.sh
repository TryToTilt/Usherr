#!/bin/sh
# Home fork: builds the printable guide, docs/home/mediarium-home-guide.pdf,
# from the pages in docs/home and their pictures.
#
#   tools/home/screenshots/pdf.sh
set -eu
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../../.." && pwd)

MSYS_NO_PATHCONV=1 docker run --rm -i \
  -v "$here:/shots:ro" -v "$repo/docs/home:/docs" -v "$repo/web/public/fonts:/fonts:ro" -v mediarium-shots-npm:/work \
  mcr.microsoft.com/playwright:v1.47.2-jammy sh -s <<'EOF'
set -e
cd /work
[ -d node_modules/playwright ] || npm i --silent --no-audit --no-fund playwright@1.47.2 >/dev/null
grep -q '"version": "14.1.4"' node_modules/marked/package.json 2>/dev/null || npm i --silent --no-audit --no-fund marked@14.1.4 >/dev/null
cp /shots/pdf.mjs .
node pdf.mjs
EOF

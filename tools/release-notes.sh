#!/usr/bin/env bash
# Prints the GitHub release notes for one version: how to install or update,
# then that version's section of CHANGELOG.md. Used by the release workflow.
#
#   tools/release-notes.sh 2.1.0 > notes.md
set -euo pipefail

version="${1:?usage: tools/release-notes.sh <version>}"
cd "$(dirname "$0")/.."

section="$(awk -v v="${version}" '
  $0 ~ "^## \\[" v "\\]" { found = 1; next }
  found && /^## \[/ { exit }
  found { print }
' USHERR-CHANGELOG.md | sed -e '/./,$!d')" # Usherr: its own changelog

if [ -z "${section}" ]; then
  echo "No section for ${version} in USHERR-CHANGELOG.md." >&2
  exit 1
fi

cat <<EOF
**Install or update:** the Docker image is \`ghcr.io/trytotilt/usherr:${version}\` (also \`:latest\`), for \`linux/amd64\` and \`linux/arm64\`. A running Usherr offers this version under Settings > System, where **Update now** installs it. New here? Start with the [README](https://github.com/TryToTilt/Usherr#install).

${section}
EOF

#!/bin/sh
# Usherr: builds the public commit from the private branch.
#
#   tools/usherr/publish.sh <version> [source-branch] [upstream-tag]
#   tools/usherr/publish.sh 1.0.0 usherr v3.1.0
#
# The public branch ("public") has one commit per Usherr release, authored
# with the GitHub noreply address, on top of the Mediarium release it is
# built on (a merge commit when it moves to a newer Mediarium). The private
# history, working notes and personal tools listed in EXCLUDE never go into
# it. Nothing is pushed: check the branch, then push it yourself.
set -eu
version=${1:?usage: publish.sh <version> [source-branch] [upstream-tag]}
src=${2:-usherr}
upstream=${3:-v3.1.0}
name="TryToTilt"
email="118094643+TryToTilt@users.noreply.github.com"

EXCLUDE="
docs/home/UPSTREAM.md
tools/home/streamed_export.py
packaging/stores
branding/usherr/usherr-app-preview.png
branding/usherr/usherr-app-phone.png
branding/usherr/usherr-preview.png
branding/usherr/usherr-palette-preview.png
branding/usherr/usherr-u-crop.png
"

repo=$(git rev-parse --show-toplevel)
cd "$repo"
index=$(mktemp)
trap 'rm -f "$index"' EXIT
GIT_INDEX_FILE=$index git read-tree "$src"
for p in $EXCLUDE; do
	GIT_INDEX_FILE=$index git rm -r -q --cached --ignore-unmatch -- "$p"
done
tree=$(GIT_INDEX_FILE=$index git write-tree)

base=$(git rev-parse "$upstream^{commit}")
parents="-p $base"
if prev=$(git rev-parse -q --verify refs/heads/public); then
	parents="-p $prev"
	git merge-base --is-ancestor "$base" "$prev" || parents="-p $prev -p $base"
fi

msg="Usherr $version

Built on Mediarium ${upstream#v} by rdborg (https://github.com/rdborg/Mediarium)."
commit=$(GIT_AUTHOR_NAME=$name GIT_AUTHOR_EMAIL=$email GIT_COMMITTER_NAME=$name GIT_COMMITTER_EMAIL=$email \
	git commit-tree "$tree" $parents -m "$msg")
git update-ref refs/heads/public "$commit"
echo "public -> $commit (Usherr $version on Mediarium ${upstream#v})"

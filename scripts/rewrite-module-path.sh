#!/usr/bin/env bash
# Rewrites the upstream Go module path (github.com/junegunn/fzf) to this fork's
# module path. Idempotent: run it after every merge from upstream, since new or
# changed upstream files will reference the old import path.
#
#   git fetch upstream && git merge upstream/master
#   scripts/rewrite-module-path.sh
#   go build ./... && go test ./...

set -euo pipefail

OLD='github.com/junegunn/fzf'
NEW='github.com/DanSM-5/fzf.lib'

cd "$(git rev-parse --show-toplevel)"

# Module declaration
sed -i -E "s#^module ${OLD}\$#module ${NEW}#" go.mod

# Import paths in Go sources: "github.com/junegunn/fzf" and "github.com/junegunn/fzf/..."
git ls-files -z '*.go' | xargs -0 sed -i -E "s#\"${OLD}(/[^\"]*)?\"#\"${NEW}\\1\"#g"

# The new path sorts differently ("DanSM-5" < "charlievieth"), so re-sort imports
gofmt -l . | xargs -r gofmt -w

# Package lists in the Makefile (test target)
sed -i -E "s#${OLD}/src#${NEW}/src#g" Makefile

if grep -rn --include='*.go' "\"${OLD}" . >/dev/null; then
	echo "warning: references to ${OLD} remain:" >&2
	grep -rn --include='*.go' "\"${OLD}" . >&2
	exit 1
fi
echo "module path set to ${NEW}"

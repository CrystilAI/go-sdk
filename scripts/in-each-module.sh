#!/usr/bin/env bash
# Runs a command in each Go module here: the SDK at the root, plus examples/,
# integration/ and providertest/, which have their own go.mod files.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
for mod in $(git ls-files --cached --others --exclude-standard '*go.mod' | xargs -n1 dirname | sort -u); do
	echo "== $mod: $*"
	(cd "$mod" && "$@")
done

#!/usr/bin/env bash
#
# Print the CHANGELOG section for one version, which is what the GitHub release
# is made of.
#
# The release used to be described by its commit subjects, so the headline and
# the ⚠️ Behavior changes — the section people read before upgrading — reached
# only the file in the repository and never the page that newsletters, Homebrew
# users and watchers actually open. Failing loudly matters more than producing
# something: falling back to a commit list would hide the same problem again,
# quietly, on a release nobody reads until later.
#
# Usage: scripts/release-notes.sh 0.33.0 [CHANGELOG.md]

set -euo pipefail

version=${1:-}
changelog=${2:-CHANGELOG.md}

if [ -z "$version" ]; then
	echo "usage: $0 <version> [changelog]" >&2
	exit 2
fi

version=${version#v}

if [ ! -f "$changelog" ]; then
	echo "$changelog does not exist" >&2
	exit 1
fi

# From the heading for this version to the line before the next one, with the
# heading itself dropped: the release page already says which version it is.
section=$(awk -v want="## [$version]" '
	index($0, want) == 1 { found = 1; next }
	found && /^## \[/ { exit }
	found { print }
' "$changelog")

# Trim the blank lines the heading and the next section leave behind.
section=$(printf '%s\n' "$section" | sed -e '/./,$!d' | awk '{ lines[NR] = $0 } END { last = NR; while (last > 0 && lines[last] ~ /^[[:space:]]*$/) last--; for (i = 1; i <= last; i++) print lines[i] }')

if [ -z "$section" ]; then
	echo "$changelog has no section for $version, or it is empty." >&2
	echo "The release notes come from that section, so there is nothing to publish." >&2
	exit 1
fi

printf '%s\n' "$section"

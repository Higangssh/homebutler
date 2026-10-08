#!/usr/bin/env bash
# Checks that the release's long-lived credentials still work, before anything
# is published.
#
# v0.41.3 built its binaries, pushed its image and updated the tap, and then
# npm answered the publish with a 404: NPM_TOKEN had been set 93 days earlier,
# past npm's 90-day limit. The release stopped with half its channels out, the
# same state v0.41.0 was left in, and a tag cannot be published twice. A dead
# token is cheap to find at the start and expensive to find at the end.
#
# The release runs this first. .github/workflows/credentials.yml runs it on
# demand, so a replaced secret can be checked without cutting a tag.
#
# A credential that is not set is skipped, as the release itself skips that
# channel. A credential that is set and does not work fails.
set -euo pipefail

failed=0

if [ -z "${NODE_AUTH_TOKEN:-}" ]; then
  echo "npm: NPM_TOKEN not set, the release will skip npm"
else
  # Outside the checkout: the release refuses to build from a dirty tree.
  npmrc="$(mktemp)"
  trap 'rm -f "$npmrc"' EXIT
  echo "//registry.npmjs.org/:_authToken=${NODE_AUTH_TOKEN}" > "$npmrc"
  # One line: an ::error:: annotation shows only the first.
  if user="$(npm whoami --registry https://registry.npmjs.org/ --userconfig "$npmrc" 2>&1 | grep -v 'complete log' | tr '\n' ' ' | sed 's/ *$//'; exit "${PIPESTATUS[0]}")"; then
    echo "npm: token works, signed in as ${user}"
  else
    echo "::error::npm: NPM_TOKEN does not work (${user}). npm granular tokens expire after at most 90 days; replace the secret and run the Credentials workflow."
    failed=1
  fi
fi

if [ -z "${TAP_TOKEN:-}" ]; then
  echo "homebrew: HOMEBREW_TAP_TOKEN not set, the release will skip the tap"
else
  tap="${TAP_REPO:-Higangssh/homebrew-homebutler}"
  push="$(curl -fsS -H "Authorization: Bearer ${TAP_TOKEN}" -H "Accept: application/vnd.github+json" \
    "https://api.github.com/repos/${tap}" 2>/dev/null | jq -r '.permissions.push // false' || echo "unreachable")"
  if [ "$push" = "true" ]; then
    echo "homebrew: token can push to ${tap}"
  else
    echo "::error::homebrew: HOMEBREW_TAP_TOKEN cannot push to ${tap} (push=${push}). Replace the secret and run the Credentials workflow."
    failed=1
  fi
fi

exit "$failed"

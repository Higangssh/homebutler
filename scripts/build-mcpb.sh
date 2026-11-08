#!/bin/sh
# Packs one built binary into a Claude Desktop extension (.mcpb).
#
#   scripts/build-mcpb.sh <binary> <goos> <goarch> <version>
#
# GoReleaser runs this as a post hook on every build, so the bundles exist
# before anything is published and a failure here fails the release with
# nothing out. The bundle is written to dist/, where release.extra_files picks
# it up. Nothing is written to the checkout: GoReleaser refuses a dirty tree.
#
# A bundle carries a native binary, so there is one per platform. The spec has
# no field for the CPU architecture, so it is in the file name.
set -eu

if [ "$#" -ne 4 ]; then
  echo "usage: $0 <binary> <goos> <goarch> <version>" >&2
  exit 2
fi
binary=$1
goos=$2
goarch=$3
version=$4

root=$(cd "$(dirname "$0")/.." && pwd)
stage="$root/dist/mcpb/${goos}_${goarch}"
out="$root/dist/homebutler_${version}_${goos}_${goarch}.mcpb"

rm -rf "$stage"
mkdir -p "$stage/server"
cp "$binary" "$stage/server/homebutler"
chmod 755 "$stage/server/homebutler"
cp "$root/assets/icon-512.png" "$stage/icon.png"

# The version comes from the tag, as server.json's and the npm package's do,
# and the platform from the binary inside; the checked-in manifest holds
# neither. A client that checks compatibility.platforms can then turn away a
# Linux bundle on a Mac rather than install a binary that cannot run.
jq --arg v "$version" --arg p "$goos" \
  '.version = $v | .compatibility.platforms = [$p]' \
  "$root/mcpb/manifest.json" > "$stage/manifest.json"

# The release job installs this version globally; the fallback is for a local
# run. Pinned, so a new mcpb release cannot change what a tag produces.
if command -v mcpb > /dev/null 2>&1; then
  mcpb_cmd="mcpb"
else
  mcpb_cmd="npx -y @anthropic-ai/mcpb@2.1.2"
fi
$mcpb_cmd validate "$stage/manifest.json"
$mcpb_cmd pack "$stage" "$out"

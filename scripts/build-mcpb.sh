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

# Checked before anything is staged, so a missing tool names itself instead of
# surfacing as an error from halfway through the script.
if ! command -v jq > /dev/null 2>&1; then
  echo "$0: jq is required to stamp the manifest, and it is not on PATH" >&2
  exit 1
fi
if ! command -v mcpb > /dev/null 2>&1 && ! command -v npx > /dev/null 2>&1; then
  echo "$0: packing needs mcpb (npm install -g @anthropic-ai/mcpb@2.1.2) or Node's npx, and neither is on PATH" >&2
  exit 1
fi

root=$(cd "$(dirname "$0")/.." && pwd)
stage="$root/dist/mcpb/${goos}_${goarch}"
out="$root/dist/homebutler_${version}_${goos}_${goarch}.mcpb"

# The manifest names platforms as Node's process.platform does, so Windows is
# win32 there, and the packer rejects "windows".
case "$goos" in
  windows)
    platform=win32
    entry=server/homebutler.exe
    ;;
  *)
    platform=$goos
    entry=server/homebutler
    ;;
esac

rm -rf "$stage"
mkdir -p "$stage/server"
cp "$binary" "$stage/$entry"
chmod 755 "$stage/$entry"
cp "$root/assets/icon-512.png" "$stage/icon.png"

# The version comes from the tag, as server.json's and the npm package's do,
# and the platform from the binary inside; the checked-in manifest holds
# neither. A client that checks compatibility.platforms can then turn away a
# Linux bundle on a Mac rather than install a binary that cannot run.
#
# The Windows bundle is rewritten here rather than through platform_overrides
# in the manifest: Claude Desktop merges an override's env into the base env,
# so no override can take away the base PATH. That PATH is a Unix one, and on
# Windows it replaces the whole PATH, so homebutler would no longer find
# ssh.exe or anything in System32. The Windows bundle therefore has no env.
jq --arg v "$version" --arg p "$platform" --arg e "$entry" '
  .version = $v
  | .compatibility.platforms = [$p]
  | if $p == "win32" then
      .server.entry_point = $e
      | .server.mcp_config.command = "${__dirname}/" + $e
      | del(.server.mcp_config.env)
    else . end
' "$root/mcpb/manifest.json" > "$stage/manifest.json"

# Checked on the manifest that is packed, so an edit to the checked-in one
# that moved the binary, or a rewrite above that stopped applying, fails the
# build instead of shipping a bundle that cannot start or that hands Windows
# a Unix PATH.
staged=$(jq -r '[.server.entry_point, .server.mcp_config.command, (.server.mcp_config.env == null)] | @tsv' "$stage/manifest.json")
want_env=false
[ "$platform" = win32 ] && want_env=true
case "$staged" in
  "$entry	"'${__dirname}/'"$entry	$want_env") ;;
  *)
    echo "$0: the $platform bundle's manifest does not start $entry as expected (entry_point, command, env is null: $staged)" >&2
    exit 1
    ;;
esac

# The release job installs this version globally; the fallback is for a local
# run. Pinned, so a new mcpb release cannot change what a tag produces.
if command -v mcpb > /dev/null 2>&1; then
  mcpb_cmd="mcpb"
else
  mcpb_cmd="npx -y @anthropic-ai/mcpb@2.1.2"
fi
$mcpb_cmd validate "$stage/manifest.json"
$mcpb_cmd pack "$stage" "$out"

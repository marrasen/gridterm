#!/bin/sh
# Installs the newest kakel for you alone, with no root:
#
#   curl -fsSL https://raw.githubusercontent.com/marrasen/kakel/main/install.sh | sh
#
# It fetches the newest release, checks it against SHA256SUMS, and has
# kakel install itself into ~/.local/bin, with a desktop file.
set -eu

case "$(uname -s)/$(uname -m)" in
Linux/x86_64 | Linux/amd64) ;;
*)
	echo "kakel's releases are for Linux on amd64; this is $(uname -s) on $(uname -m)." >&2
	echo "Build it from source instead: https://github.com/marrasen/kakel/blob/main/BUILDING.md" >&2
	exit 1
	;;
esac

tag=$(curl -fsSL https://api.github.com/repos/marrasen/kakel/releases/latest |
	sed -n 's/^ *"tag_name": *"\([^"]*\)".*/\1/p' | head -n 1)
[ -n "$tag" ] || { echo "Couldn't find the newest release." >&2; exit 1; }
name="kakel_${tag}_linux_amd64.tar.gz"
base="https://github.com/marrasen/kakel/releases/download/$tag"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
echo "Fetching kakel $tag..."
curl -fsSL -o "$tmp/$name" "$base/$name"
curl -fsSL -o "$tmp/SHA256SUMS" "$base/SHA256SUMS"
(cd "$tmp" && grep " \*\{0,1\}$name\$" SHA256SUMS | sha256sum -c -)
tar xzf "$tmp/$name" -C "$tmp" kakel
"$tmp/kakel" -install

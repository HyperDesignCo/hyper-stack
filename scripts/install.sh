#!/bin/bash
# Hyper Stack installer for macOS (no administrator rights needed for this step).
#
# The repository is public: no GitHub account is needed.
#   curl -fsSL https://github.com/HyperDesignCo/hyper-stack/releases/latest/download/install.sh -o install.sh
#   bash install.sh --install
#
# Options:  --version v0.6.0   install a specific release (default: latest)
#           --autostart        start Hyper Stack when you log in
#           --install          also run "hyper install" (downloads about 400 MB)
#           --from <folder>    install from a local folder holding the release files
set -euo pipefail

repo="HyperDesignCo/hyper-stack"
version="latest"; autostart=0; doinstall=0; from=""
while [ $# -gt 0 ]; do
  case "$1" in
    --version) version="$2"; shift 2 ;;
    --autostart) autostart=1; shift ;;
    --install) doinstall=1; shift ;;
    --from) from="$2"; shift 2 ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done

[ "$(uname -s)" = "Darwin" ] || { echo "This installer is for macOS." >&2; exit 1; }
case "$(uname -m)" in
  arm64) asset="hyper-darwin-arm64" ;;
  x86_64) asset="hyper-darwin-amd64" ;;
  *) echo "Unsupported processor: $(uname -m)" >&2; exit 1 ;;
esac

tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT

if [ -n "$from" ]; then
  cp "$from/$asset" "$from/checksums.txt" "$tmp/"
elif command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then
  tag=(); [ "$version" != "latest" ] && tag=("$version")
  gh release download ${tag[@]+"${tag[@]}"} --repo "$repo" --pattern "$asset" --pattern checksums.txt --dir "$tmp"
else
  if [ "$version" = "latest" ]; then base="https://github.com/$repo/releases/latest/download"
  else base="https://github.com/$repo/releases/download/$version"; fi
  curl -fsSL "$base/$asset" -o "$tmp/$asset"
  curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt"
fi

want="$(tr -d '\r' < "$tmp/checksums.txt" | awk -v f="$asset" '$2==f {print $1}')"
got="$(shasum -a 256 "$tmp/$asset" | awk '{print $1}')"
[ -n "$want" ] && [ "$want" = "$got" ] || { echo "Checksum mismatch for $asset. Nothing was installed." >&2; exit 1; }

chmod +x "$tmp/$asset"
# A program downloaded with a browser carries a quarantine mark; ours came from curl or gh, but clear it anyway.
xattr -d com.apple.quarantine "$tmp/$asset" 2>/dev/null || true

args=(self-install)
[ "$autostart" = 1 ] && args+=(--autostart)
"$tmp/$asset" "${args[@]}"

hyper="$HOME/Library/Application Support/HyperStack/app/hyper"
echo
echo "Hyper Stack is installed. Open a new Terminal window, then run:  hyper install"
if [ "$doinstall" = 1 ]; then "$hyper" install; fi

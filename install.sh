#!/bin/sh
# pgproof installer — downloads the latest release binary for your OS/arch.
#   curl -fsSL https://raw.githubusercontent.com/shaxzodbek-uzb/pgproof/main/install.sh | sh
# Env overrides: PGPROOF_VERSION (tag), PGPROOF_INSTALL_DIR (target dir).
set -eu

REPO="shaxzodbek-uzb/pgproof"
INSTALL_DIR="${PGPROOF_INSTALL_DIR:-/usr/local/bin}"

err() { echo "pgproof-install: $*" >&2; exit 1; }

# --- detect platform ---------------------------------------------------------
os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$os" in
  linux|darwin) ;;
  *) err "unsupported OS '$os'. Grab a binary from https://github.com/$REPO/releases" ;;
esac
case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) err "unsupported architecture '$arch'" ;;
esac

# --- resolve version ---------------------------------------------------------
# Resolve "latest" from the releases/latest redirect target rather than the
# REST API, which rate-limits unauthenticated callers to 60 requests/hour/IP.
version="${PGPROOF_VERSION:-}"
if [ -z "$version" ]; then
  effective="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest")"
  version="${effective##*/tag/}"
  case "$version" in
    v[0-9]*) ;;
    *) err "could not determine latest version; set PGPROOF_VERSION (e.g. v0.1.0)" ;;
  esac
fi
num="${version#v}"

# --- download + extract ------------------------------------------------------
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
asset="pgproof_${num}_${os}_${arch}.tar.gz"
url="https://github.com/$REPO/releases/download/$version/$asset"

echo "Downloading $asset ..."
curl -fsSL "$url" -o "$tmp/$asset" || err "download failed: $url"
tar -xzf "$tmp/$asset" -C "$tmp" || err "extract failed"
[ -f "$tmp/pgproof" ] || err "binary not found in archive"

# --- install -----------------------------------------------------------------
if [ -w "$INSTALL_DIR" ]; then
  mv "$tmp/pgproof" "$INSTALL_DIR/pgproof"
else
  echo "Elevating to write to $INSTALL_DIR (set PGPROOF_INSTALL_DIR to avoid sudo)"
  sudo mv "$tmp/pgproof" "$INSTALL_DIR/pgproof"
fi
chmod +x "$INSTALL_DIR/pgproof" 2>/dev/null || sudo chmod +x "$INSTALL_DIR/pgproof"

echo "Installed pgproof $version to $INSTALL_DIR/pgproof"
"$INSTALL_DIR/pgproof" --version || true

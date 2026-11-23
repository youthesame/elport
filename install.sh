#!/bin/sh
set -eu

REPO="youthesame/elport"
INSTALL_DIR="${ELPORT_INSTALL_DIR:-$HOME/.local/bin}"
TAG="${ELPORT_VERSION:-}"

need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "error: required command not found: $1" >&2
    exit 1
  }
}

need curl
need tar

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
  darwin|linux) ;;
  *) echo "error: unsupported OS: $OS" >&2; exit 1 ;;
esac

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *) echo "error: unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

if [ -z "$TAG" ]; then
  FINAL_URL="$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest")"
  case "$FINAL_URL" in
    "https://github.com/${REPO}/releases/tag/"*)
      TAG="${FINAL_URL#"https://github.com/${REPO}/releases/tag/"}"
      ;;
    *)
      echo "error: could not determine latest release tag from ${FINAL_URL}" >&2
      exit 1
      ;;
  esac
fi

case "$TAG" in
  v*) ;;
  *) TAG="v${TAG}" ;;
esac

if ! printf '%s\n' "$TAG" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$'; then
  echo "error: invalid release tag: ${TAG}" >&2
  exit 1
fi

case "$TAG" in
  v0.1.*)
    echo "error: latest release ${TAG} is the Python v0.1.x line; pass a Go release tag via ELPORT_VERSION" >&2
    exit 1
    ;;
esac

VER="${TAG#v}"
ARCHIVE="elport_${VER}_${OS}_${ARCH}.tar.gz"
BASE_URL="https://github.com/${REPO}/releases/download/${TAG}"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

curl -fsSL "${BASE_URL}/${ARCHIVE}" -o "${TMP}/${ARCHIVE}"
curl -fsSL "${BASE_URL}/checksums.txt" -o "${TMP}/checksums.txt"

EXPECTED="$(awk -v f="$ARCHIVE" '$2 == f { print $1 }' "${TMP}/checksums.txt")"
if [ -z "$EXPECTED" ]; then
  echo "error: checksum entry not found for ${ARCHIVE}" >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL="$(sha256sum "${TMP}/${ARCHIVE}" | awk '{ print $1 }')"
elif command -v shasum >/dev/null 2>&1; then
  ACTUAL="$(shasum -a 256 "${TMP}/${ARCHIVE}" | awk '{ print $1 }')"
else
  echo "error: neither sha256sum nor shasum found to verify download" >&2
  exit 1
fi

if [ "$ACTUAL" != "$EXPECTED" ]; then
  echo "error: SHA-256 mismatch for ${ARCHIVE}" >&2
  exit 1
fi

tar -xzf "${TMP}/${ARCHIVE}" -C "$TMP" elport
mkdir -p "$INSTALL_DIR"
mv "${TMP}/elport" "${INSTALL_DIR}/elport"
chmod 755 "${INSTALL_DIR}/elport"

echo "Installed elport ${TAG} to ${INSTALL_DIR}/elport"
case ":${PATH:-}:" in
  *":${INSTALL_DIR}:"*) ;;
  *) echo "Add ${INSTALL_DIR} to PATH: export PATH=\"${INSTALL_DIR}:\$PATH\"" ;;
esac

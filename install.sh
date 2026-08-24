#!/bin/sh
# GlassMarble (gmb) installer for Linux and macOS
# Usage: curl -fsSL https://raw.githubusercontent.com/Syamchand123/GlassMarble/main/install.sh | sh

set -e

REPO="Syamchand123/GlassMarble"
BINARY_NAME="gmb"
ALIAS_NAME="glassmarble"

# Detect OS
OS="$(uname -s)"
case "${OS}" in
    Linux*)     OS_NAME="linux" ;;
    Darwin*)   OS_NAME="darwin" ;;
    *)
        echo "Error: Unsupported operating system ${OS}. Please build from source." >&2
        exit 1
        ;;
esac

# Detect Architecture
ARCH="$(uname -m)"
case "${ARCH}" in
    x86_64|amd64)   ARCH_NAME="amd64" ;;
    arm64|aarch64)  ARCH_NAME="arm64" ;;
    *)
        echo "Error: Unsupported architecture ${ARCH}. Please build from source." >&2
        exit 1
        ;;
esac

# Determine installation directory
if [ -n "${INSTALL_DIR}" ]; then
    DEST_DIR="${INSTALL_DIR}"
elif [ "$(id -u)" -eq 0 ]; then
    DEST_DIR="/usr/local/bin"
else
    DEST_DIR="${HOME}/.local/bin"
fi

mkdir -p "${DEST_DIR}"

# Fetch latest version tag if not specified
if [ -z "${VERSION}" ]; then
    VERSION="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/' || echo "v1.0.0")"
fi
CLEAN_VER="${VERSION#v}"

ARCHIVE_NAME="gmb_${CLEAN_VER}_${OS_NAME}_${ARCH_NAME}.tar.gz"
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/${ARCHIVE_NAME}"
CHECKSUM_URL="https://github.com/${REPO}/releases/download/${VERSION}/checksums.txt"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT INT TERM

echo "==> Downloading GlassMarble ${VERSION} (${OS_NAME}/${ARCH_NAME})..."
curl -fsSL "${DOWNLOAD_URL}" -o "${TMP_DIR}/${ARCHIVE_NAME}"

# Verify checksum if available
if curl -fsSL "${CHECKSUM_URL}" -o "${TMP_DIR}/checksums.txt" 2>/dev/null; then
    echo "==> Verifying SHA256 checksum..."
    (
        cd "${TMP_DIR}"
        if command -v sha256sum >/dev/null 2>&1; then
            grep "${ARCHIVE_NAME}" checksums.txt | sha256sum -c -
        elif command -v shasum >/dev/null 2>&1; then
            grep "${ARCHIVE_NAME}" checksums.txt | shasum -a 256 -c -
        fi
    )
fi

echo "==> Extracting binary..."
tar -xzf "${TMP_DIR}/${ARCHIVE_NAME}" -C "${TMP_DIR}"

# Install gmb and alias
install -m 755 "${TMP_DIR}/${BINARY_NAME}" "${DEST_DIR}/${BINARY_NAME}"
if [ -f "${TMP_DIR}/${ALIAS_NAME}" ]; then
    install -m 755 "${TMP_DIR}/${ALIAS_NAME}" "${DEST_DIR}/${ALIAS_NAME}"
else
    ln -sf "${DEST_DIR}/${BINARY_NAME}" "${DEST_DIR}/${ALIAS_NAME}" 2>/dev/null || true
fi

echo ""
echo "✓ GlassMarble installed successfully to ${DEST_DIR}/${BINARY_NAME}"

# Check PATH
case ":${PATH}:" in
    *:"${DEST_DIR}":*) ;;
    *)
        echo ""
        echo "Note: ${DEST_DIR} is not currently in your \$PATH."
        echo "Add it by running:"
        echo "  export PATH=\"${DEST_DIR}:\$PATH\""
        echo "Or add it to your ~/.bashrc or ~/.zshrc."
        ;;
esac

echo ""
"${DEST_DIR}/${BINARY_NAME}" version 2>/dev/null || true

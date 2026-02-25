#!/bin/sh
set -eu

: "${CDP_BINARY_PATH:?CDP_BINARY_PATH is required}"
: "${CDP_RUNTIME:?CDP_RUNTIME is required}"
: "${CDP_VERSION:?CDP_VERSION is required}"
: "${CDP_PACKAGE_NAME:?CDP_PACKAGE_NAME is required}"
: "${CDP_INSTALL_PREFIX:?CDP_INSTALL_PREFIX is required}"
: "${CDP_INPUT_MODE:?CDP_INPUT_MODE is required}"
: "${CDP_INPUT_PATH:?CDP_INPUT_PATH is required}"
: "${CDP_INPUT_SHA256:?CDP_INPUT_SHA256 is required}"

CDP_OUTPUT_DIR="${CDP_OUTPUT_DIR:-./packages}"
CDP_RELEASE="${CDP_RELEASE:-1}"
CDP_ARCH="${CDP_ARCH:-x86_64}"

apk add --no-cache bash tar coreutils findutils apk-tools go alpine-sdk >/dev/null
mkdir -p "${CDP_OUTPUT_DIR}"

exec "${CDP_BINARY_PATH}" --log-level error package apk \
  --runtime "${CDP_RUNTIME}" \
  --version "${CDP_VERSION}" \
  --package-name "${CDP_PACKAGE_NAME}" \
  --install-prefix "${CDP_INSTALL_PREFIX}" \
  --input-mode "${CDP_INPUT_MODE}" \
  --input-path "${CDP_INPUT_PATH}" \
  --input-sha256 "${CDP_INPUT_SHA256}" \
  --release "${CDP_RELEASE}" \
  --arch "${CDP_ARCH}" \
  --out-dir "${CDP_OUTPUT_DIR}" \
  --output json

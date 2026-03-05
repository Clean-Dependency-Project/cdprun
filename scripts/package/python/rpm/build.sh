#!/usr/bin/env bash
set -euo pipefail

source /workspace/scripts/package/common/runtime-contract.sh
cdp_require_vars \
  CDP_RUNTIME \
  CDP_VERSION \
  CDP_PACKAGE_NAME \
  CDP_INSTALL_PREFIX \
  CDP_INPUT_PATH \
  CDP_INPUT_SHA256

CDP_OUTPUT_DIR="${CDP_OUTPUT_DIR:-./packages}"
CDP_RELEASE="${CDP_RELEASE:-1}"
CDP_ARCH="${CDP_ARCH:-x86_64}"

start_ns="$(date +%s%N)"

input_path="$(cdp_workspace_path "${CDP_INPUT_PATH}")"

if [[ ! -f "${input_path}" ]]; then
  echo "missing Python source archive: ${input_path}" >&2
  exit 1
fi

actual_sha256="$(sha256sum "${input_path}" | awk '{print $1}')"
if [[ "${actual_sha256}" != "${CDP_INPUT_SHA256}" ]]; then
  echo "sha256 mismatch for ${input_path}: got ${actual_sha256}, expected ${CDP_INPUT_SHA256}" >&2
  exit 1
fi

echo "installing Python RPM build dependencies" >&2
yum install -y --allowerasing \
  rpm-build rpmdevtools tar gzip findutils coreutils bash \
  gcc gcc-c++ make openssl-devel bzip2-devel libffi-devel \
  zlib-devel readline-devel sqlite-devel ncurses-devel \
  xz-devel tk-devel gdbm-devel libuuid-devel >/dev/null
yum clean all >/dev/null

mkdir -p "${CDP_OUTPUT_DIR}"
rpmdev-setuptree >/dev/null

source_filename="$(basename "${input_path}")"
spec_path="${HOME}/rpmbuild/SPECS/python.spec"
source_target="${HOME}/rpmbuild/SOURCES/${source_filename}"
cp "/workspace/rpm/python.spec" "${spec_path}"
cp "${input_path}" "${source_target}"

echo "building Python RPM from ${source_filename}" >&2
rpmbuild -bb "${spec_path}" \
  --define "runtime_version ${CDP_VERSION}" \
  --define "runtime_release ${CDP_RELEASE}" \
  --define "runtime_arch ${CDP_ARCH}" \
  --define "package_name ${CDP_PACKAGE_NAME}" \
  --define "install_prefix ${CDP_INSTALL_PREFIX}" \
  --define "source_filename ${source_filename}" \
  --define "_smp_mflags -j1" \
  --define "_topdir ${HOME}/rpmbuild" \
  1>&2

rpm_file="$(find "${HOME}/rpmbuild/RPMS" -type f -name "*.rpm" | head -n 1)"
if [[ -z "${rpm_file}" ]]; then
  echo "rpmbuild completed but no RPM artifact was found" >&2
  exit 1
fi

out_file="${CDP_OUTPUT_DIR%/}/$(basename "${rpm_file}")"
cp "${rpm_file}" "${out_file}"
package_sha256="$(sha256sum "${out_file}" | awk '{print $1}')"

workspace_out_file="${out_file}"
package_path="$(cdp_workspace_relative "${workspace_out_file}")"

duration_ns="$(( $(date +%s%N) - start_ns ))"

cat <<EOF
{
  "runtime": "${CDP_RUNTIME}",
  "version": "${CDP_VERSION}",
  "package_type": "rpm",
  "package_name": "${CDP_PACKAGE_NAME}",
  "release": "${CDP_RELEASE}",
  "arch": "${CDP_ARCH}",
  "install_prefix": "${CDP_INSTALL_PREFIX}",
  "input": {
    "path": "${CDP_INPUT_PATH}",
    "sha256": "${CDP_INPUT_SHA256}"
  },
  "package_filename": "$(basename "${out_file}")",
  "package_path": "${package_path}",
  "package_sha256": "${package_sha256}",
  "duration": ${duration_ns}
}
EOF

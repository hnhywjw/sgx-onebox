#!/bin/bash
set -euo pipefail

BUNDLE_DIR="$(cd "$(dirname "$0")" && pwd)"
ARCH="${1:-linux-amd64}"
TARGET="${BUNDLE_DIR}/${ARCH}"

if [ ! -d "${TARGET}" ]; then
  echo "错误: 目标目录 ${TARGET} 不存在"
  echo "用法: $0 <linux-amd64|linux-arm64>"
  exit 1
fi

MANIFEST="${TARGET}/manifest.json"

echo "=== 生成 manifest.json (${ARCH}) ==="

sha_of() {
  local f="${TARGET}/${1}"
  if [ -f "${f}" ]; then
    sha256sum "${f}" | awk '{print $1}'
  else
    echo ""
  fi
}

FILES=("install.sh" "install-sgx.sh" "build-dcap-verify-quote.sh" "k3s-install.sh" "k3s" "kubectl" "crictl" "ctr" "k3s-airgap-images-amd64.tar.gz" "sgx-packages.tar.gz" "dcap-verify-quote")
ENTRIES=()
for f in "${FILES[@]}"; do
  h=$(sha_of "${f}")
  if [ -n "${h}" ]; then
    ENTRY="    \"${f}\": \"${h}\""
    ENTRIES+=("${ENTRY}")
    echo "  ${f}: ${h:0:16}..."
  else
    echo "  跳过 ${f}: 文件不存在"
  fi
done

if [ ${#ENTRIES[@]} -eq 0 ]; then
  echo ""
  echo "警告: 没有找到任何组件文件，manifest.json 将包含空的 sha256 映射"
  echo "  请先运行 fetch.sh ${ARCH} 下载组件"
fi

JOINED=""
for i in "${!ENTRIES[@]}"; do
  if [ "${i}" -gt 0 ]; then
    JOINED+=",
"
  fi
  JOINED+="${ENTRIES[${i}]}"
done

cat > "${MANIFEST}" << EOF
{
  "k3sVersion": "v1.30.4+k3s1",
  "runtimeVersion": "containerd://1.7.21-k3s1",
  "kubectlVersion": "v1.30.4",
  "sgxDriverVersion": "2.29.100.1",
  "dcapVersion": "1.26.100.1",
  "osFamily": ["linux", "ubuntu", "debian", "rhel", "centos", "rocky", "almalinux"],
  "sha256": {
${JOINED}
  }
}
EOF

echo ""
echo "=== 生成完成 ==="
echo "  ${MANIFEST}"

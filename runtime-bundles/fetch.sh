#!/bin/bash
set -euo pipefail

HAS_FAILURES=0

BUNDLE_DIR="$(cd "$(dirname "$0")" && pwd)"
ARCH="${1:-linux-amd64}"
TARGET="${BUNDLE_DIR}/${ARCH}"

if [ ! -d "${TARGET}" ]; then
  echo "错误: 目标目录 ${TARGET} 不存在"
  echo "用法: $0 <linux-amd64|linux-arm64>"
  exit 1
fi

echo "=== 下载运行时组件到 ${TARGET} ==="

K3S_VERSION="v1.30.4+k3s1"
KUBECTL_VERSION="v1.30.4"

mkdir -p "${TARGET}"

OS_ARCH="amd64"
if [ "${ARCH}" = "linux-arm64" ]; then
  OS_ARCH="arm64"
fi

echo ""
echo "--- K3s ${K3S_VERSION} ---"
K3S_BIN="${TARGET}/k3s"
if [ -f "${K3S_BIN}" ]; then
  echo "  已存在: ${K3S_BIN}"
else
  K3S_URL="https://github.com/k3s-io/k3s/releases/download/${K3S_VERSION}/k3s"
  if [ "${ARCH}" = "linux-arm64" ]; then
    K3S_URL="https://github.com/k3s-io/k3s/releases/download/${K3S_VERSION}/k3s-arm64"
  fi
  echo "  下载: ${K3S_URL}"
  curl -sfL "${K3S_URL}" -o "${K3S_BIN}" || { echo "  K3s 下载失败，请手动放置"; rm -f "${K3S_BIN}"; HAS_FAILURES=1; }
  if [ -f "${K3S_BIN}" ]; then
    chmod +x "${K3S_BIN}"
    echo "  K3s 下载完成 ($(du -h "${K3S_BIN}" | cut -f1))"
  fi
fi

echo ""
echo "--- kubectl ${KUBECTL_VERSION} ---"
KUBECTL_BIN="${TARGET}/kubectl"
if [ -f "${KUBECTL_BIN}" ]; then
  echo "  已存在: ${KUBECTL_BIN}"
else
  KUBECTL_URL="https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${OS_ARCH}/kubectl"
  echo "  下载: ${KUBECTL_URL}"
  curl -sfL "${KUBECTL_URL}" -o "${KUBECTL_BIN}" || { echo "  kubectl 下载失败，请手动放置"; rm -f "${KUBECTL_BIN}"; HAS_FAILURES=1; }
  if [ -f "${KUBECTL_BIN}" ]; then
    chmod +x "${KUBECTL_BIN}"
    echo "  kubectl 下载完成 ($(du -h "${KUBECTL_BIN}" | cut -f1))"
  fi
fi

echo ""
echo "--- install.sh ---"
INSTALL_SH="${TARGET}/install.sh"
cat > "${INSTALL_SH}" << 'INSTALL_SCRIPT'
#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
INSTALL_PREFIX="${INSTALL_PREFIX:-/usr/local/bin}"

echo "=== 安装 SGX OneBox 运行时依赖 ==="
echo "  安装目录: ${INSTALL_PREFIX}"
mkdir -p "${INSTALL_PREFIX}"

install_bin() {
  local src="$1" name="$2"
  if [ ! -f "${src}" ]; then
    echo "  跳过 ${name}: 文件不存在"
    return 0
  fi
  cp "${src}" "${INSTALL_PREFIX}/${name}"
  chmod +x "${INSTALL_PREFIX}/${name}"
  echo "  已安装: ${name} -> ${INSTALL_PREFIX}/${name}"
}

install_bin "${SCRIPT_DIR}/k3s" "k3s"
install_bin "${SCRIPT_DIR}/kubectl" "kubectl"

if [ -f "${SCRIPT_DIR}/crictl" ]; then
  install_bin "${SCRIPT_DIR}/crictl" "crictl"
fi

if [ -f "${SCRIPT_DIR}/ctr" ]; then
  install_bin "${SCRIPT_DIR}/ctr" "ctr"
fi

echo ""
echo "=== SGX/DCAP 安装 ==="
echo "  请根据目标节点发行版手动安装 SGX 驱动和 DCAP:"
echo "  Ubuntu 20.04/22.04:"
echo "    apt-get update && apt-get install -y sgx-aesm-service libsgx-dcap-default-qpl"
echo "  CentOS/RHEL 8/9:"
echo "    dnf install -y sgx-aesm-service libsgx-dcap-default-qpl"
echo ""

if [ -f "${SCRIPT_DIR}/dcap-verify-quote" ]; then
  install_bin "${SCRIPT_DIR}/dcap-verify-quote" "dcap-verify-quote"
fi

echo "=== 运行时依赖安装完成 ==="
INSTALL_SCRIPT
chmod +x "${INSTALL_SH}"
echo "  install.sh 已生成"

echo ""
echo "=== 下载完成 ==="
echo "  运行 generate-manifest.sh ${ARCH} 更新 manifest.json"
exit $HAS_FAILURES

#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
INSTALL_PREFIX="${INSTALL_PREFIX:-/usr/local/bin}"
K3S_IMAGES_DIR="/var/lib/rancher/k3s/agent/images"

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

echo ""
echo "--- K3s 运行时二进制 ---"
install_bin "${SCRIPT_DIR}/k3s" "k3s"
install_bin "${SCRIPT_DIR}/kubectl" "kubectl"

if [ -f "${SCRIPT_DIR}/crictl" ]; then
  install_bin "${SCRIPT_DIR}/crictl" "crictl"
fi

if [ -f "${SCRIPT_DIR}/ctr" ]; then
  install_bin "${SCRIPT_DIR}/ctr" "ctr"
fi

echo ""
echo "--- K3s Airgap 镜像 ---"
AIRGAP_IMAGES="${SCRIPT_DIR}/k3s-airgap-images-amd64.tar.gz"
if [ -f "${AIRGAP_IMAGES}" ]; then
  mkdir -p "${K3S_IMAGES_DIR}"
  cp "${AIRGAP_IMAGES}" "${K3S_IMAGES_DIR}/"
  echo "  已安装 airgap images -> ${K3S_IMAGES_DIR}/"
else
  echo "  跳过 airgap images: 文件不存在"
fi

echo ""
echo "--- SGX/DCAP 安装 ---"
if [ -f "${SCRIPT_DIR}/install-sgx.sh" ]; then
  echo "  检测到 SGX 离线包，开始安装..."
  bash "${SCRIPT_DIR}/install-sgx.sh" || echo "  SGX/DCAP 安装失败，继续后续步骤..."
else
  echo "  请根据目标节点发行版手动安装 SGX 驱动和 DCAP:"
  echo "  Ubuntu 20.04/22.04:"
  echo "    apt-get update && apt-get install -y sgx-aesm-service libsgx-dcap-default-qpl"
fi

if [ -f "${SCRIPT_DIR}/dcap-verify-quote" ]; then
  install_bin "${SCRIPT_DIR}/dcap-verify-quote" "dcap-verify-quote"
elif [ -f "${SCRIPT_DIR}/build-dcap-verify-quote.sh" ]; then
  echo "  dcap-verify-quote 未预编译，请运行 build-dcap-verify-quote.sh 编译安装"
fi

echo ""
echo "--- K3s 服务安装 (离线模式) ---"
if [ "${INSTALL_K3S_SKIP_DOWNLOAD:-}" = "true" ] && [ -f "${SCRIPT_DIR}/k3s-install.sh" ]; then
  echo "  使用内置 install.sh 安装 K3s 服务..."
  export INSTALL_K3S_SKIP_DOWNLOAD=true
  export INSTALL_K3S_VERSION="${INSTALL_K3S_VERSION:-v1.30.4+k3s1}"
  export K3S_URL="${K3S_URL:-}"
  export K3S_TOKEN="${K3S_TOKEN:-}"
  if [ -z "${INSTALL_K3S_EXEC:-}" ]; then
    if [ -n "${K3S_URL:-}" ]; then
      export INSTALL_K3S_EXEC="agent"
    else
      export INSTALL_K3S_EXEC="server"
    fi
  fi
  bash "${SCRIPT_DIR}/k3s-install.sh"
  echo "  K3s 服务已安装并启动"
elif [ "${INSTALL_K3S_SKIP_DOWNLOAD:-}" = "true" ]; then
  echo "  错误: k3s-install.sh 不在资源包中，无法离线安装 K3s"
  exit 1
else
  echo "  非 K3s 安装模式 (INSTALL_K3S_SKIP_DOWNLOAD 未设置)"
  echo "  跳过 K3s 服务注册，仅复制二进制文件"
fi

echo ""
echo "=== 运行时依赖安装完成 ==="

#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SGX_PKGS="${SCRIPT_DIR}/sgx-packages"
SGX_EXTRACT_DIR=""
cleanup_extracted() {
  if [ -n "${SGX_EXTRACT_DIR}" ] && [ -d "${SGX_PKGS}" ]; then
    rm -rf "${SGX_PKGS}"
  fi
}
trap cleanup_extracted EXIT

echo "============================================"
echo "  Intel SGX / DCAP 离线安装"
echo "============================================"

# Step 1: Check kernel SGX support
echo ""
echo "[1/4] 检查内核 SGX 支持..."

SGX_READY=0
if [ -c /dev/sgx_enclave ] && [ -c /dev/sgx_provision ]; then
  echo "  SGX 内核驱动已就绪"
  SGX_READY=1
elif grep -q sgx /proc/cpuinfo 2>/dev/null; then
  echo "  CPU 支持 SGX, 但内核驱动未加载"
  echo "  请确保: BIOS 已启用 SGX, 内核 >=5.11 且加载了 intel_sgx 模块"
  echo "  加载模块: modprobe intel_sgx"
else
  echo "  警告: CPU 不支持 SGX 或未在 BIOS 中启用"
  echo "  SGX 用户态组件仍可安装, 但运行时需要 SGX 硬件"
fi

# Step 2: Install SGX user-space components
echo ""
echo "[2/4] 安装 SGX 用户态组件..."

SGX_EXTRACT_DIR=""
if { [ ! -d "${SGX_PKGS}" ] || ! ls "${SGX_PKGS}"/*.deb >/dev/null 2>&1; } && [ -f "${SGX_PKGS}.tar.gz" ]; then
  SGX_EXTRACT_DIR="${SGX_PKGS}"
  SGX_PKGS="${SCRIPT_DIR}/sgx-packages-extracted"
  mkdir -p "${SGX_PKGS}"
  tar -xzf "${SGX_EXTRACT_DIR}.tar.gz" -C "${SGX_PKGS}"
  echo "  已从 sgx-packages.tar.gz 解压 SGX 包"
fi

if [ -d "${SGX_PKGS}" ] && ls "${SGX_PKGS}"/*.deb >/dev/null 2>&1; then
  if ! dpkg -i "${SGX_PKGS}"/*.deb 2>&1; then
    echo "  修复依赖..."
    apt-get update -qq 2>/dev/null || true
    apt-get install -f -y 2>/dev/null || true
    if ! dpkg -i "${SGX_PKGS}"/*.deb 2>&1; then
      echo "  SGX 用户态组件安装失败，请检查依赖并重试"
      exit 1
    fi
  fi
  echo "  SGX 用户态组件安装完成"
else
  echo "  sgx-packages 目录为空, 跳过"
  echo "  在线安装: apt-get install -y sgx-aesm-service libsgx-dcap-default-qpl"
fi

# Step 3: Install build tools
echo ""
echo "[3/4] 安装编译工具链..."

if command -v cmake >/dev/null 2>&1 && command -v gcc >/dev/null 2>&1 && command -v g++ >/dev/null 2>&1; then
  echo "  编译工具链已就绪"
else
  echo "  安装 build-essential cmake git..."
  apt-get update -qq 2>/dev/null || true
  apt-get install -y build-essential cmake git 2>/dev/null && {
    echo "  编译工具链安装完成"
  } || {
    echo "  警告: 编译工具安装失败, 请手动安装后运行 build-dcap-verify-quote.sh"
  }
fi

# Step 4: Start AESM service
echo ""
echo "[4/4] 启动 AESM 服务..."

if command -v aesm_service >/dev/null 2>&1; then
  if systemctl is-active --quiet aesmd 2>/dev/null; then
    echo "  aesmd 服务已在运行"
  else
    systemctl start aesmd 2>/dev/null && echo "  aesmd 服务已启动" || echo "  请手动启动: systemctl start aesmd"
  fi
else
  echo "  警告: aesm_service 未找到"
fi

echo ""
echo "============================================"
echo "  SGX 安装完成"
echo "============================================"
echo ""
echo "  验证:"
echo "    dmesg | grep -i sgx"
echo "    systemctl status aesmd"
echo ""
echo "  下一步: 运行 build-dcap-verify-quote.sh 编译 dcap-verify-quote"
echo "============================================"

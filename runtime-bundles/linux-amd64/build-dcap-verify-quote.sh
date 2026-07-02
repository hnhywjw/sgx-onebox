#!/bin/bash
# 完整编译安装 dcap-verify-quote
# 包含 Intel SGX SDK 下载、DCAP 示例编译、安装
set -euo pipefail

echo "============================================"
echo "  编译 dcap-verify-quote"
echo "============================================"
echo ""

SGX_SDK_VERSION="2.25.100.3"
SGX_SDK_INSTALLER="sgx_linux_x64_sdk_${SGX_SDK_VERSION}.bin"
SGX_SDK_URL="https://download.01.org/intel-sgx/sgx-linux/2.25/distro/ubuntu22.04-server/${SGX_SDK_INSTALLER}"
SGX_SDK_DIR="/opt/intel/sgxsdk"
WORKDIR="${WORKDIR:-/tmp/dcap-build}"

# Step 1: Install prerequisites
echo "[1/6] 检查编译依赖..."
MISSING=""
for cmd in cmake gcc g++ git wget; do
  if ! command -v "$cmd" >/dev/null 2>&1; then
    MISSING="$MISSING $cmd"
  fi
done
if [ -n "$MISSING" ]; then
  echo "  安装缺失依赖:$MISSING"
  apt-get update -qq 2>/dev/null || true
  apt-get install -y build-essential cmake git wget 2>/dev/null || {
    echo "  错误: 依赖安装失败"
    exit 1
  }
fi
echo "  编译依赖已就绪"

# Step 2: Check SGX runtime packages
echo ""
echo "[2/6] 检查 SGX DCAP 开发库..."

if ! dpkg -l libsgx-dcap-quote-verify-dev 2>/dev/null | grep -q "^ii"; then
  SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
  if [ -d "${SCRIPT_DIR}/sgx-packages" ]; then
    echo "  安装 SGX 开发包..."
    for deb in "${SCRIPT_DIR}/sgx-packages/"*dev*.deb; do
      [ -f "$deb" ] && dpkg -i "$deb" 2>/dev/null || true
    done
  fi
  if ! dpkg -l libsgx-dcap-quote-verify-dev 2>/dev/null | grep -q "^ii"; then
    echo "  警告: libsgx-dcap-quote-verify-dev 未安装"
    echo "  请先运行 install-sgx.sh 安装 SGX 运行时"
    echo "  或在线安装: apt-get install -y libsgx-dcap-quote-verify-dev"
  fi
fi
echo "  DCAP 开发库检查完成"

# Step 3: Install Intel SGX SDK
echo ""
echo "[3/6] 安装 Intel SGX SDK ${SGX_SDK_VERSION}..."

if [ -d "${SGX_SDK_DIR}" ] && [ -f "${SGX_SDK_DIR}/include/sgx_error.h" ]; then
  echo "  Intel SGX SDK 已安装: ${SGX_SDK_DIR}"
else
  mkdir -p "${WORKDIR}"
  cd "${WORKDIR}"

  if [ ! -f "${SGX_SDK_INSTALLER}" ]; then
    echo "  下载 Intel SGX SDK..."
    wget -q --show-progress "${SGX_SDK_URL}" -O "${SGX_SDK_INSTALLER}" || {
      echo "  错误: SDK 下载失败"
      echo "  手动下载: ${SGX_SDK_URL}"
      echo "  放到 ${WORKDIR}/${SGX_SDK_INSTALLER} 后重新运行"
      exit 1
    }
  else
    echo "  SDK 安装包已存在"
  fi

  chmod +x "${SGX_SDK_INSTALLER}"
  echo "  安装 SDK 到 ${SGX_SDK_DIR}..."

  # Intel SGX SDK installer is interactive, pipe 'yes' for all prompts
  # 'yes' exits with 141 (SIGPIPE) when installer closes stdin; wrap to suppress pipefail
  { yes 2>/dev/null || true; } | ./"${SGX_SDK_INSTALLER}" --prefix "${SGX_SDK_DIR}" 2>&1

  if [ ! -f "${SGX_SDK_DIR}/include/sgx_error.h" ]; then
    echo "  错误: SDK 安装失败"
    exit 1
  fi
  echo "  Intel SGX SDK 安装完成"
fi

# Source SDK environment
if [ -f "${SGX_SDK_DIR}/environment" ]; then
  # shellcheck disable=SC1091
  source "${SGX_SDK_DIR}/environment"
fi

# Step 4: Clone Intel DCAP samples
echo ""
echo "[4/6] 获取 Intel DCAP 示例代码..."

mkdir -p "${WORKDIR}"
cd "${WORKDIR}"

if [ ! -d "SGXDataCenterAttestationPrimitives" ]; then
  git clone --depth 1 https://github.com/intel/SGXDataCenterAttestationPrimitives.git 2>&1 | tail -3
else
  echo "  DCAP 源码已存在"
fi

# Step 5: Build dcap-verify-quote
echo ""
echo "[5/6] 编译 dcap-verify-quote (QVL_ONLY 模式)..."

SAMPLE_DIR="${WORKDIR}/SGXDataCenterAttestationPrimitives/SampleCode/QuoteVerificationSample"
if [ ! -d "${SAMPLE_DIR}" ]; then
  echo "  错误: 示例目录不存在: ${SAMPLE_DIR}"
  exit 1
fi

cd "${SAMPLE_DIR}"

# Build with QVL_ONLY=1 (no enclave needed, quote verification via library only)
# Set SGX_QPL_LOGGING=0 to avoid dcap_quoteprov dependency
# Set SGX_SDK to the installed path
make clean 2>/dev/null || true
SGX_SDK="${SGX_SDK_DIR}" QVL_ONLY=1 SGX_QPL_LOGGING=0 make -j"$(nproc)" 2>&1 | tail -10

if [ ! -f "app" ]; then
  echo ""
  echo "  错误: 编译失败"
  echo "  请检查 SGX SDK 是否正确安装: ls ${SGX_SDK_DIR}/include/sgx_error.h"
  exit 1
fi

echo ""
echo "  编译成功"

# Step 6: Install
echo ""
echo "[6/6] 安装到系统..."

INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
mkdir -p "${INSTALL_DIR}"
cp app "${INSTALL_DIR}/dcap-verify-quote"
chmod +x "${INSTALL_DIR}/dcap-verify-quote"

echo ""
echo "============================================"
echo "  dcap-verify-quote 安装完成"
echo "============================================"
echo ""
echo "  安装路径: ${INSTALL_DIR}/dcap-verify-quote"
echo "  验证:"
if "${INSTALL_DIR}/dcap-verify-quote" --version 2>&1; then
  echo "    --version 检查通过"
else
  echo "    (运行环境无 SGX 硬件, 二进制已就绪)"
fi
echo ""
echo "============================================"

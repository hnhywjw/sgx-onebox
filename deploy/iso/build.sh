#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
OUTPUT_DIR="$ROOT_DIR/out/iso"
WORK_DIR="$OUTPUT_DIR/.work"
ISO_CACHE="$ROOT_DIR/out/.cache"

UBUNTU_VER="22.04.5"
ISO_FILE="ubuntu-${UBUNTU_VER}-live-server-amd64.iso"
ISO_URL="https://releases.ubuntu.com/${UBUNTU_VER}/${ISO_FILE}"

ORIG_DIR="$WORK_DIR/orig"
OUTPUT_ISO="$OUTPUT_DIR/sgx-onebox-$(date +%Y%m%d).iso"
PAYLOAD_DIR="$WORK_DIR/payload"

echo "=== SGX OneBox ISO Builder ==="

# --------------------------------------------------
# 0. Clean and setup
# --------------------------------------------------
mkdir -p "$OUTPUT_DIR" "$WORK_DIR" "$ORIG_DIR" "$PAYLOAD_DIR" "$ISO_CACHE"

# --------------------------------------------------
# 1. Build platform artifacts
# --------------------------------------------------
echo "=== 1. 构建平台组件 ==="

# 1a. Build backend binary
echo "    1a. 编译后端..."
cd "$ROOT_DIR"
go build -ldflags="-s -w" -o "$PAYLOAD_DIR/platform-api" ./cmd/platform-api/
echo "        $(du -h "$PAYLOAD_DIR/platform-api" | cut -f1)"

# 1b. Build frontend
echo "    1b. 构建前端..."
cd "$ROOT_DIR/apps/web"
if [ ! -d "node_modules" ]; then
  npm install --silent
fi
npm run build --silent
mkdir -p "$PAYLOAD_DIR/web"
cp -r dist/* "$PAYLOAD_DIR/web/"
echo "        $(du -sh dist | cut -f1)"

# 1c. Copy runtime-bundles for offline K3s/SGX installation
echo "    1c. 复制运行时资源包..."
BUNDLE_SRC="$ROOT_DIR/runtime-bundles"
if [ -d "$BUNDLE_SRC/linux-amd64" ]; then
  mkdir -p "$PAYLOAD_DIR/runtime-bundles"
  cp -r "$BUNDLE_SRC/linux-amd64" "$PAYLOAD_DIR/runtime-bundles/"
  find "$PAYLOAD_DIR/runtime-bundles" -name 'fetch.sh' -o -name 'generate-manifest.sh' | xargs rm -f 2>/dev/null || true
  BUNDLE_COUNT=$(find "$PAYLOAD_DIR/runtime-bundles" -name manifest.json | wc -l)
  echo "        ${BUNDLE_COUNT} 个运行时包"
else
  echo "        警告: runtime-bundles 不存在"
fi

# 1d. Copy configs
echo "    1d. 复制配置..."
cp "$ROOT_DIR/deploy/systemd/platform-api.service" "$PAYLOAD_DIR/"
cp "$ROOT_DIR/deploy/iso/platform.env.example" "$PAYLOAD_DIR/platform.env"

# --------------------------------------------------
# 2. Create install script (runs inside target system during autoinstall)
# --------------------------------------------------
echo "=== 2. 生成安装脚本 ==="
cat > "$PAYLOAD_DIR/install.sh" << 'INSTALL_SCRIPT'
#!/bin/bash
set -euo pipefail

INSTALL_DIR="/opt/sgx-onebox"
LOG="/var/log/sgx-onebox-install.log"
SRC="/cdrom/payload"

exec > >(tee -a "$LOG") 2>&1
echo "SGX OneBox 安装开始 $(date)"

# ---- 2.1 Create directories ----
mkdir -p "$INSTALL_DIR"/{bin,web,config,data,runtime-bundles}

# ---- 2.2 Install platform binary ----
if [ -f "$SRC/platform-api" ]; then
  cp "$SRC/platform-api" "$INSTALL_DIR/bin/"
  chmod +x "$INSTALL_DIR/bin/platform-api"
fi

# ---- 2.3 Install frontend ----
if [ -d "$SRC/web" ] && [ -f "$SRC/web/index.html" ]; then
  cp -r "$SRC/web"/* "$INSTALL_DIR/web/"
fi

# ---- 2.4 Install runtime bundles ----
if [ -d "$SRC/runtime-bundles" ]; then
  cp -r "$SRC/runtime-bundles"/* "$INSTALL_DIR/runtime-bundles/"
fi

# ---- 2.5 Configure environment ----
cp "$SRC/platform.env" "$INSTALL_DIR/config/"
if grep -q 'replace-with-strong-random-secret' "$INSTALL_DIR/config/platform.env" 2>/dev/null || ! grep -q '^PLATFORM_SECRET=' "$INSTALL_DIR/config/platform.env"; then
  NEW_SECRET=$(tr -dc 'A-Za-z0-9' </dev/urandom | head -c 48)
  if grep -q '^PLATFORM_SECRET=' "$INSTALL_DIR/config/platform.env" 2>/dev/null; then
    sed -i "s#^PLATFORM_SECRET=.*#PLATFORM_SECRET=$NEW_SECRET#" "$INSTALL_DIR/config/platform.env"
  else
    echo "PLATFORM_SECRET=$NEW_SECRET" >> "$INSTALL_DIR/config/platform.env"
  fi
fi

# ---- 2.6 Nginx ----
apt-get install -y nginx 2>/dev/null || true
cat > /etc/nginx/sites-available/sgx-onebox << 'NGX'
server {
    listen 80;
    server_name _;
    root /opt/sgx-onebox/web;
    index index.html;
    location / {
        try_files $uri $uri/ /index.html;
    }
    location /api/ {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
}
NGX
ln -sf /etc/nginx/sites-available/sgx-onebox /etc/nginx/sites-enabled/
rm -f /etc/nginx/sites-enabled/default
nginx -t && systemctl enable nginx 2>/dev/null || true

# ---- 2.7 Systemd service ----
cp "$SRC/platform-api.service" /etc/systemd/system/
systemctl daemon-reload
systemctl enable platform-api 2>/dev/null || true

# ---- 2.8 Install K3s (offline) ----
BUNDLE_DIR="$SRC/runtime-bundles/linux-amd64"
if [ -d "$BUNDLE_DIR" ] && [ -f "$BUNDLE_DIR/install.sh" ]; then
  cp "$BUNDLE_DIR/k3s" /usr/local/bin/ 2>/dev/null && chmod +x /usr/local/bin/k3s
  cp "$BUNDLE_DIR/kubectl" /usr/local/bin/ 2>/dev/null && chmod +x /usr/local/bin/kubectl
  cp "$BUNDLE_DIR/k3s-install.sh" /usr/local/bin/ 2>/dev/null && chmod +x /usr/local/bin/k3s-install.sh
  INSTALL_K3S_SKIP_DOWNLOAD=true INSTALL_K3S_EXEC="server --disable=traefik" bash "$BUNDLE_DIR/install.sh"
  systemctl enable k3s 2>/dev/null || true
fi

# ---- 2.9 Install SGX (offline) ----
if [ -d "$BUNDLE_DIR" ] && [ -f "$BUNDLE_DIR/install-sgx.sh" ]; then
  bash "$BUNDLE_DIR/install-sgx.sh" || echo "  SGX 部分组件可能需要重启后生效"
fi

# ---- 2.10 Start services ----
systemctl restart platform-api nginx k3s 2>/dev/null || true

IP=$(hostname -I 2>/dev/null | awk '{print $1}' || echo "localhost")
echo "============================================"
echo "  SGX OneBox 安装完成"
echo "  Web UI: http://$IP"
echo "  API:    http://$IP:8080"
echo "============================================"
INSTALL_SCRIPT
chmod +x "$PAYLOAD_DIR/install.sh"

# --------------------------------------------------
# 3. Download and extract Ubuntu ISO
# --------------------------------------------------
echo "=== 3. 准备 Ubuntu Server ISO ==="
SRC_ISO="$ISO_CACHE/$ISO_FILE"
SUMS_URL="https://releases.ubuntu.com/${UBUNTU_VER}/SHA256SUMS"
SUMS_FILE="$ISO_CACHE/SHA256SUMS-${UBUNTU_VER}"

if [ ! -f "$SRC_ISO" ]; then
  echo "    下载 Ubuntu ${UBUNTU_VER} Server ISO..."
  timeout 900 wget -q --show-progress -O "$SRC_ISO" "$ISO_URL"

  echo "    下载 SHA256SUMS..."
  wget -q -O "$SUMS_FILE" "$SUMS_URL" 2>/dev/null || true

  if [ -f "$SUMS_FILE" ]; then
    EXPECTED=$(grep "$ISO_FILE" "$SUMS_FILE" | awk '{print $1}')
    ACTUAL=$(sha256sum "$SRC_ISO" | awk '{print $1}')
    if [ -n "$EXPECTED" ] && [ "$ACTUAL" = "$EXPECTED" ]; then
      echo "    SHA256 通过"
    elif [ -z "$EXPECTED" ]; then
      echo "    警告: SHA256SUMS 中未找到 $ISO_FILE，跳过校验"
    else
      echo "    校验失败! 期望: $EXPECTED"
      echo "              实际: $ACTUAL"
      rm -f "$SRC_ISO"
      exit 1
    fi
  else
    echo "    警告: 无法下载 SHA256SUMS，跳过校验"
  fi
else
  echo "    使用缓存 ISO: $SRC_ISO"
fi

echo "    解压 ISO (预计 2-3 分钟)..."
xorriso -osirrox on -indev "$SRC_ISO" -extract / "$ORIG_DIR" > /dev/null 2>&1
echo "    解压完成"
chmod -R +w "$ORIG_DIR"

# --------------------------------------------------
# 4. Create autoinstall config
# --------------------------------------------------
echo "=== 4. 生成 autoinstall 配置 ==="

cat > "$ORIG_DIR/user-data" << 'USERDATA'
#cloud-config
autoinstall:
  version: 1
  locale: en_US.UTF-8
  keyboard:
    layout: us
  identity:
    hostname: sgx-onebox
    username: admin
    password: "$6$DnIN.MCdQwy/PkKB$d2MOIchqLNPfYBnyVDBKMPYYAUgu4bbzrBpqzJT/iP/Ha/ys6E3WE360k2biNavljPn7.gDzI3gXX1b1fO2Sn/"
  ssh:
    install-server: true
    allow-pw: true
  storage:
    layout:
      name: lvm
  packages:
    - nginx
    - curl
    - wget
    - ca-certificates
    - gnupg
    - lsb-release
    - openssh-server
    - build-essential
    - git
  late-commands:
    - bash /cdrom/payload/install.sh
USERDATA

touch "$ORIG_DIR/meta-data"

# Copy payload into ISO root
cp -r "$PAYLOAD_DIR" "$ORIG_DIR/"

# --------------------------------------------------
# 5. Modify boot config for autoinstall
# --------------------------------------------------
echo "=== 5. 修改引导配置 ==="

# GRUB (UEFI boot)
GRUB_CFG="$ORIG_DIR/boot/grub/grub.cfg"
if [ -f "$GRUB_CFG" ]; then
  if grep -q 'linux.*vmlinuz' "$GRUB_CFG"; then
    sed -i '/linux.*vmlinuz/s/---/autoinstall ds=nocloud-net;s=file:\/\/\/cdrom\/ ---/' "$GRUB_CFG"
  fi
  if grep -q 'linux.*vmlinuz' "$GRUB_CFG" && ! grep -q 'autoinstall' "$GRUB_CFG"; then
    sed -i '/linux.*vmlinuz/s/$/ autoinstall ds=nocloud-net;s=file:\/\/\/cdrom\//' "$GRUB_CFG"
  fi
  echo "    grub.cfg 已配置"
fi

# ISOLINUX (BIOS boot)
ISOLINUX_CFG="$ORIG_DIR/isolinux/txt.cfg"
if [ -f "$ISOLINUX_CFG" ]; then
  sed -i 's/---/autoinstall ds=nocloud-net;s=file:\/\/\/cdrom\/ ---/' "$ISOLINUX_CFG" 2>/dev/null || true
  echo "    isolinux 已配置"
fi

# --------------------------------------------------
# 6. Build ISO
# --------------------------------------------------
echo "    重新打包 ISO (预计 5-8 分钟)..."
xorriso -as mkisofs \
  -r -V "SGX OneBox Installer" \
  -J -joliet-long \
  -b isolinux/isolinux.bin \
  -c isolinux/boot.cat \
  -no-emul-boot \
  -boot-load-size 4 \
  -boot-info-table \
  -eltorito-alt-boot \
  -e boot/grub/efi.img \
  -no-emul-boot \
  -isohybrid-gpt-basdat \
  -o "$OUTPUT_ISO" \
  "$ORIG_DIR" > /dev/null 2>&1
echo "    打包完成"

sha256sum "$OUTPUT_ISO" > "$OUTPUT_ISO.sha256"
chmod -R a+r "$OUTPUT_DIR"

echo ""
echo "============================================"
echo "  Done"
echo "  ISO:  $OUTPUT_ISO ($(du -h "$OUTPUT_ISO" | cut -f1))"
echo "  SHA:  $(cat "$OUTPUT_ISO.sha256")"
echo "============================================"
echo ""
echo "Usage:"
echo "  dd if=$OUTPUT_ISO of=/dev/sdX bs=4M status=progress"
echo "  Default login: admin / admin123"

#!/bin/bash
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
RELEASE_DIR="${PROJECT_ROOT}/out/release"
PACKAGE_NAME="sgx-onebox-platform-$(date +%Y%m%d-%H%M%S)"
PACKAGE_DIR="${RELEASE_DIR}/${PACKAGE_NAME}"

echo "=== 1. 清理旧产物 ==="
rm -rf "${RELEASE_DIR}"
mkdir -p "${PACKAGE_DIR}/bin"
mkdir -p "${PACKAGE_DIR}/web"
mkdir -p "${PACKAGE_DIR}/config"
mkdir -p "${PACKAGE_DIR}/scripts"

echo "=== 2. 编译后端 ==="
cd "${PROJECT_ROOT}"
go build -ldflags="-s -w" -o "${PACKAGE_DIR}/bin/platform-api" ./cmd/platform-api/
echo "   后端二进制: ${PACKAGE_DIR}/bin/platform-api ($(du -h "${PACKAGE_DIR}/bin/platform-api" | cut -f1))"

echo "=== 3. 构建前端 ==="
cd "${PROJECT_ROOT}/apps/web"
npm install --silent
npm run build
cp -r dist/* "${PACKAGE_DIR}/web/"
echo "   前端文件已复制到 ${PACKAGE_DIR}/web/"

echo "=== 4. 复制部署配置 ==="
cp "${PROJECT_ROOT}/deploy/systemd/platform-api.service" "${PACKAGE_DIR}/config/"
cp "${PROJECT_ROOT}/deploy/iso/platform.env.example" "${PACKAGE_DIR}/config/platform.env"
echo "   systemd 服务 + 环境配置已复制"

echo "=== 4.1 复制内置运行时资源包 ==="
BUNDLE_SRC="${PROJECT_ROOT}/runtime-bundles"
if [ -d "${BUNDLE_SRC}" ]; then
  cp -r "${BUNDLE_SRC}" "${PACKAGE_DIR}/runtime-bundles"
  find "${PACKAGE_DIR}/runtime-bundles" -name 'fetch.sh' -o -name 'generate-manifest.sh' | xargs rm -f
  BUNDLE_COUNT=$(find "${PACKAGE_DIR}/runtime-bundles" -name manifest.json | wc -l)
  echo "   已包含 ${BUNDLE_COUNT} 个内置运行时资源包"
else
  echo "   警告: runtime-bundles 目录不存在，跳过内置资源包"
fi

echo "=== 5. 生成 nginx 配置 ==="
cat > "${PACKAGE_DIR}/config/nginx-sgx-onebox.conf" << 'NGINX'
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
NGINX

echo "=== 6. 生成安装脚本 ==="
cat > "${PACKAGE_DIR}/scripts/install.sh" << 'INSTALL'
#!/bin/bash
set -euo pipefail

INSTALL_DIR="/opt/sgx-onebox"
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"

echo "=== 安装 SGX OneBox 平台 ==="

echo "1. 创建安装目录..."
mkdir -p "${INSTALL_DIR}/bin"
mkdir -p "${INSTALL_DIR}/web"
mkdir -p "${INSTALL_DIR}/config"

echo "2. 复制二进制..."
cp "${SCRIPT_DIR}/bin/platform-api" "${INSTALL_DIR}/bin/"
chmod +x "${INSTALL_DIR}/bin/platform-api"

echo "2.1 创建数据目录..."
mkdir -p "${INSTALL_DIR}/data"

echo "3. 复制前端..."
rm -rf "${INSTALL_DIR}/web"/*
cp -r "${SCRIPT_DIR}/web/"* "${INSTALL_DIR}/web/"

echo "3.1 复制内置运行时资源包..."
mkdir -p "${INSTALL_DIR}/runtime-bundles"
if [ -d "${SCRIPT_DIR}/runtime-bundles" ]; then
  cp -r "${SCRIPT_DIR}/runtime-bundles/"* "${INSTALL_DIR}/runtime-bundles/"
  echo "   已安装内置运行时资源包到 ${INSTALL_DIR}/runtime-bundles/"
else
  echo "   警告: 无内置运行时资源包"
fi

echo "4. 复制配置..."
cp "${SCRIPT_DIR}/config/platform.env" "${INSTALL_DIR}/config/"
cp "${SCRIPT_DIR}/config/nginx-sgx-onebox.conf" "${INSTALL_DIR}/config/"
if ! grep -q '^PLATFORM_SECRET=' "${INSTALL_DIR}/config/platform.env"; then
  echo "PLATFORM_SECRET=$(tr -dc 'A-Za-z0-9' </dev/urandom | head -c 48)" >> "${INSTALL_DIR}/config/platform.env"
fi
if grep -q '^PLATFORM_SECRET=replace-with-strong-random-secret-at-least-32-chars$' "${INSTALL_DIR}/config/platform.env"; then
  sed -i "s#^PLATFORM_SECRET=.*#PLATFORM_SECRET=$(tr -dc 'A-Za-z0-9' </dev/urandom | head -c 48)#" "${INSTALL_DIR}/config/platform.env"
fi

echo "5. 安装 systemd 服务..."
cp "${SCRIPT_DIR}/config/platform-api.service" /etc/systemd/system/
systemctl daemon-reload
systemctl enable platform-api
systemctl restart platform-api

echo "6. 配置 nginx..."
apt-get install -y nginx 2>/dev/null || true
cp "${SCRIPT_DIR}/config/nginx-sgx-onebox.conf" /etc/nginx/sites-available/sgx-onebox
ln -sf /etc/nginx/sites-available/sgx-onebox /etc/nginx/sites-enabled/
nginx -t && systemctl reload nginx || echo "   nginx 未安装或配置失败，请手动配置"

echo ""
echo "=== 安装完成 ==="
echo "  后端服务: systemctl status platform-api"
echo "  前端页面: http://$(hostname -I 2>/dev/null | awk '{print $1}' || echo 'localhost')"
echo "  API 端口: 8080"
echo "  数据目录: ${INSTALL_DIR}/data/"
INSTALL
chmod +x "${PACKAGE_DIR}/scripts/install.sh"

echo "=== 7. 打包 ==="
cd "${RELEASE_DIR}"
tar -czf "${PACKAGE_NAME}.tar.gz" "${PACKAGE_NAME}"
sha256sum "${PACKAGE_NAME}.tar.gz" > "${PACKAGE_NAME}.tar.gz.sha256"

echo ""
echo "=== 打包完成 ==="
echo "  文件: ${RELEASE_DIR}/${PACKAGE_NAME}.tar.gz"
echo "  大小: $(du -h "${RELEASE_DIR}/${PACKAGE_NAME}.tar.gz" | cut -f1)"
echo "  SHA256: $(cat "${RELEASE_DIR}/${PACKAGE_NAME}.tar.gz.sha256")"
echo ""
echo "目标服务器安装:"
echo "  1. scp ${PACKAGE_NAME}.tar.gz user@host:/tmp/"
echo "  2. ssh user@host"
echo "  3. tar -xzf /tmp/${PACKAGE_NAME}.tar.gz -C /tmp/"
echo "  4. sudo bash /tmp/${PACKAGE_NAME}/scripts/install.sh"

#!/bin/sh
# html-site 一键安装脚本（macOS / Linux）
#
# 用法：
#   curl -fsSL https://raw.githubusercontent.com/zaizyp/html-site/main/install.sh | sh
#
# 功能：
#   1. 从 GitHub Release 下载对应平台的 html-site 二进制到 ~/.local/bin
#   2. 把配套 skill 复制到 ~/.agents/skills/html-site
#   3. 打印下一步（配置 server url + token）
#
# 如需卸载：删除 ~/.local/bin/html-site 和 ~/.agents/skills/html-site 即可。

set -eu

OWNER="zaizyp"
REPO="html-site"
BIN_NAME="html-site"
INSTALL_DIR="${HOME}/.local/bin"
SKILL_DIR="${HOME}/.agents/skills/html-site"

# 下载稳定文件名（无版本号），永远指向最新 release
DOWNLOAD_BASE="https://github.com/${OWNER}/${REPO}/releases/latest/download"

# ----------------------------------------------------------------------------
# 1. 探测 OS / arch
# ----------------------------------------------------------------------------
OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
    Darwin) os="darwin" ;;
    Linux)  os="linux" ;;
    *)
        echo "✗ 不支持的操作系统：$OS（本脚本仅支持 macOS / Linux）" >&2
        echo "  Windows 用户请使用 install.ps1，或参照 README 手动下载。" >&2
        exit 1
        ;;
esac

case "$ARCH" in
    x86_64|amd64)   arch="amd64" ;;
    arm64|aarch64)  arch="arm64" ;;
    *)
        echo "✗ 不支持的 CPU 架构：$ARCH" >&2
        exit 1
        ;;
esac

ASSET="${BIN_NAME}_${os}_${arch}"
URL="${DOWNLOAD_BASE}/${ASSET}"

echo "→ 平台：${os}/${arch}"
echo "→ 下载：$URL"

# ----------------------------------------------------------------------------
# 2. 下载二进制
# ----------------------------------------------------------------------------
mkdir -p "$INSTALL_DIR"

TMPFILE="$(mktemp)"
trap 'rm -f "$TMPFILE"' EXIT

# 优先 curl，回退 wget
if command -v curl >/dev/null 2>&1; then
    curl -fsSL -o "$TMPFILE" "$URL"
elif command -v wget >/dev/null 2>&1; then
    wget -qO "$TMPFILE" "$URL"
else
    echo "✗ 需要 curl 或 wget 来下载，未找到。" >&2
    exit 1
fi

chmod +x "$TMPFILE"
mv "$TMPFILE" "$INSTALL_DIR/$BIN_NAME"
trap - EXIT
echo "✓ 二进制已安装：$INSTALL_DIR/$BIN_NAME"

# ----------------------------------------------------------------------------
# 3. 安装 skill（失败不致命，只警告）
# ----------------------------------------------------------------------------
mkdir -p "$(dirname "$SKILL_DIR")"
# skill 文件从仓库 main 分支的 raw 内容拉取
SKILL_URL="https://raw.githubusercontent.com/${OWNER}/${REPO}/main/skills/html-site/SKILL.md"
if command -v curl >/dev/null 2>&1; then
    if curl -fsSL -o "${SKILL_DIR}/SKILL.md" "$SKILL_URL" 2>/dev/null; then
        echo "✓ skill 已安装：$SKILL_DIR/SKILL.md"
    else
        echo "! skill 下载失败（不影响 CLI 使用），可手动从仓库复制 skills/html-site/" >&2
    fi
elif command -v wget >/dev/null 2>&1; then
    if wget -qO "${SKILL_DIR}/SKILL.md" "$SKILL_URL" 2>/dev/null; then
        echo "✓ skill 已安装：$SKILL_DIR/SKILL.md"
    else
        echo "! skill 下载失败（不影响 CLI 使用），可手动从仓库复制 skills/html-site/" >&2
    fi
fi

# ----------------------------------------------------------------------------
# 4. PATH 检查 + 下一步提示
# ----------------------------------------------------------------------------
case ":$PATH:" in
    *":$INSTALL_DIR:"*) ;;
    *)
        echo ""
        echo "⚠️  $INSTALL_DIR 不在 PATH 中。请把它加进去："
        echo "    echo 'export PATH=\"$INSTALL_DIR:\$PATH\"' >> ~/.bashrc   # bash"
        echo "    echo 'set -x PATH $INSTALL_DIR \$PATH' >> ~/.config/fish/config.fish  # fish"
        echo "    然后重开终端或 source 配置文件。"
        ;;
esac

echo ""
echo "🎉 安装完成！下一步："
echo "    html-site version                          # 验证"
echo "    html-site config set --url <服务器地址> --token <你的token>"
echo ""
echo "    token 由服务器管理员执行 'html-site user add <name>' 生成。"

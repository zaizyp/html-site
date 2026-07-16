#!/bin/sh
# html-site 一键安装/升级脚本（macOS / Linux）
#
# 用法：
#   curl -fsSL https://raw.githubusercontent.com/zaizyp/html-site/main/install.sh | sh
#
# 本脚本幂等，可重复执行：每次都拉取最新二进制覆盖安装，并把最新 SKILL.md
# 同步到本机所有已安装技能的 agent 目录。既适用于首次安装，也适用于升级。
#
# 功能：
#   1. 从 GitHub Release 下载对应平台的 html-site 最新二进制到 ~/.local/bin
#   2. 把最新 SKILL.md 写入 ~/.agents/skills/html-site，并同步到其余已存在的
#      agent 目录（~/.zcode ~/.claude ~/.codex ~/.cursor）
#   3. 打印下一步（配置 server url + token）
#
# 如需卸载：删除 ~/.local/bin/html-site 和各 agent 目录下的 html-site 技能即可。

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
# 3. 安装/更新 skill（幂等，可重复执行）
#    策略与 `html-site upgrade` 对齐：
#    - 默认目录 ~/.agents/skills 一定写入（首次安装也覆盖）
#    - 其余候选目录（~/.zcode ~/.claude ~/.codex ~/.cursor）仅当已存在时更新，
#      不主动创建，避免给没装对应 agent 的用户留下空目录
# ----------------------------------------------------------------------------
SKILL_URL="https://raw.githubusercontent.com/${OWNER}/${REPO}/main/skills/html-site/SKILL.md"

fetch_to() {
    # fetch_to <url> <dest>：curl 优先，回退 wget。成功返回 0。
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL -o "$2" "$1" 2>/dev/null
    elif command -v wget >/dev/null 2>&1; then
        wget -qO "$2" "$1" 2>/dev/null
    else
        return 127
    fi
}

# 下载 SKILL.md 到临时文件，再分发到各目录
SKILL_TMP="$(mktemp)"
if fetch_to "$SKILL_URL" "$SKILL_TMP"; then
    # 3.1 默认目录：确保存在并写入（首次安装也走这里）
    mkdir -p "$SKILL_DIR"
    cp "$SKILL_TMP" "${SKILL_DIR}/SKILL.md"
    echo "✓ skill 已安装：${SKILL_DIR}/SKILL.md"

    # 3.2 其余候选目录：仅当根目录已存在时更新其下的 html-site 技能
    #     （列表与 upgrade.go 的 agentSkillDirs 保持一致）
    for skill_root in \
        "${HOME}/.zcode/skills" \
        "${HOME}/.claude/skills" \
        "${HOME}/.codex/skills" \
        "${HOME}/.cursor/skills"; do
        if [ -d "$skill_root" ]; then
            mkdir -p "${skill_root}/html-site"
            cp "$SKILL_TMP" "${skill_root}/html-site/SKILL.md"
            echo "✓ skill 已同步：${skill_root}/html-site/SKILL.md"
        fi
    done
else
    echo "! skill 下载失败（不影响 CLI 使用），可手动从仓库复制 skills/html-site/" >&2
fi
rm -f "$SKILL_TMP"

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
echo ""
echo "    后续升级：重跑本命令即可，或执行 'html-site upgrade'。"

# html-site 一键安装/升级脚本（Windows / PowerShell）
#
# 用法（在 PowerShell 中执行）：
#   irm https://raw.githubusercontent.com/zaizyp/html-site/main/install.ps1 | iex
#
# 本脚本幂等，可重复执行：每次都拉取最新二进制覆盖安装，并把最新 SKILL.md
# 同步到本机所有已安装技能的 agent 目录。既适用于首次安装，也适用于升级。
#
# 功能：
#   1. 从 GitHub Release 下载 html-site.exe 最新版到 %USERPROFILE%\.local\bin
#   2. 把该目录加入用户 PATH（如尚未加入）
#   3. 把最新 SKILL.md 写入 %USERPROFILE%\.agents\skills\html-site，并同步到其余
#      已存在的 agent 目录（.zcode .claude .codex .cursor）
#   4. 打印下一步（配置 server url + token）
#
# 如需卸载：删除 html-site.exe 和各 agent 目录下的 html-site 技能即可。

$ErrorActionPreference = "Stop"

$Owner    = "zaizyp"
$Repo     = "html-site"
$BinName  = "html-site.exe"
$InstallDir = Join-Path $env:USERPROFILE ".local\bin"
$SkillDir   = Join-Path $env:USERPROFILE ".agents\skills\html-site"

# 下载稳定文件名（无版本号），永远指向最新 release
$DownloadBase = "https://github.com/$Owner/$Repo/releases/latest/download"

# ----------------------------------------------------------------------------
# 1. 探测架构
# ----------------------------------------------------------------------------
$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    "AMD64" { "amd64" }
    "ARM64" { "arm64" }
    default {
        Write-Error "不支持的 CPU 架构：$env:PROCESSOR_ARCHITECTURE"
        exit 1
    }
}

# Windows 产物带 .exe 后缀（见 release.yml: build_one windows amd64 ".exe"）
$Asset = "html-site_windows_$arch.exe"
$Url   = "$DownloadBase/$Asset"

Write-Host "→ 平台：windows/$arch"
Write-Host "→ 下载：$Url"

# ----------------------------------------------------------------------------
# 2. 下载二进制
# ----------------------------------------------------------------------------
if (-not (Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
}

$Dest = Join-Path $InstallDir $BinName
try {
    Invoke-WebRequest -Uri $Url -OutFile $Dest -UseBasicParsing
} catch {
    Write-Error "下载失败：$($_.Exception.Message)"
    exit 1
}
Write-Host "✓ 二进制已安装：$Dest" -ForegroundColor Green

# ----------------------------------------------------------------------------
# 3. 加入用户 PATH（如尚未加入）
# ----------------------------------------------------------------------------
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$InstallDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$InstallDir", "User")
    # 当前会话也立即可用
    if (-not ($env:Path -like "*$InstallDir*")) {
        $env:Path += ";$InstallDir"
    }
    Write-Host "✓ 已将 $InstallDir 加入用户 PATH（新开终端生效）" -ForegroundColor Green
}

# ----------------------------------------------------------------------------
# 4. 安装/更新 skill（幂等，可重复执行）
#    策略与 `html-site upgrade` 对齐：
#    - 默认目录 ~/.agents/skills 一定写入（首次安装也覆盖）
#    - 其余候选目录（~/.zcode ~/.claude ~/.codex ~/.cursor）仅当已存在时更新，
#      不主动创建，避免给没装对应 agent 的用户留下空目录
# ----------------------------------------------------------------------------
$SkillUrl = "https://raw.githubusercontent.com/$Owner/$Repo/main/skills/html-site/SKILL.md"
$skillTmp = Join-Path $env:TEMP "html-site-SKILL.md"
try {
    Invoke-WebRequest -Uri $SkillUrl -OutFile $skillTmp -UseBasicParsing

    # 4.1 默认目录：确保存在并写入
    if (-not (Test-Path $SkillDir)) {
        New-Item -ItemType Directory -Path $SkillDir -Force | Out-Null
    }
    Copy-Item $skillTmp -Destination (Join-Path $SkillDir "SKILL.md") -Force
    Write-Host "✓ skill 已安装：$SkillDir\SKILL.md" -ForegroundColor Green

    # 4.2 其余候选目录：仅当根目录已存在时更新其下的 html-site 技能
    #     （列表与 upgrade.go 的 agentSkillDirs 保持一致）
    $skillRoots = @(
        (Join-Path $env:USERPROFILE ".zcode\skills"),
        (Join-Path $env:USERPROFILE ".claude\skills"),
        (Join-Path $env:USERPROFILE ".codex\skills"),
        (Join-Path $env:USERPROFILE ".cursor\skills")
    )
    foreach ($root in $skillRoots) {
        if (Test-Path $root) {
            $targetDir = Join-Path $root "html-site"
            if (-not (Test-Path $targetDir)) {
                New-Item -ItemType Directory -Path $targetDir -Force | Out-Null
            }
            Copy-Item $skillTmp -Destination (Join-Path $targetDir "SKILL.md") -Force
            Write-Host "✓ skill 已同步：$targetDir\SKILL.md" -ForegroundColor Green
        }
    }
} catch {
    Write-Warning "skill 下载失败（不影响 CLI 使用），可手动从仓库复制 skills/html-site/"
} finally {
    if (Test-Path $skillTmp) { Remove-Item $skillTmp -Force -ErrorAction SilentlyContinue }
}

# ----------------------------------------------------------------------------
# 5. 下一步提示
# ----------------------------------------------------------------------------
Write-Host ""
Write-Host "🎉 安装完成！下一步（新开一个 PowerShell 窗口后执行）：" -ForegroundColor Cyan
Write-Host "    html-site version                          # 验证"
Write-Host "    html-site config set --url <服务器地址> --token <你的token>"
Write-Host ""
Write-Host "    token 由服务器管理员执行 'html-site user add <name>' 生成。"
Write-Host "    后续升级：重跑本命令即可，或执行 'html-site upgrade'。"

# html-site 一键安装脚本（Windows / PowerShell）
#
# 用法（在 PowerShell 中执行）：
#   irm https://raw.githubusercontent.com/zaizyp/html-site/main/install.ps1 | iex
#
# 功能：
#   1. 从 GitHub Release 下载 html-site.exe 到 %USERPROFILE%\.local\bin
#   2. 把该目录加入用户 PATH（如尚未加入）
#   3. 把配套 skill 复制到 %USERPROFILE%\.agents\skills\html-site
#   4. 打印下一步（配置 server url + token）
#
# 如需卸载：删除 html-site.exe 和 skill 目录即可。

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

$Asset = "html-site_windows_$arch"
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
# 4. 安装 skill（失败不致命）
# ----------------------------------------------------------------------------
if (-not (Test-Path $SkillDir)) {
    New-Item -ItemType Directory -Path $SkillDir -Force | Out-Null
}
$SkillUrl = "https://raw.githubusercontent.com/$Owner/$Repo/main/skills/html-site/SKILL.md"
try {
    Invoke-WebRequest -Uri $SkillUrl -OutFile (Join-Path $SkillDir "SKILL.md") -UseBasicParsing
    Write-Host "✓ skill 已安装：$SkillDir\SKILL.md" -ForegroundColor Green
} catch {
    Write-Warning "skill 下载失败（不影响 CLI 使用），可手动从仓库复制 skills/html-site/"
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

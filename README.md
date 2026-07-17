# html-site

一个轻量的**离线 HTML 在线托管站点**。把单个 HTML 文件发布成可分享的在线网页，生成访问链接发给别人；支持分享码保护、多人 owner 隔离、管理后台、分组归类、AI agent 自动发布/修改。

- **单二进制**：Go 编写，无运行时依赖（纯 Go SQLite，免 CGO）。同一个二进制既是服务端（`serve`），又是客户端（`upload`/`update`/...），后台页面也嵌入二进制。
- **管理后台**：浏览器访问 `/admin`，账号密码登录，管理员/普通用户两档角色，可视化管理用户、页面、分组。
- **三层权限**：写操作靠 API token 隔离 owner；读操作靠分享码（可选）；后台靠账号密码 + session + 角色。
- **分组归类**：每个用户可建单层分组，把页面归类管理。
- **配套 skill**：内置 `html-site` skill，让 AI agent 按 SKILL.md 自动发布 HTML 并回报链接。

---

## 安装

html-site 是**客户端 / 服务器 (C/S)** 架构：客户端（`html-site` 二进制 + skill）负责发布页面，服务器（`html-site serve` 或 Docker）负责托管。**大多数用户只需要装客户端**，连到别人提供的服务器即可。

选一条适合你的路径：

| 路径 | 适合谁 | 一句话 |
|------|--------|--------|
| **A. 丢给 AI agent 自动装** | 用 Codex / Claude Code / ZCode / WorkBuddy 等 | 把本仓库链接发给 AI，它读本 README 自动装客户端 |
| **B. macOS / Linux 一行** | 命令行用户 | `curl … \| sh` |
| **C. Windows 一行** | Windows 用户 | `irm … \| iex` |
| **D. 自托管服务器** | 想自己跑 server 的人 | `docker pull` GHCR 镜像 |

> 不管走哪条路径，**你都需要一个服务器地址 (`url`) 和一个 API `token`**。
> 这两者由服务器管理员在执行 `html-site user add <name>` 时生成（token 仅显示一次）。
> 没有这两个值，客户端无法发布页面。

---

### 路径 A：丢给 AI agent 自动装（推荐）

如果你用的是 Codex、Claude Code、ZCode、WorkBuddy 等 AI 编程助手，最省事的方式是：**把本仓库链接发给它**，让它自己读完本 README 完成客户端安装。

你可以直接对 AI 说：

> 帮我装一下这个 html-site 技能作为客户端：https://github.com/zaizyp/html-site
> 服务器地址是 `<你的URL>`，token 是 `<你的token>`

AI 会自动执行下面的步骤。

#### 给 AI 的安装步骤（人和 AI 都可读）

**第 1 步 · 确认平台并下载二进制**

先检测操作系统和 CPU 架构，然后从下表选对应的文件名。所有文件都从「最新 Release」的稳定 URL 拉取（无需知道版本号）：

| 操作系统 | 架构 | 下载文件名 |
|----------|------|-----------|
| macOS (Apple Silicon) | arm64 | `html-site_darwin_arm64` |
| macOS (Intel) | amd64 | `html-site_darwin_amd64` |
| Linux | arm64 | `html-site_linux_arm64` |
| Linux | amd64 | `html-site_linux_amd64` |
| Windows | arm64 | `html-site_windows_arm64.exe` |
| Windows | amd64 | `html-site_windows_amd64.exe` |

下载地址模板（替换 `<文件名>`）：
```
https://github.com/zaizyp/html-site/releases/latest/download/<文件名>
```

- **macOS / Linux**：放到 `~/.local/bin/html-site`，执行 `chmod +x`，并确保 `~/.local/bin` 在 `PATH` 中。
- **Windows**：放到 `%USERPROFILE%\.local\bin\html-site.exe`，并确保该目录在用户 `PATH` 中。

> 如果 Release 里没有你要的平台，可用源码自建：`git clone` 本仓库后 `CGO_ENABLED=0 go build -o html-site ./cmd/html-site`（需 Go 1.25+，纯 Go 无 CGO）。

**第 2 步 · 安装 skill（可选但推荐）**

把仓库内 `skills/html-site/SKILL.md` 复制到你所用的 AI 框架的 skill 目录：

| 框架 | skill 目录 |
|------|-----------|
| ZCode / WorkBuddy | `~/.agents/skills/html-site/SKILL.md` |
| Claude Code | `~/.claude/skills/html-site/SKILL.md` |
| Codex | `~/.codex/skills/html-site/SKILL.md` |
| Cursor | `~/.cursor/skills/html-site/SKILL.md` |

> 各框架的用户级 skill 目录是事实标准位置。安装脚本和 `html-site upgrade` 会扫描上面这些目录，凡已安装 html-site 技能的都会自动同步到最新。

> 装了 skill = 下次听到「发布 HTML」会**自动触发**；不装也能照着本 README 立即操作 CLI。

**第 3 步 · 配置连接（关键，需用户提供凭据）**

```bash
html-site config set --url <服务器地址> --token <你的token>
```

> ⚠️ **安全要求**：`url` 和 `token` 是访问凭据。如果用户没有提供，**必须向用户索要，严禁自行编造或猜测**。token 由服务器管理员执行 `html-site user add <name>` 时生成，仅显示一次。

**第 4 步 · 验证**

```bash
html-site version     # 应输出版本号，如 html-site 1.2.0
html-site config show # 确认 url 和 token 已配置（token 脱敏显示）
```

验证通过后，即可发布页面：
```bash
html-site upload --file page.html --title "我的页面"
```

#### 升级（二进制 + 技能一起更新）

后续有新版本时，一条命令即可升级——同时更新二进制，并把最新 SKILL.md 同步到本机所有已安装技能的 agent 目录（ZCode / Claude Code / Codex / Cursor 都会覆盖到）：

```bash
html-site upgrade          # 检查并升级到最新版（版本相同则只同步技能）
html-site upgrade --force  # 强制重新下载并覆盖（排查问题时用）
```

升级后新版本在**下次执行 `html-site`** 时生效。AI agent 也可以自主调用 `html-site upgrade` 完成升级。

---

### 路径 B：macOS / Linux 一行命令

```bash
curl -fsSL https://raw.githubusercontent.com/zaizyp/html-site/main/install.sh | sh
```

脚本会自动探测架构、下载二进制到 `~/.local/bin`、安装 skill 到 `~/.agents/skills`，并提示你配置 `url` + `token`。
（也可用 `wget -qO- …/install.sh | sh`）

### 路径 C：Windows 一行命令（PowerShell）

```powershell
irm https://raw.githubusercontent.com/zaizyp/html-site/main/install.ps1 | iex
```

脚本会下载 `html-site.exe` 到 `%USERPROFILE%\.local\bin`、自动加入用户 `PATH`、安装 skill，并提示你配置 `url` + `token`。

> **注意**：`irm | iex` 安装后，需**新开一个 PowerShell 窗口**，`html-site` 命令才在 PATH 中生效。

### 路径 D：自托管服务器

见下方 [Docker 部署](#docker-部署) 或 [从源码构建](#1-编译) 章节。镜像已发布到 GHCR，开箱即用：

```bash
docker pull ghcr.io/zaizyp/html-site:latest
```

---

## 快速开始（服务器端）

以下命令在**服务器上**执行，用于启动服务、创建用户。如果你只是客户端用户（连别人的服务器），跳到 [安装](#安装) 章节配置好 `url` + `token` 即可。

### 1. 从源码编译

```bash
go build -o html-site ./cmd/html-site      # Linux/macOS
go build -o html-site.exe ./cmd/html-site  # Windows
```

产物是单个可执行文件（约 15MB），拷到哪都能跑。

### 2. 启动服务端（在服务器上）

```bash
./html-site serve --addr :8080 --data ./data --url https://your-domain.com
```

所有参数都有默认值，也可以不传任何参数直接 `./html-site serve`（用默认 `:8080` + `./data`）。

- `--addr`：监听地址，默认 `:8080`
- `--data`：数据目录（SQLite + HTML 文件），默认 `./data`，首次运行自动创建
- `--url`：对外可访问的基础 URL，用于拼接访问链接；留空则用请求 Host 头推断

**环境变量**（优先级低于命令行 flag，便于容器/系统部署时不传参数）：

| 环境变量 | 作用 | 默认值 |
|----------|------|--------|
| `HTML_SITE_ADDR` | 监听地址 | `:8080` |
| `HTML_SITE_DATA` | 数据目录 | `./data` |
| `HTML_SITE_URL` | 对外基础 URL（server 拼链接用） | 空（用请求 Host 推断） |
| `HTML_SITE_TOKEN` | 客户端 API token（覆盖 config 文件） | 空 |

```bash
# 用环境变量启动（无需任何 flag）
HTML_SITE_ADDR=:9090 HTML_SITE_DATA=/var/lib/html-site HTML_SITE_URL=https://site.example.com ./html-site serve
```

建议用 systemd / supervisord 守护，并用 nginx/caddy 套一层 HTTPS。

### 3. 创建 owner（在服务器上本地执行）

```bash
# 创建用户：首个用户自动成为管理员
./html-site user add --data ./data --password <密码> alice
# 输出：
#   name:      alice
#   token:     <64位十六进制>
#   role:      admin
#   password:  (已设置)
#   （token 仅显示一次，请妥善保存）

# 显式创建管理员或普通用户
./html-site user add --data ./data --password pwd --admin bob
./html-site user add --data ./data --password pwd charlie

# 修改某用户密码
./html-site user passwd --data ./data alice
```

> ⚠️ **flags 必须写在位置参数前面**：`user add --data ./data --password pwd alice` ✅ ；`user add alice --data ./data` ❌（Go flag 包在首个非 flag 参数处停止解析）。

列出所有 owner：
```bash
./html-site user list --data ./data
```

### 4. 客户端配置（在 AI agent 机器上）

```bash
./html-site config set --url https://your-domain.com --token <alice的token>
./html-site config show   # 确认（token 会脱敏）
```

配置存在 `~/.html-site/config.json`（权限 0600）。也可用环境变量 `HTML_SITE_URL` / `HTML_SITE_TOKEN` 覆盖。

### 5. 发布页面

```bash
./html-site upload --file page.html --title "我的页面"
# 输出：
#   slug:       aB3x9Q
#   url:        https://your-domain.com/v/aB3x9Q
#   access:     public（任何人凭链接可访问）
```

把 `url` 发给别人即可。带分享码的发布：
```bash
./html-site upload --file secret.html --share-code cat2024
# 输出会包含 share_code，把它连同链接一起发给对方
```

归入分组（单层，不存在则自动创建）：
```bash
./html-site upload --file report.html --title "Q3 报告" --group reports
```

### 6. 管理后台（浏览器）

服务启动后访问 `https://your-domain.com/admin`，用账号密码登录：

- **管理员**：查看所有用户/页面/分组；创建/删除用户；重置任意用户密码；调整角色；删除任意页面。
- **普通用户**：只管理自己的页面和分组；修改自己密码；重新生成自己的 API token。

后台功能：页面列表（按分组/用户筛选、搜索、删除）、分组管理（增删改名）、用户管理（管理员）、个人设置（改密 + token 管理）。

---

## Docker 部署

镜像已发布到 GitHub Container Registry（GHCR），无需本地构建即可使用。仓库另提供 `Dockerfile`（多阶段构建）和 `docker-compose.yml`（示例配置）。

### 用 docker compose（推荐）

```bash
# 1. 按需修改 docker-compose.yml 中的 HTML_SITE_URL（填你的实际域名）
# 2. 拉取镜像并启动（compose 文件已默认用 ghcr.io 镜像，无需 --build）
docker compose pull && docker compose up -d

# 3. 首次启动后，进容器创建管理员
docker compose exec html-site /app/html-site user add --password <密码> admin

# 4. 访问 http://localhost:8080/admin 登录后台
```

数据持久化在宿主机 `./data` 目录（挂载到容器 `/app/data`），删容器不丢数据。

### 用 docker 直接拉取运行

```bash
# 拉取 GHCR 镜像
docker pull ghcr.io/zaizyp/html-site:latest

# 运行（数据持久化到宿主机 ./data）
docker run -d --name html-site \
  -p 8080:8080 \
  -v "$PWD/data:/app/data" \
  -e HTML_SITE_URL=http://localhost:8080 \
  --restart unless-stopped \
  ghcr.io/zaizyp/html-site:latest

# 创建管理员
docker exec -it html-site /app/html-site user add --password <密码> admin
```

> 想从本地源码自行构建（例如改了代码未发布），把上面的 `pull` 换成 `docker build -t html-site .`，`docker run` 的镜像名用 `html-site` 即可。

### 镜像特点

- **多阶段构建**：编译阶段用 `golang:1.25-alpine`，运行阶段用 `alpine:3.20`，最终镜像很小（~25MB）。
- **纯 Go 无 CGO**：modernc.org/sqlite 免 CGO，静态编译，无运行时依赖。
- **非 root 运行**：容器内以 `app` 用户运行。
- **环境变量配置**：通过 `HTML_SITE_*` 环境变量配置，无需传 flag。
- **healthcheck**：自动检查 `/healthz` 端点。

### 生产部署建议

生产环境建议前面套一层 nginx/caddy 走 HTTPS，`HTML_SITE_URL` 填 HTTPS 域名：

```yaml
environment:
  HTML_SITE_URL: "https://site.example.com"  # 你的 HTTPS 域名
```

nginx 反代示例（将 / 转发到容器 8080）：
```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Forwarded-Host $host;
}
```

---

## 全部命令

### 服务端 / 管理（本地操作数据库）

| 命令 | 说明 |
|------|------|
| `serve [--addr :8080] [--data ./data] [--url URL]` | 启动服务 |
| `user add --data D [--password P] [--admin] <name>` | 创建用户，返回 token（仅一次） |
| `user passwd --data D <name>` | 设置/修改用户密码 |
| `user list --data D` | 列出所有用户 |

### 客户端（调远端 API）

| 命令 | 说明 |
|------|------|
| `config set --url URL --token TOKEN` | 保存连接配置 |
| `config show` | 查看当前配置 |
| `upload --file F [--title T] [--share-code C] [--slug S] [--group G]` | 发布新页面 |
| `update --slug S [--file F] [--title T] [--share-code C] [--public]` | 修改 |
| `list [--json]` | 列出当前 owner 的页面 |
| `info --slug S [--json]` | 查看详情（含 HTML 内容） |
| `delete --slug S` | 删除 |
| `upgrade [--force]` | 自更新二进制 + 同步技能到所有 agent 目录 |

### 升级

| 命令 | 说明 |
|------|------|
| `upgrade` | 检查 GitHub 最新版本，不同则下载替换二进制，并把最新 SKILL.md 同步到本机所有已安装技能的 agent 目录 |
| `upgrade --force` | 强制重新下载并覆盖（即使版本相同） |

---

## 权限模型

### 角色（后台）

| 能力 | 管理员 | 普通用户 |
|------|:---:|:---:|
| 后台登录 | ✅ | ✅ |
| 管理自己的页面/分组 | ✅ | ✅ |
| 查看所有用户和页面 | ✅ | ❌（只看自己） |
| 创建/删除用户、重置密码、改角色 | ✅ | ❌ |
| 删除任意用户的页面 | ✅ | ❌ |
| 修改自己密码、重置自己 token | ✅ | ✅ |

### 写权限：owner 隔离（API）

- 每个 owner 一个 token，由管理员 `user add` 生成，或在后台个人设置中重置。
- 所有 `/api/*` 请求必须带 `X-API-Token` 头；服务端据此识别 owner。
- 每个页面记录 `owner_id`；**只能增删改自己的页面**。
- 操作别人的页面统一返回 `404`（不暴露页面是否存在）。

### 读权限：分享码

- 发布时 `--share-code` 留空 = **公开**，任何人凭链接可访问。
- 设了分享码 = **受保护**，访问 `/v/{slug}` 时会进入分享码输入页。
- 校验通过后种一个 HttpOnly cookie（30 天有效），后续访问免重复输入。

---

## HTTP API（供自定义集成）

所有 `/api/*` 需带 `X-API-Token` 头。

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/api/pages` | 上传，body `{content, title?, share_code?, slug?, group?}` |
| `GET` | `/api/pages` | 列出当前 owner 的页面 |
| `GET` | `/api/pages/{slug}` | 查看元信息 + content |
| `PUT` | `/api/pages/{slug}` | 修改，body `{content?, title?, share_code_set?, share_code?}` |
| `DELETE` | `/api/pages/{slug}` | 删除 |
| `GET` | `/v/{slug}` | 查看 HTML（无码直返；有码走校验） |
| `POST` | `/v/{slug}/verify` | 提交分享码，body `share_code=xxx` |
| `GET` | `/healthz` | 健康检查 |

---

## AI Skill

仓库内 `skills/html-site/SKILL.md` 是配套技能，教 AI agent 如何调用 CLI 完成发布。详细的安装方式见开头 [安装 / 路径 A](#路径-a丢给-ai-agent-自动装推荐) 章节——既支持手动复制，也支持直接把仓库链接丢给 AI 让它自己装。

安装后，AI agent 在听到"发布 HTML""生成可访问链接""修改已上线页面"等指令时，会自动按 SKILL.md 调用 CLI 完成发布并把链接回报给你。

**前提**：agent 机器上 `html-site` 二进制已加入 PATH，且 `config set` 配好了 server 和 token。

---

## 备份与迁移

整个系统状态都在 `--data` 目录里：

```
data/
├── app.db          # SQLite 元数据（users + pages + groups + sessions）
├── app.db-wal      # WAL 日志（运行时存在）
└── pages/          # 所有 HTML 文件，按 slug 命名
    ├── aB3x9Q.html
    └── mypage.html
```

**备份**：停服或直接热拷贝整个 `data/` 目录即可（SQLite WAL 模式下热拷贝也安全）。**迁移到新机器**：把 `data/` 拷过去，重启 `serve` 指向新目录。

**版本升级迁移**：从第一期版本（无管理后台/分组）升级时，直接用新二进制启动即可——`store.Open` 会自动执行幂等迁移：补 `users.role`/`users.password_hash`/`pages.group_id` 列、建 `groups`/`sessions` 表、补索引，并把首个用户提升为管理员。无需手动操作数据库。

---

## 项目结构

```
html-site/
├── cmd/html-site/main.go      # 入口：子命令分发
├── internal/
│   ├── model/                  # 数据结构 + JSON tag
│   ├── store/                  # SQLite 元数据 + 磁盘文件存储 + 迁移
│   ├── server/                 # HTTP 服务端（API + 查看层 + 管理后台）
│   │   └── web/                # 后台模板 + 静态资源（go:embed 嵌入）
│   ├── client/                 # HTTP 客户端（给 CLI 用）
│   └── cli/                    # 子命令实现
├── skills/html-site/SKILL.md   # 配套 AI skill
├── Dockerfile                  # 多阶段构建
├── docker-compose.yml          # 部署示例（默认拉 GHCR 镜像）
├── install.sh                  # macOS/Linux 一键安装
├── install.ps1                 # Windows 一键安装
├── .github/workflows/release.yml  # CI：打 tag 自动发布二进制 + GHCR 镜像
├── .dockerignore
└── go.mod                      # 依赖：modernc.org/sqlite + golang.org/x/crypto
```

---

## 安全注意事项

- token 用 `crypto/rand` 生成（32 字节 hex，64 字符），不可猜测。
- 密码用 bcrypt 存储，明文永不落库。
- 后台 session token 随机 32 字节，HttpOnly cookie；所有 POST 表单带 CSRF token 校验。
- slug 默认随机 base62 6 位（约 5.7×10¹⁰ 空间），不可枚举；自定义 slug 限 `[A-Za-z0-9_-]`。
- 分享码 cookie 设 `HttpOnly` + `SameSite=Lax`，值是分享码 SHA-256 前缀（不存明文）。
- 上传体积限制 10MB。
- 防止删除最后一个管理员（至少保留一个）。
- **生产部署务必套 nginx/caddy 走 HTTPS**，避免 token、密码、分享码明文传输。
- `~/.html-site/config.json` 含 token，权限设为 0600。

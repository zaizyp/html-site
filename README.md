# html-site

一个轻量的**离线 HTML 在线托管站点**。把单个 HTML 文件发布成可分享的在线网页，生成访问链接发给别人；支持分享码保护、多人 owner 隔离、管理后台、分组归类、AI agent 自动发布/修改。

- **单二进制**：Go 编写，无运行时依赖（纯 Go SQLite，免 CGO）。同一个二进制既是服务端（`serve`），又是客户端（`upload`/`update`/...），后台页面也嵌入二进制。
- **管理后台**：浏览器访问 `/admin`，账号密码登录，管理员/普通用户两档角色，可视化管理用户、页面、分组。
- **三层权限**：写操作靠 API token 隔离 owner；读操作靠分享码（可选）；后台靠账号密码 + session + 角色。
- **分组归类**：每个用户可建单层分组，把页面归类管理。
- **配套 skill**：内置 `html-site` skill，让 AI agent 按 SKILL.md 自动发布 HTML 并回报链接。

---

## 快速开始

### 1. 编译

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

仓库提供 `Dockerfile`（多阶段构建）和 `docker-compose.yml`（示例配置）。

### 用 docker compose（推荐）

```bash
# 1. 按需修改 docker-compose.yml 中的 HTML_SITE_URL（填你的实际域名）
# 2. 构建并启动
docker compose up -d --build

# 3. 首次启动后，进容器创建管理员
docker compose exec html-site /app/html-site user add --password <密码> admin

# 4. 访问 http://localhost:8080/admin 登录后台
```

数据持久化在宿主机 `./data` 目录（挂载到容器 `/app/data`），删容器不丢数据。

### 用 docker 直接构建运行

```bash
# 构建
docker build -t html-site .

# 运行（数据持久化到宿主机 ./data）
docker run -d --name html-site \
  -p 8080:8080 \
  -v "$PWD/data:/app/data" \
  -e HTML_SITE_URL=http://localhost:8080 \
  --restart unless-stopped \
  html-site

# 创建管理员
docker exec -it html-site /app/html-site user add --password <密码> admin
```

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

## AI Skill 安装

仓库内 `skills/html-site/SKILL.md` 是配套技能。安装到 agent：

```bash
# 复制到用户级 skills 目录
cp -r skills/html-site ~/.agents/skills/
```

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
├── docker-compose.yml          # 部署示例
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

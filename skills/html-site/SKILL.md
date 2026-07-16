---
name: html-site
version: 0.1.0
description: "把离线 HTML 文件发布成可分享的在线网页。当用户要求『发布 HTML / 上线页面 / 把这个 HTML 生成链接发给别人 / 生成可访问链接 / 修改已上线的页面 / 删除已发布页面 / 查看已发布页面列表』时使用本 skill。需要本地安装 html-site 二进制并配置好 server 地址与 token。"
metadata:
  requires:
    bins: ["html-site"]
  cliHelp: "html-site help; html-site upload --help(子命令无 --help 时用 html-site help 查看用法)"
---

# html-site —— 离线 HTML 在线发布

把单个 HTML 文件发布到一个在线站点，生成可分享的访问链接；支持修改、删除、查看已发布页面。访问权限分两层：

- **写权限（owner 隔离）**：每个 agent/用户有自己的 token，只能增删改自己发布的页面。
- **读权限（分享码）**：发布时可选设置分享码。无分享码 = 任何人凭链接可看；有分享码 = 访问者需输入分享码。

## 前置条件 —— 执行操作前必读

**CRITICAL**：本 skill 所有操作都依赖 `html-site` CLI 能连通远端 server。首次使用前确认：

1. `html-site` 二进制已在 PATH 中（`html-site version` 能输出版本号）。
2. 已执行过一次配置（任选其一）：
   - 配置文件：`html-site config set --url <SERVER_URL> --token <TOKEN>`
   - 或环境变量：`HTML_SITE_URL` 与 `HTML_SITE_TOKEN`

可用 `html-site config show` 确认当前配置（token 会脱敏显示）。

> **若用户尚未提供 server 地址和 token，先问用户索要，不要自行编造。** token 在管理员执行 `html-site user add <name>` 时生成，仅显示一次。

## 命令速查

**发布新页面**（最常用）：
```bash
html-site upload --file page.html [--title "标题"] [--share-code SECRET] [--slug my-page] [--group 分组名]
```
- `--file`：HTML 文件路径（必填）
- `--title`：页面标题，可选
- `--share-code`：分享码；**不传 = 公开访问**，传了 = 受保护
- `--slug`：自定义短链；不传 = 随机生成（推荐，避免冲突）
- `--group`：归入分组（单层，不存在则自动创建）；不传 = 未分组

**修改已发布页面**：
```bash
# 改内容
html-site update --slug <SLUG> --file new.html

# 改标题
html-site update --slug <SLUG> --title "新标题"

# 加/改分享码
html-site update --slug <SLUG> --share-code NEWCODE

# 改为公开（去掉分享码）
html-site update --slug <SLUG> --public
```

**查看与管理**：
```bash
html-site list                # 列出当前 owner 的全部页面
html-site info  --slug <SLUG> # 查看某页面详情（含 HTML 内容）
html-site delete --slug <SLUG># 删除某页面
html-site list --json         # JSON 输出，便于解析
html-site info  --slug <SLUG> --json
```

## 输出格式约定 —— 发布后必做

**CRITICAL**：执行 `upload` 后，必须把输出中的关键字段明确回报给用户，格式如下（让用户一眼看到链接）：

- `url`：**访问链接**（最重要，必须高亮给出）
- `access`：是 `public`（公开）还是 `protected`（需分享码）
- 若 `protected`：**必须同时给出 `share_code`**，并提醒用户把这个分享码连同链接一起发给对方，否则对方打不开
- `group`：若设置了分组，可顺带告知用户页面归入了哪个分组

示例回报：
> ✅ 已发布：https://site.example.com/v/abc123
> 🔓 公开访问，任何人凭链接即可打开。

或：
> ✅ 已发布：https://site.example.com/v/secret
> 🔒 需要分享码 `cat2024`。把这个分享码连同链接一起发给对方。

执行 `update` / `delete` 后，简短确认结果（已更新 / 已删除 + 最终的 url 与 access 状态）。

## 决策指引

- **要不要设分享码？** 内容含敏感信息或只想给特定人看 → 设分享码；公开演示、作品展示 → 不设。**不确定时默认不设**，并告知用户可随时用 `update --share-code` 补上。
- **要不要自定义 slug？** 想让链接好记（如 `/v/report-q3`）→ 自定义；否则用随机的更短且不可枚举。自定义 slug 冲突时会报错，换一个即可。
- **用户说"改一下已发布的页面"**：先 `html-site info --slug <SLUG>` 确认是哪个页面（或先 `list` 找到 slug），改好本地 HTML 后用 `update --file`。
- **用户说"链接打不开了"**：可能是被删了或加了分享码。用 `list` / `info` 核查当前状态。

## 常见错误排查

| 现象 | 原因与处理 |
|------|-----------|
| `尚未配置 server 地址或 token` | 先 `config set --url ... --token ...`，或让用户提供 |
| `http 401: invalid token` | token 错误或失效，重新向管理员索取 |
| `http 404: page not found`（操作别人的页面时）| slug 不存在或**不属于当前 owner**；权限设计上不区分两者，统一 404 |
| `http 409: slug already taken`（upload 自定义 slug 时）| slug 被占了，换一个或省略 `--slug` 用随机 |
| `--data` 等 flag 没生效 | **flags 必须写在位置参数前面**，例如 `html-site user add --data ./data alice`，而不是 `user add alice --data ./data`（仅 `user` 管理命令涉及） |

## 不在本 Skill 范围

- **server 部署与运维**（`html-site serve`、建 user、备份）属于管理员操作，不在 agent 日常发布流程内；用户问到时指引其查阅 README。
- 本 skill 不负责编写 HTML 内容本身；HTML 应已由你（agent）或用户准备好。

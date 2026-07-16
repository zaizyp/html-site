-- html-site 数据库 schema
-- 仅负责建表（CREATE TABLE IF NOT EXISTS，对新库完整建、对旧库不改动已有表）。
-- 所有索引与历史库的列迁移统一由 migrate.go 处理，避免旧库因缺列导致 CREATE INDEX 失败。

CREATE TABLE IF NOT EXISTS users (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  name          TEXT UNIQUE NOT NULL,
  token         TEXT UNIQUE NOT NULL,
  password_hash TEXT NOT NULL DEFAULT '',  -- bcrypt，空=仅 token 登录，不能登录后台
  role          TEXT NOT NULL DEFAULT 'user', -- 'admin' | 'user'
  created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- groups 必须在 pages 之前创建（pages.group_id 引用 groups）
CREATE TABLE IF NOT EXISTS groups (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  owner_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(owner_id, name)
);

CREATE TABLE IF NOT EXISTS pages (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  slug       TEXT UNIQUE NOT NULL,
  owner_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title      TEXT NOT NULL DEFAULT '',
  share_code TEXT NOT NULL DEFAULT '',   -- 空字符串 = 公开访问；非空 = 需输入分享码
  file_path  TEXT NOT NULL,              -- 磁盘存储相对路径，相对 data/pages
  size_bytes INTEGER NOT NULL DEFAULT 0,
  group_id   INTEGER REFERENCES groups(id) ON DELETE SET NULL, -- 可空，单层分组
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- 后台登录 session
CREATE TABLE IF NOT EXISTS sessions (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token      TEXT UNIQUE NOT NULL,       -- 随机 32 字节 hex，作为 cookie 值
  csrf       TEXT NOT NULL,              -- 绑定该 session 的 CSRF token
  expires_at DATETIME NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- 页面访问统计（PV/UV）。IP 与 UA 仅存 hash，不存明文，兼顾隐私与 UV 去重。
CREATE TABLE IF NOT EXISTS page_views (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  page_id        INTEGER NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
  viewed_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  ip_hash        TEXT NOT NULL DEFAULT '',    -- IP 的 sha256 前 16 hex
  ua_hash        TEXT NOT NULL DEFAULT ''     -- User-Agent 的 sha256 前 16 hex
);

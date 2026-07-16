// config.go：读写客户端配置 ~/.html-site/config.json
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	clientpkg "html-site/internal/client"
)

// Config 存储客户端连接信息。
type Config struct {
	BaseURL string `json:"base_url"`
	Token   string `json:"token"`
}

// configPath 返回配置文件绝对路径：~/.html-site/config.json
func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".html-site", "config.json"), nil
}

// LoadConfig 读取配置文件。文件不存在时返回空 Config（不视为错误）。
func LoadConfig() (Config, error) {
	cfg := Config{}
	// 环境变量优先
	if u := os.Getenv("HTML_SITE_URL"); u != "" {
		cfg.BaseURL = u
	}
	if t := os.Getenv("HTML_SITE_TOKEN"); t != "" {
		cfg.Token = t
	}
	path, err := configPath()
	if err != nil {
		return cfg, nil // 拿不到 home 就只用环境变量
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	var fileCfg Config
	if err := json.Unmarshal(data, &fileCfg); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	// 文件中的值仅在环境变量未设置时生效
	if cfg.BaseURL == "" {
		cfg.BaseURL = fileCfg.BaseURL
	}
	if cfg.Token == "" {
		cfg.Token = fileCfg.Token
	}
	return cfg, nil
}

// SaveConfig 写入配置文件（自动创建目录）。
func SaveConfig(cfg Config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600) // 含 token，限制权限
}

// requireClient 加载配置并校验完整性，返回可用的 Client。
func requireClient() (*Client, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	if cfg.BaseURL == "" || cfg.Token == "" {
		return nil, fmt.Errorf("尚未配置 server 地址或 token，请先执行：\n  html-site config set --url <URL> --token <TOKEN>")
	}
	return NewClient(cfg), nil
}

// Client 是 *client.Client 的本地别名，方便子命令引用。
type Client = clientpkg.Client

// NewClient 用配置构造客户端。
func NewClient(cfg Config) *Client {
	return clientpkg.New(cfg.BaseURL, cfg.Token)
}

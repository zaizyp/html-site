// Package cli 实现 html-site 的全部子命令（serve / config / user / upload / update / list / info / delete）。
//
// 命令分发由 Dispatch 完成；每个子命令是一个独立的函数，接收 os.Args[1:] 之后的参数。
// 输出约定：面向 agent，关键结果（slug/url/share_code）输出为易解析的 key: value 行。
package cli

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"html-site/internal/model"
	"html-site/internal/server"
	"html-site/internal/store"
)

// Dispatch 是入口：根据 args[0] 路由到对应子命令。
// 返回 (exitCode, error)：error 非空时已打印。
func Dispatch(args []string) int {
	if len(args) == 0 {
		printUsage()
		return 1
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "-h", "-help", "--help", "help":
		printUsage()
		return 0
	case "serve":
		return cmdServe(rest)
	case "config":
		return cmdConfig(rest)
	case "user":
		return cmdUser(rest)
	case "upload":
		return cmdUpload(rest)
	case "update":
		return cmdUpdate(rest)
	case "list":
		return cmdList(rest)
	case "info":
		return cmdInfo(rest)
	case "delete":
		return cmdDelete(rest)
	case "version":
		fmt.Println("html-site v0.1.0")
		return 0
	default:
		fmt.Fprintf(os.Stderr, "未知命令：%q\n\n", cmd)
		printUsage()
		return 1
	}
}

func printUsage() {
	fmt.Fprint(os.Stdout, `html-site — 离线 HTML 托管站点（server + client 单二进制）

服务端：
  html-site serve [--addr :8080] [--data ./data] [--url https://...]

本地管理（操作 SQLite）：
  html-site user add [--password P] [--admin] <name>   创建用户
  html-site user passwd <name>                         设置/修改密码
  html-site user list                                  列出所有用户

客户端（调用远端 server，需先 config set）：
  html-site config set --url URL --token TOKEN
  html-site config show
  html-site upload   --file F [--title T] [--share-code C] [--slug S] [--group G]
  html-site update   --slug S [--file F] [--title T] [--share-code C] [--public]
  html-site list
  html-site info     --slug S
  html-site delete   --slug S

环境变量（均可用，优先级低于命令行 flag）：
  HTML_SITE_ADDR     serve 监听地址（默认 :8080）
  HTML_SITE_DATA     数据目录（默认 ./data）
  HTML_SITE_URL      server 对外 URL（serve）或客户端连接地址（client）
  HTML_SITE_TOKEN    客户端 API token（覆盖 config 文件）
`)
}

// ----------------------------------------------------------------------------
// serve
// ----------------------------------------------------------------------------

func cmdServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", envOr("HTML_SITE_ADDR", ":8080"), "监听地址（环境变量 HTML_SITE_ADDR）")
	data := fs.String("data", envOr("HTML_SITE_DATA", "./data"), "数据目录（环境变量 HTML_SITE_DATA）")
	publicURL := fs.String("url", envOr("HTML_SITE_URL", ""), "对外可访问的基础 URL，留空则用请求 Host 推断（环境变量 HTML_SITE_URL）")
	fs.Parse(args)

	st, err := store.Open(*data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "打开存储失败：%v\n", err)
		return 1
	}
	defer st.Close()

	// 启动时清理过期 session
	if n, _ := st.PurgeExpiredSessions(); n > 0 {
		fmt.Fprintf(os.Stderr, "已清理 %d 个过期 session\n", n)
	}

	srv := server.New(st, server.Options{Addr: *addr, PublicURL: *publicURL})
	if err := srv.ListenAndServe(*addr); err != nil {
		fmt.Fprintf(os.Stderr, "服务启动失败：%v\n", err)
		return 1
	}
	return 0
}

// ----------------------------------------------------------------------------
// config
// ----------------------------------------------------------------------------

func cmdConfig(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "用法：html-site config set --url URL --token TOKEN")
		fmt.Fprintln(os.Stderr, "      html-site config show")
		return 1
	}
	switch args[0] {
	case "set":
		fs := flag.NewFlagSet("config set", flag.ExitOnError)
		url := fs.String("url", "", "server 基础 URL，如 https://site.example.com")
		token := fs.String("token", "", "API token")
		fs.Parse(args[1:])
		if *url == "" || *token == "" {
			fmt.Fprintln(os.Stderr, "--url 和 --token 均为必填")
			return 1
		}
		// 保留原有配置中未被覆盖的字段（当前只有这两个）
		cfg := Config{BaseURL: strings.TrimRight(*url, "/"), Token: *token}
		if err := SaveConfig(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "保存配置失败：%v\n", err)
			return 1
		}
		fmt.Println("已保存配置到 ~/.html-site/config.json")
		return 0
	case "show":
		cfg, err := LoadConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "读取配置失败：%v\n", err)
			return 1
		}
		mask := cfg.Token
		if len(mask) > 8 {
			mask = mask[:8] + "…"
		}
		fmt.Printf("base_url: %s\n", cfg.BaseURL)
		fmt.Printf("token:    %s\n", mask)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "未知 config 子命令：%q\n", args[0])
		return 1
	}
}

// ----------------------------------------------------------------------------
// user（本地管理）
// ----------------------------------------------------------------------------

func cmdUser(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "用法：html-site user add <name> | html-site user list")
		return 1
	}
	switch args[0] {
	case "add":
		return cmdUserAdd(args[1:])
	case "list":
		return cmdUserList(args[1:])
	case "passwd":
		return cmdUserPasswd(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "未知 user 子命令：%q\n", args[0])
		return 1
	}
}

func cmdUserAdd(args []string) int {
	fs := flag.NewFlagSet("user add", flag.ExitOnError)
	data := fs.String("data", defaultDataDir(), "数据目录")
	admin := fs.Bool("admin", false, "创建为管理员")
	password := fs.String("password", "", "登录密码（留空则不能登录后台，仅可用 token）")
	fs.Parse(args)
	rest := fs.Args()
	if len(rest) == 0 {
		fmt.Fprintln(os.Stderr, "用法：html-site user add [--admin] [--password PWD] <name>")
		return 1
	}
	name := rest[0]
	st, err := store.Open(*data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "打开存储失败：%v\n", err)
		return 1
	}
	defer st.Close()
	role := ""
	if *admin {
		role = "admin"
	}
	u, err := st.CreateUser(name, role, *password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "创建用户失败：%v\n", err)
		return 1
	}
	// token 仅此处打印一次
	fmt.Printf("name:      %s\n", u.Name)
	fmt.Printf("token:     %s\n", u.Token)
	fmt.Printf("role:      %s\n", u.Role)
	if u.HasPassword {
		fmt.Println("password:  (已设置)")
	} else {
		fmt.Println("password:  (未设置，不能登录后台)")
	}
	fmt.Println("（token 仅显示一次，请妥善保存）")
	return 0
}

// cmdUserPasswd 设置/修改用户密码。
func cmdUserPasswd(args []string) int {
	fs := flag.NewFlagSet("user passwd", flag.ExitOnError)
	data := fs.String("data", defaultDataDir(), "数据目录")
	fs.Parse(args)
	rest := fs.Args()
	if len(rest) == 0 {
		fmt.Fprintln(os.Stderr, "用法：html-site user passwd <name>")
		return 1
	}
	name := rest[0]
	fmt.Printf("为 %s 设置新密码：", name)
	pwd, err := readPasswordFromStdin()
	if err != nil || pwd == "" {
		fmt.Fprintln(os.Stderr, "读取密码失败或密码为空")
		return 1
	}
	st, err := store.Open(*data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "打开存储失败：%v\n", err)
		return 1
	}
	defer st.Close()
	u, err := st.UserByName(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "用户不存在：%v\n", err)
		return 1
	}
	if err := st.SetPassword(u.ID, pwd); err != nil {
		fmt.Fprintf(os.Stderr, "设置密码失败：%v\n", err)
		return 1
	}
	fmt.Println("已更新密码")
	return 0
}

func cmdUserList(args []string) int {
	fs := flag.NewFlagSet("user list", flag.ExitOnError)
	data := fs.String("data", defaultDataDir(), "数据目录")
	fs.Parse(args)
	st, err := store.Open(*data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "打开存储失败：%v\n", err)
		return 1
	}
	defer st.Close()
	users, err := st.ListUsers()
	if err != nil {
		fmt.Fprintf(os.Stderr, "查询失败：%v\n", err)
		return 1
	}
	if len(users) == 0 {
		fmt.Println("（暂无用户，先用 user add 创建）")
		return 0
	}
	fmt.Printf("%-4s %-20s %-12s\n", "ID", "NAME", "CREATED")
	for _, u := range users {
		fmt.Printf("%-4d %-20s %s\n", u.ID, u.Name, u.CreatedAt.Format("2006-01-02 15:04"))
	}
	return 0
}

// defaultDataDir 给本地管理命令一个默认数据目录。
// 优先取环境变量 HTML_SITE_DATA，其次 ./data（与 serve 一致）。
func defaultDataDir() string { return envOr("HTML_SITE_DATA", "./data") }

// envOr 返回环境变量 name 的值，未设置则返回 fallback。
func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// ----------------------------------------------------------------------------
// upload
// ----------------------------------------------------------------------------

func cmdUpload(args []string) int {
	fs := flag.NewFlagSet("upload", flag.ExitOnError)
	file := fs.String("file", "", "HTML 文件路径（必填）")
	title := fs.String("title", "", "页面标题（可选）")
	shareCode := fs.String("share-code", "", "分享码（留空=公开访问）")
	slug := fs.String("slug", "", "自定义 slug（留空=随机生成）")
	group := fs.String("group", "", "归入的分组名（不存在则自动创建）")
	fs.Parse(args)

	if *file == "" {
		fmt.Fprintln(os.Stderr, "--file 为必填")
		return 1
	}
	content, err := os.ReadFile(*file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取文件失败：%v\n", err)
		return 1
	}
	c, err := requireClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	resp, err := c.Upload(model.CreatePageRequest{
		Content:   string(content),
		Title:     *title,
		ShareCode: *shareCode,
		Slug:      *slug,
		GroupName: *group,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "上传失败：%v\n", err)
		return 1
	}
	// 关键字段清晰输出，方便 agent 解析回报
	fmt.Printf("slug:       %s\n", resp.Slug)
	fmt.Printf("url:        %s\n", resp.URL)
	if resp.Group != "" {
		fmt.Printf("group:      %s\n", resp.Group)
	}
	if resp.Public {
		fmt.Println("access:     public（任何人凭链接可访问）")
	} else {
		fmt.Printf("access:     protected（需要分享码）\n")
		fmt.Printf("share_code: %s\n", resp.ShareCode)
	}
	return 0
}

// ----------------------------------------------------------------------------
// update
// ----------------------------------------------------------------------------

func cmdUpdate(args []string) int {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	slug := fs.String("slug", "", "要修改的页面 slug（必填）")
	file := fs.String("file", "", "新的 HTML 文件路径（可选，不传则不改内容）")
	title := fs.String("title", "", "新标题（可选）")
	shareCode := fs.String("share-code", "", "设置分享码；传空串仅在与 --public 互斥时有效")
	public := fs.Bool("public", false, "改为公开访问（清除分享码）")
	fs.Parse(args)

	if *slug == "" {
		fmt.Fprintln(os.Stderr, "--slug 为必填")
		return 1
	}
	c, err := requireClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	req := model.UpdatePageRequest{}
	if *file != "" {
		content, err := os.ReadFile(*file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "读取文件失败：%v\n", err)
			return 1
		}
		s := string(content)
		req.Content = &s
	}
	if *title != "" {
		req.Title = title
	}
	// 分享码/公开：--public 优先；否则用 --share-code
	if *public {
		req.ShareCodeSet = true
		req.ShareCode = ""
	} else if fs.Lookup("share-code").Value.String() != "" || flagWasPassed(fs, "share-code") {
		req.ShareCodeSet = true
		req.ShareCode = *shareCode
	}

	resp, err := c.Update(*slug, req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "更新失败：%v\n", err)
		return 1
	}
	fmt.Printf("updated: true\n")
	if resp.URL != "" {
		fmt.Printf("url:     %s\n", resp.URL)
	}
	if resp.Page != nil {
		if resp.Page.HasCode {
			fmt.Printf("access:  protected\n")
		} else {
			fmt.Printf("access:  public\n")
		}
	}
	return 0
}

// flagWasPassed 判断某 flag 是否在命令行显式传入（用于区分“未传”与“传了空值”）。
func flagWasPassed(fs *flag.FlagSet, name string) bool {
	passed := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			passed = true
		}
	})
	return passed
}

// ----------------------------------------------------------------------------
// list
// ----------------------------------------------------------------------------

func cmdList(args []string) int {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "以 JSON 输出")
	fs.Parse(args)
	c, err := requireClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	pages, err := c.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "查询失败：%v\n", err)
		return 1
	}
	if *jsonOut {
		b, _ := json.MarshalIndent(pages, "", "  ")
		fmt.Println(string(b))
		return 0
	}
	if len(pages) == 0 {
		fmt.Println("（暂无页面）")
		return 0
	}
	fmt.Printf("%-8s %-10s %-12s %-20s %s\n", "SLUG", "ACCESS", "SIZE", "UPDATED", "TITLE")
	for _, p := range pages {
		access := "public"
		if p.HasCode {
			access = "protected"
		}
		fmt.Printf("%-8s %-10s %-12s %-20s %s\n",
			p.Slug, access, humanBytes(p.SizeBytes),
			p.UpdatedAt.Format("2006-01-02 15:04"), p.Title)
	}
	return 0
}

// ----------------------------------------------------------------------------
// info
// ----------------------------------------------------------------------------

func cmdInfo(args []string) int {
	fs := flag.NewFlagSet("info", flag.ExitOnError)
	slug := fs.String("slug", "", "页面 slug（必填）")
	jsonOut := fs.Bool("json", false, "以 JSON 输出")
	fs.Parse(args)
	if *slug == "" {
		fmt.Fprintln(os.Stderr, "--slug 为必填")
		return 1
	}
	c, err := requireClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	info, err := c.Info(*slug)
	if err != nil {
		fmt.Fprintf(os.Stderr, "查询失败：%v\n", err)
		return 1
	}
	if *jsonOut {
		b, _ := json.MarshalIndent(map[string]any{
			"page":    info.Page,
			"content": info.Content,
		}, "", "  ")
		fmt.Println(string(b))
		return 0
	}
	p := info.Page
	fmt.Printf("slug:       %s\n", p.Slug)
	fmt.Printf("title:      %s\n", p.Title)
	fmt.Printf("owner:      %s\n", p.OwnerName)
	fmt.Printf("size:       %s\n", humanBytes(p.SizeBytes))
	if p.HasCode {
		fmt.Printf("access:     protected\n")
		fmt.Printf("share_code: %s\n", p.ShareCode)
	} else {
		fmt.Printf("access:     public\n")
	}
	fmt.Printf("created_at: %s\n", p.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("updated_at: %s\n", p.UpdatedAt.Format("2006-01-02 15:04:05"))
	if info.Content != "" {
		fmt.Println("--- content ---")
		fmt.Println(info.Content)
	}
	return 0
}

// ----------------------------------------------------------------------------
// delete
// ----------------------------------------------------------------------------

func cmdDelete(args []string) int {
	fs := flag.NewFlagSet("delete", flag.ExitOnError)
	slug := fs.String("slug", "", "页面 slug（必填）")
	fs.Parse(args)
	if *slug == "" {
		fmt.Fprintln(os.Stderr, "--slug 为必填")
		return 1
	}
	c, err := requireClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := c.Delete(*slug); err != nil {
		fmt.Fprintf(os.Stderr, "删除失败：%v\n", err)
		return 1
	}
	fmt.Printf("deleted: true\nslug:    %s\n", *slug)
	return 0
}

// ----------------------------------------------------------------------------
// 小工具
// ----------------------------------------------------------------------------

// humanBytes 把字节数格式化为人类可读字符串。
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for nn := n / unit; nn >= unit; nn /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// readPasswordFromStdin 从标准输入读取一行密码（去除换行）。
// 非交互场景下退化为普通读取（不做回显关闭，保持简单可移植）。
func readPasswordFromStdin() (string, error) {
	r := bufio.NewReader(os.Stdin)
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

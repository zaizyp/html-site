// web.go：用 go:embed 把后台模板和静态资源嵌入二进制，保持单文件部署。
package server

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

//go:embed web/tmpl/*.html
var tmplFS embed.FS

//go:embed web/static
var staticFS embed.FS

// tmplFuncs 模板公共函数。
var tmplFuncs = template.FuncMap{
	"hasRole":   func(role, want string) bool { return role == want },
	"SizeHuman": sizeHuman,
	"add":       func(a, b int) int { return a + b },
	"sub":       func(a, b int) int { return a - b },
	"pct":       pctWidth, // count, max -> "NN%"
	"pageURL":   pageURLFn,
}

// pctWidth 返回条形图填充宽度百分比（count/max*100，至少 2%）。
func pctWidth(count, max int) string {
	if max <= 0 {
		return "0%"
	}
	v := count * 100 / max
	if v < 2 && count > 0 {
		v = 2
	}
	return strconv.Itoa(v) + "%"
}

// pageURLFn 生成分页链接，保留 owner/group/q 筛选参数。
// 用法：{{pageURL "/admin/pages" pageNum filterOwner filterGroup query}}
func pageURLFn(base string, page int, owner, group int64, q string) string {
	v := url.Values{}
	v.Set("page", strconv.Itoa(page))
	if owner != 0 {
		v.Set("owner", strconv.FormatInt(owner, 10))
	}
	if group != 0 {
		v.Set("group", strconv.FormatInt(group, 10))
	}
	if q != "" {
		v.Set("q", q)
	}
	return base + "?" + v.Encode()
}

// sizeHuman 字节数 → 人类可读。
func sizeHuman(n int64) string {
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

// renderTmpl 渲染指定模板到 w。data 为模板数据。
// layout 为基础框架，content 为内容块名。
func renderTmpl(w io.Writer, name string, data any) error {
	t, err := template.New("").Funcs(tmplFuncs).ParseFS(tmplFS, "web/tmpl/layout.html", "web/tmpl/"+name)
	if err != nil {
		return err
	}
	// layout.html 里用 {{define "layout"}}...{{template "content" .}} 包裹，
	// 各页面文件 define "content"。这里执行 layout。
	return t.ExecuteTemplate(w, "layout", data)
}

// serveStatic 提供静态资源服务（CSS/JS）。
func serveStatic(w http.ResponseWriter, r *http.Request) {
	// 路径形如 /static/style.css → web/static/style.css
	p := r.URL.Path
	// 精确匹配前缀，避免目录穿越
	data, err := staticFS.ReadFile("web" + p)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	setStaticContentType(w, p)
	_, _ = w.Write(data)
}

// setStaticContentType 根据 расширение 设置 Content-Type。
func setStaticContentType(w http.ResponseWriter, path string) {
	switch {
	case endsWith(path, ".css"):
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case endsWith(path, ".js"):
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	case endsWith(path, ".svg"):
		w.Header().Set("Content-Type", "image/svg+xml")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}
}

func endsWith(s, suf string) bool {
	return len(s) >= len(suf) && s[len(s)-len(suf):] == suf
}

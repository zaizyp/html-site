// upgrade.go：实现 `html-site upgrade` —— 自更新二进制 + 同步技能到所有已知 agent 目录。
//
// 设计要点：
//   - 版本检测：调 GitHub API /releases/latest 拿最新 tag，与当前 Version 比较
//   - 二进制替换：下载到临时文件 → chmod +x → 原子 rename 覆盖 os.Executable()
//     （Windows 上 os.Rename 覆盖正在运行的 .exe 会失败，需先 rename 旧文件再写入新文件）
//   - 技能同步：扫描所有已知 agent skill 目录，凡含 html-site 子目录的就更新 SKILL.md；
//     同时遍历「已安装技能的候选目录」，存在则更新，保持幂等。
package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// GitHub 仓库坐标（发布产物来源）
const (
	githubOwner = "zaizyp"
	githubRepo  = "html-site"
)

// latestRelease 是 GitHub API /releases/latest 返回结构的子集。
type latestRelease struct {
	TagName string `json:"tag_name"`
}

// agentSkillDirs 返回本机所有「可能安装了 html-site 技能」的目录候选。
// 这些是 ZCode / Claude Code / Codex / WorkBuddy 等主流 agent 的用户级 skill 根目录。
// 返回的是「skill 根目录」（如 ~/.agents/skills），调用方负责拼出 html-site 子目录。
func agentSkillDirs() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	candidates := []string{
		filepath.Join(home, ".agents", "skills"),  // ZCode / WorkBuddy
		filepath.Join(home, ".zcode", "skills"),   // ZCode（备用位置）
		filepath.Join(home, ".claude", "skills"),  // Claude Code
		filepath.Join(home, ".codex", "skills"),   // OpenAI Codex
		filepath.Join(home, ".cursor", "skills"),  // Cursor
	}
	var exists []string
	for _, d := range candidates {
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			exists = append(exists, d)
		}
	}
	return exists
}

// fetchLatestTag 调 GitHub API 拿最新 release 的 tag（如 "v1.0.2"）。
func fetchLatestTag() (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", githubOwner, githubRepo)
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API 返回 %s", resp.Status)
	}
	var rel latestRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", err
	}
	if rel.TagName == "" {
		return "", fmt.Errorf("未从 GitHub 解析到 tag_name")
	}
	return rel.TagName, nil
}

// downloadFile 把 url 下载到 dst（覆盖）。
func downloadFile(dst, url string) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载 %s 返回 %s", url, resp.Status)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

// replaceSelf 用 newPath 的内容替换当前正在运行的二进制。
// Windows 不允许覆盖正在运行的可执行文件，采用「旧文件改名 → 新文件就位」策略。
func replaceSelf(newPath string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("无法定位当前二进制：%w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}

	if runtime.GOOS == "windows" {
		// 把旧 exe 改名（.old），再把新文件移到原位置
		old := exe + ".old"
		if _, err := os.Stat(old); err == nil {
			os.Remove(old) // 清理上次残留
		}
		if err := os.Rename(exe, old); err != nil {
			return fmt.Errorf("备份旧二进制失败：%w", err)
		}
		if err := os.Rename(newPath, exe); err != nil {
			// 尽力回滚
			os.Rename(old, exe)
			return fmt.Errorf("替换二进制失败：%w", err)
		}
		// 旧文件留给系统（运行中无法立即删），下次升级时清理
		return nil
	}
	// Unix：直接 rename 覆盖（原子）
	return os.Rename(newPath, exe)
}

// updateSkill 把最新的 SKILL.md 内容写入指定的 skill 根目录下的 html-site 子目录。
// 仅当该根目录存在 html-site 子目录（即用户之前装过）时才更新，不主动创建。
func updateSkill(skillRoot string, content []byte) (bool, error) {
	target := filepath.Join(skillRoot, "html-site", "SKILL.md")
	if _, err := os.Stat(filepath.Join(skillRoot, "html-site")); os.IsNotExist(err) {
		return false, nil // 未安装，跳过
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(target, content, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// cmdUpgrade 实现 `html-site upgrade`。
// 流程：查最新版本 → 比较 → 下载二进制 → 原子替换 → 同步技能到所有已安装目录。
func cmdUpgrade(args []string) int {
	fs := flag.NewFlagSet("upgrade", flag.ExitOnError)
	force := fs.Bool("force", false, "强制升级，跳过版本比较")
	fs.Parse(args)

	fmt.Printf("当前版本：%s\n", Version)

	// 1. 查最新版本
	fmt.Printf("→ 检查最新版本…\n")
	latest, err := fetchLatestTag()
	if err != nil {
		fmt.Fprintf(os.Stderr, "检查最新版本失败：%v\n", err)
		fmt.Fprintln(os.Stderr, "可用 --force 强制重装。")
		return 1
	}
	fmt.Printf("最新版本：%s\n", latest)

	if !*force && normalizeVersion(latest) == normalizeVersion(Version) {
		fmt.Println("✓ 已是最新版本，无需升级。")
		// 即便版本相同，也同步一次技能（SKILL.md 可能单独更新过）
		syncSkills()
		return 0
	}

	// 2. 下载二进制（稳定文件名）
	osName, arch := runtime.GOOS, runtime.GOARCH
	assetName := fmt.Sprintf("html-site_%s_%s", osName, arch)
	if osName == "windows" {
		assetName += ".exe"
	}
	url := fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s",
		githubOwner, githubRepo, latest, assetName)
	fmt.Printf("→ 下载：%s\n", url)

	tmp, err := os.CreateTemp("", "html-site-upgrade-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "创建临时文件失败：%v\n", err)
		return 1
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	if err := downloadFile(tmpPath, url); err != nil {
		fmt.Fprintf(os.Stderr, "下载失败：%v\n", err)
		return 1
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(tmpPath, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "设置可执行权限失败：%v\n", err)
			return 1
		}
	}

	// 3. 原子替换当前二进制
	fmt.Printf("→ 安装到 %s\n", exeLocation())
	if err := replaceSelf(tmpPath); err != nil {
		fmt.Fprintf(os.Stderr, "替换二进制失败：%v\n", err)
		return 1
	}

	// 4. 同步技能
	syncSkills()

	fmt.Printf("\n🎉 已升级到 %s\n", latest)
	fmt.Println("新版本在下次启动 html-site 时生效。")
	return 0
}

// syncSkills 把内置 SKILL.md 同步到所有已安装技能的 agent 目录。
// SKILL.md 来源：优先从 GitHub 仓库 main 分支拉最新版；失败则用本二进制旁的同名文件。
func syncSkills() {
	fmt.Printf("→ 同步技能…\n")
	roots := agentSkillDirs()
	if len(roots) == 0 {
		fmt.Println("  （未检测到已安装技能的 agent 目录，跳过技能同步）")
		return
	}

	// 拉最新 SKILL.md
	skillURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/main/skills/html-site/SKILL.md",
		githubOwner, githubRepo)
	content, err := fetchBytes(skillURL)
	if err != nil {
		// 回退：用本二进制旁的 skills/html-site/SKILL.md（开发/自建场景）
		if fallback := localSkillContent(); fallback != nil {
			content = fallback
		} else {
			fmt.Fprintf(os.Stderr, "  ! 拉取最新 SKILL.md 失败：%v（跳过技能同步）\n", err)
			return
		}
	}

	updated := 0
	for _, root := range roots {
		ok, err := updateSkill(root, content)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ! %s 更新失败：%v\n", root, err)
			continue
		}
		if ok {
			fmt.Printf("  ✓ 已更新：%s\n", filepath.Join(root, "html-site", "SKILL.md"))
			updated++
		}
	}
	if updated == 0 {
		fmt.Println("  （各 agent 目录均未安装过 html-site 技能，未改动；如需安装请重跑 install）")
	}
}

// fetchBytes GET url 返回 body 字节。
func fetchBytes(url string) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// localSkillContent 尝试读取当前二进制旁的 skills/html-site/SKILL.md（自建/开发回退用）。
func localSkillContent() []byte {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	// 开发期：源码树里的路径
	candidates := []string{
		filepath.Join(filepath.Dir(exe), "skills", "html-site", "SKILL.md"),
	}
	for _, p := range candidates {
		if b, err := os.ReadFile(p); err == nil {
			return b
		}
	}
	return nil
}

// exeLocation 返回当前二进制的可读路径（用于日志）。
func exeLocation() string {
	exe, err := os.Executable()
	if err != nil {
		return "(未知)"
	}
	return exe
}

// normalizeVersion 去掉版本号前后的 'v' 和空白，便于比较。
func normalizeVersion(v string) string {
	return strings.Trim(strings.TrimPrefix(strings.TrimSpace(v), "v"), " ")
}

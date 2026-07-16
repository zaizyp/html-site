// page_path.go：页面 HTML 文件的磁盘二级目录路径规则。
//
// 存储结构（相对 pagesDir）：
//
//	pages/u<owner_id>/g<group_id>/<slug>.html
//
// 其中未分组页面的 group_id 用 0，目录名 g0。
//
// 设计动机：
//   - 用稳定的数字 ID 作目录名，而非用户名/分组名。这样重命名用户或分组
//     不会破坏磁盘路径，也不必移动文件。
//   - slug 仍走 NormalizeSlug 校验（仅 A-Za-z0-9-_），杜绝目录穿越。
//   - file_path 列语义不变（相对 pagesDir 的相对路径），只是值变为二级路径。
package store

import "fmt"

// pageRelPath 返回某页面相对 pagesDir 的存储路径。
// groupID 为 0 表示未分组（落到 g0 目录）。
func pageRelPath(ownerID, groupID int64, slug string) string {
	return fmt.Sprintf("u%d/g%d/%s.html", ownerID, groupID, slug)
}

// isNestedPath 判断 file_path 是否已是二级目录形式（含 /）。
// 旧的平铺路径形如 "abc123.html"（不含 /），迁移时据此识别。
func isNestedPath(rel string) bool {
	for i := 0; i < len(rel); i++ {
		if rel[i] == '/' {
			return true
		}
	}
	return false
}

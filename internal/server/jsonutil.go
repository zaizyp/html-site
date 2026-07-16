// jsonutil.go：JSON 编解码小工具。
package server

import "encoding/json"

// decodeJSONBytes 从字节切片解码到目标结构。
func decodeJSONBytes(body []byte, dst any) error {
	return json.Unmarshal(body, dst)
}

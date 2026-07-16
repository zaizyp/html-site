// html-site 命令行入口。
//
// 单二进制，包含服务端（serve）与客户端（upload/update/...）两套子命令。
// 子命令分发委托给 cli.Dispatch。
package main

import (
	"os"

	"html-site/internal/cli"
)

func main() {
	os.Exit(cli.Dispatch(os.Args[1:]))
}

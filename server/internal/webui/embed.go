// Package webui 嵌入前端构建产物。
//
// 构建前由 scripts/copy-dist.mjs 把 web/dist 拷贝到本目录 dist/。
// 未构建前端时 dist/.gitkeep 保证 //go:embed 能通过编译。
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// FS 返回以 dist 为根的文件系统。
func FS() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}

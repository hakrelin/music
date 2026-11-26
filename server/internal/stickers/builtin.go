package stickers

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

//go:embed builtin
var builtinFS embed.FS

// builtinPrefix 是内置表情显示名的前缀（「内置表情 01」…）。
const builtinPrefix = "内置表情 "

// SeedBuiltin 把编译进二进制的内置表情写进表情库。
//
// 幂等：ID 用文件内容的 sha256 前 16 位十六进制，已经存在的直接跳过，
// 所以每次启动跑一遍是安全的，也不会产生重复项。
// 返回本次新增的条数（首次启动是全部，之后是 0）。
func (s *Store) SeedBuiltin() (int, error) {
	entries, err := fs.ReadDir(builtinFS, "builtin")
	if err != nil {
		return 0, fmt.Errorf("读取内置表情失败: %w", err)
	}

	// 按文件名排序，保证「内置表情 01…N」的顺序稳定（不受目录遍历顺序影响）。
	var names []string
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	added := 0
	for idx, name := range names {
		if path.Ext(name) == ".md" || path.Ext(name) == ".txt" {
			continue
		}
		data, err := builtinFS.ReadFile("builtin/" + name)
		if err != nil {
			return added, fmt.Errorf("读取内置表情 %s 失败: %w", name, err)
		}
		id := builtinID(data)
		if _, exists := s.Get(id); exists {
			continue
		}
		display := fmt.Sprintf("%s%02d", builtinPrefix, idx+1)
		if _, err := s.add(id, display, data, true); err != nil {
			// 单张失败不阻断其它：内置图里偶尔混进超大 GIF 也不该让服务起不来。
			s.logSkip(name, err)
			continue
		}
		added++
	}
	return added, nil
}

// builtinID 由内容算出稳定的表情 ID（sha256 前 16 hex，符合 idPattern 且够短）。
func builtinID(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}

// logSkip 内置表情导入失败时给出可定位的日志，但不影响启动。
func (s *Store) logSkip(name string, err error) {
	s.mu.Lock()
	s.skipped = append(s.skipped, name+": "+err.Error())
	s.mu.Unlock()
}

// BuiltinSkipped 返回内置表情导入时被跳过的条目（诊断用）。
func (s *Store) BuiltinSkipped() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, len(s.skipped))
	copy(out, s.skipped)
	return out
}

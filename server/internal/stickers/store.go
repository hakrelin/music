// Package stickers 是房间聊天用的表情包（图片）本地库。
//
// 设计取舍：
//   - 文件直接落在 dataDir/stickers/ 下，元数据存 index.json，不依赖对象存储
//     （表情包很小，几百 KB 级，放本地磁盘足够，也避免和音频缓存抢配额）。
//   - 只接受 png/jpeg/gif/webp：SVG 能带脚本，直接拒掉，避免 XSS。
//   - ID 由服务端生成且只允许十六进制，读取时再做一次白名单校验，杜绝路径穿越。
//   - 元数据整体原子写（临时文件 + rename），崩了最多丢最后一次导入。
package stickers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// MaxItemBytes 是单张表情包上限（2MB）。聊天贴图不需要更大。
	MaxItemBytes = 2 << 20
	// DefaultMaxItems 是表情库总数量上限，防止被灌爆磁盘。
	DefaultMaxItems = 300
	// maxNameRunes 限制显示名长度，避免名字过长撑破面板。
	maxNameRunes = 48
	indexFile    = "index.json"
)

var (
	// ErrTooLarge 表示上传的图片超过 MaxItemBytes。
	ErrTooLarge = errors.New("表情包太大（上限 2MB）")
	// ErrUnsupported 表示上传的文件不是允许的图片类型。
	ErrUnsupported = errors.New("只支持 PNG / JPEG / GIF / WebP")
	// ErrFull 表示表情库已达到数量上限。
	ErrFull = errors.New("表情包数量已达上限，先删几个再导入")
	// ErrNotFound 表示 ID 不存在。
	ErrNotFound = errors.New("表情包不存在")
	// ErrBuiltin 表示这是编译进二进制的内置表情，不允许删除
	// （删了下次启动也会被重新播种，不如直接拒绝，避免误解）。
	ErrBuiltin = errors.New("内置表情不能删除")

	extByMIME = map[string]string{
		"image/png":  ".png",
		"image/jpeg": ".jpg",
		"image/gif":  ".gif",
		"image/webp": ".webp",
	}
	idPattern = regexp.MustCompile(`^[a-f0-9]{12,32}$`)
)

// Item 是表情包的元数据。
type Item struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	MIME    string `json:"mime"`
	Size    int64  `json:"size"`
	Created int64  `json:"created"`
	// Builtin 标记编译进二进制的内置表情：前端不提供删除按钮，
	// 服务端也允许它们被重新播种回来。
	Builtin bool `json:"builtin,omitempty"`
}

// URL 返回表情包对外可访问的路径（前端直接当 <img src>）。
func (i Item) URL() string { return "/api/stickers/" + i.ID }

// Store 是表情包库。
type Store struct {
	dir      string
	maxItems int

	mu    sync.RWMutex
	items []Item
	// skipped 记录内置表情导入时被跳过的条目（类型不支持/超大等），仅用于日志诊断。
	skipped []string
}

type indexPayload struct {
	Version int    `json:"version"`
	Items   []Item `json:"items"`
}

// New 打开（必要时创建）目录并加载索引。
func New(dir string) (*Store, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, errors.New("stickers: dataDir 为空")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("stickers: 创建目录失败: %w", err)
	}
	s := &Store{dir: dir, maxItems: DefaultMaxItems}
	s.load()
	return s, nil
}

// Dir 返回文件落盘目录（日志用）。
func (s *Store) Dir() string { return s.dir }

// List 返回按创建时间倒序的表情包（最新导入的排前面）。
func (s *Store) List() []Item {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Item, len(s.items))
	copy(out, s.items)
	return out
}

// Get 按 ID 取元数据。
func (s *Store) Get(id string) (Item, bool) {
	if !idPattern.MatchString(id) {
		return Item{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, it := range s.items {
		if it.ID == id {
			return it, true
		}
	}
	return Item{}, false
}

// Open 打开表情包文件，调用方负责关闭。
func (s *Store) Open(id string) (Item, *os.File, error) {
	it, ok := s.Get(id)
	if !ok {
		return Item{}, nil, ErrNotFound
	}
	ext := extByMIME[it.MIME]
	if ext == "" {
		return Item{}, nil, ErrUnsupported
	}
	f, err := os.Open(filepath.Join(s.dir, it.ID+ext))
	if err != nil {
		return Item{}, nil, ErrNotFound
	}
	return it, f, nil
}

// Add 导入一张表情包。data 为原始字节，MIME 由内容嗅探决定，ID 随机生成。
func (s *Store) Add(name string, data []byte) (Item, error) {
	return s.add("", name, data, false)
}

// add 是导入的实际实现。id 为空时随机生成；内置表情传内容哈希，保证幂等。
func (s *Store) add(id, name string, data []byte, builtin bool) (Item, error) {
	if len(data) == 0 {
		return Item{}, errors.New("文件是空的")
	}
	if len(data) > MaxItemBytes {
		return Item{}, ErrTooLarge
	}
	sniffLen := len(data)
	if sniffLen > 512 {
		sniffLen = 512
	}
	mime := strings.ToLower(strings.TrimSpace(strings.Split(http.DetectContentType(data[:sniffLen]), ";")[0]))
	ext, ok := extByMIME[mime]
	if !ok {
		return Item{}, ErrUnsupported
	}
	if id != "" && !idPattern.MatchString(id) {
		return Item{}, fmt.Errorf("非法的表情包 ID: %q", id)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.maxItems > 0 && len(s.items) >= s.maxItems {
		return Item{}, ErrFull
	}
	for _, it := range s.items {
		if id != "" && it.ID == id {
			return it, nil // 已经导入过（内置表情重复播种）
		}
	}
	if id == "" {
		generated, err := newID()
		if err != nil {
			return Item{}, err
		}
		id = generated
	}

	final := filepath.Join(s.dir, id+ext)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return Item{}, fmt.Errorf("写入表情包失败: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return Item{}, fmt.Errorf("落盘表情包失败: %w", err)
	}

	item := Item{
		ID:      id,
		Name:    cleanName(name, id+ext),
		MIME:    mime,
		Size:    int64(len(data)),
		Created: time.Now().Unix(),
		Builtin: builtin,
	}
	s.items = append(s.items, item)
	sortItems(s.items)
	if err := s.saveLocked(); err != nil {
		// 列表按「内置优先 + 创建时间倒序」排过序，必须按 ID 回滚，
		// 不能简单截断尾部（尾部可能是别人导入的老表情）。
		kept := s.items[:0]
		for _, it := range s.items {
			if it.ID != item.ID {
				kept = append(kept, it)
			}
		}
		s.items = kept
		_ = os.Remove(final)
		return Item{}, err
	}
	return item, nil
}

// sortItems 统一排序：内置表情排最前，其余按导入时间倒序。
func sortItems(items []Item) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Builtin != items[j].Builtin {
			return items[i].Builtin
		}
		return items[i].Created > items[j].Created
	})
}

// Delete 删除表情包（先摘索引再删文件，索引写失败则回滚）。
func (s *Store) Delete(id string) error {
	if !idPattern.MatchString(id) {
		return ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := -1
	for i, it := range s.items {
		if it.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ErrNotFound
	}
	removed := s.items[idx]
	if removed.Builtin {
		return ErrBuiltin
	}
	s.items = append(s.items[:idx:idx], s.items[idx+1:]...)
	if err := s.saveLocked(); err != nil {
		return err
	}
	if ext := extByMIME[removed.MIME]; ext != "" {
		_ = os.Remove(filepath.Join(s.dir, removed.ID+ext))
	}
	return nil
}

func (s *Store) load() {
	raw, err := os.ReadFile(filepath.Join(s.dir, indexFile))
	if err != nil {
		return
	}
	var payload indexPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		// 索引坏了不影响启动：当空库处理，文件仍在磁盘上，之后可手工恢复。
		return
	}
	valid := payload.Items[:0]
	for _, it := range payload.Items {
		if idPattern.MatchString(it.ID) && extByMIME[it.MIME] != "" {
			valid = append(valid, it)
		}
	}
	s.items = valid
	sortItems(s.items)
}

func (s *Store) saveLocked() error {
	payload := indexPayload{Version: 1, Items: s.items}
	if payload.Items == nil {
		payload.Items = []Item{}
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化表情索引失败: %w", err)
	}
	final := filepath.Join(s.dir, indexFile)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return fmt.Errorf("写入表情索引失败: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("替换表情索引失败: %w", err)
	}
	return nil
}

// CopyTo 把表情包内容写进 io.Writer（供 HTTP 层使用，便于统一做 Range/超时）。
func (s *Store) CopyTo(id string, w io.Writer) (Item, error) {
	it, f, err := s.Open(id)
	if err != nil {
		return Item{}, err
	}
	defer f.Close()
	if _, err := io.Copy(w, f); err != nil {
		return it, err
	}
	return it, nil
}

func newID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成表情包 ID 失败: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// cleanName 只保留可读名字：去掉控制字符、限长，空则用文件名兜底。
func cleanName(name, fallback string) string {
	name = strings.TrimSpace(name)
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	if name == "" {
		name = fallback
	}
	r := []rune(name)
	if len(r) > maxNameRunes {
		name = string(r[:maxNameRunes])
	}
	return name
}

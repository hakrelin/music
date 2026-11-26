package stickers

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

// 1x1 透明 PNG：够真实也够小。
const pngOnePixel = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(pngOnePixel)
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	return data
}

func TestAddListDelete(t *testing.T) {
	s := newTestStore(t)
	item, err := s.Add("doge.png", pngBytes(t))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !idPattern.MatchString(item.ID) {
		t.Fatalf("ID 不合规: %q", item.ID)
	}
	if item.MIME != "image/png" {
		t.Fatalf("MIME = %q, want image/png", item.MIME)
	}
	if item.Name != "doge.png" {
		t.Fatalf("Name = %q", item.Name)
	}
	if item.URL() != "/api/stickers/"+item.ID {
		t.Fatalf("URL = %q", item.URL())
	}
	if got := len(s.List()); got != 1 {
		t.Fatalf("List 长度 = %d, want 1", got)
	}

	// 文件可读 + 重开能恢复索引
	_, f, err := s.Open(item.ID)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_ = f.Close()

	again, err := New(s.Dir())
	if err != nil {
		t.Fatalf("重开: %v", err)
	}
	if got := len(again.List()); got != 1 {
		t.Fatalf("重开后列表长度 = %d, want 1", got)
	}

	if err := s.Delete(item.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := s.Get(item.ID); ok {
		t.Fatal("删除后仍能 Get 到")
	}
	if _, _, err := s.Open(item.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("删除后 Open err = %v, want ErrNotFound", err)
	}
}

func TestRejectNonImageAndOversize(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Add("a.txt", []byte("hello world")); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("非图片 err = %v, want ErrUnsupported", err)
	}
	big := make([]byte, MaxItemBytes+1)
	copy(big, pngBytes(t))
	if _, err := s.Add("big.png", big); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("超大 err = %v, want ErrTooLarge", err)
	}
	if _, err := s.Add("empty.png", nil); err == nil {
		t.Fatal("空文件应当报错")
	}
}

func TestIDWhitelistBlocksTraversal(t *testing.T) {
	s := newTestStore(t)
	for _, bad := range []string{"../../etc/passwd", "..", "abc/../../x", "ABCDEF0123456789", "short"} {
		if _, ok := s.Get(bad); ok {
			t.Fatalf("Get(%q) 应当拒绝", bad)
		}
		if err := s.Delete(bad); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Delete(%q) err = %v, want ErrNotFound", bad, err)
		}
	}
}

// 内置表情：播种幂等、排在列表最前、且不允许删除。
func TestSeedBuiltin(t *testing.T) {
	s := newTestStore(t)

	added, err := s.SeedBuiltin()
	if err != nil {
		t.Fatalf("SeedBuiltin: %v", err)
	}
	if added == 0 {
		t.Fatal("内置表情一张都没导入")
	}
	items := s.List()
	if len(items) != added {
		t.Fatalf("播种后条数 = %d, want %d", len(items), added)
	}
	for _, it := range items {
		if !it.Builtin {
			t.Fatalf("%s 没有 builtin 标记", it.ID)
		}
		if !strings.HasPrefix(it.Name, builtinPrefix) {
			t.Fatalf("内置表情名字 = %q", it.Name)
		}
		if _, ok := s.Get(it.ID); !ok {
			t.Fatalf("播种后取不到 %s", it.ID)
		}
	}
	if skipped := s.BuiltinSkipped(); len(skipped) > 0 {
		t.Fatalf("有内置表情被跳过: %v", skipped)
	}

	// 再跑一次：不新增、不重复
	again, err := s.SeedBuiltin()
	if err != nil {
		t.Fatalf("二次 SeedBuiltin: %v", err)
	}
	if again != 0 {
		t.Fatalf("二次播种新增 = %d, want 0", again)
	}
	if got := len(s.List()); got != added {
		t.Fatalf("二次播种后条数 = %d, want %d", got, added)
	}

	// 重开库再来一次：索引里的内置项也被识别为已存在
	reopened, err := New(s.Dir())
	if err != nil {
		t.Fatalf("重开: %v", err)
	}
	if again, err := reopened.SeedBuiltin(); err != nil || again != 0 {
		t.Fatalf("重开后播种 = %d, %v", again, err)
	}

	// 内置表情拒绝删除
	if err := s.Delete(items[0].ID); !errors.Is(err, ErrBuiltin) {
		t.Fatalf("删除内置表情 err = %v, want ErrBuiltin", err)
	}
	if _, ok := s.Get(items[0].ID); !ok {
		t.Fatal("拒绝删除后条目却消失了")
	}

	// 自己导入的表情排在内置之后
	own, err := s.Add("my.png", pngBytes(t))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got := s.List()[len(s.List())-1]; got.ID != own.ID {
		t.Fatalf("自定义表情没有排到最后: %+v", got)
	}
}

func TestNameCleaningAndLimit(t *testing.T) {	s := newTestStore(t)
	item, err := s.Add(strings.Repeat("啊", 80), pngBytes(t))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got := len([]rune(item.Name)); got != maxNameRunes {
		t.Fatalf("名字长度 = %d, want %d", got, maxNameRunes)
	}
	// 空名字回落到文件名
	item2, err := s.Add("   ", pngBytes(t))
	if err != nil {
		t.Fatalf("Add(空名): %v", err)
	}
	if item2.Name == "" {
		t.Fatal("空名字没有兜底")
	}
}

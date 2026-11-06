package demo

import (
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
)

// coverCache 缓存生成的 SVG：每张只有 1–2KB，24 首 + 2 个歌单可以全放内存。
var coverCache = struct {
	sync.RWMutex
	m map[string][]byte
}{m: make(map[string][]byte, 32)}

// 深绿/米白的几组配色，按 id 取模挑选，让每张封面有微妙差异。
var palettes = [][3]string{
	{"#0e2a20", "#2f5d47", "#f2e9d4"},
	{"#123126", "#3c6b52", "#efe4c9"},
	{"#16382b", "#4a7c5f", "#f5edd9"},
	{"#0b241b", "#2a5340", "#e9dfc6"},
	{"#1a4030", "#56866a", "#f7f0de"},
	{"#102c22", "#356049", "#f0e6cf"},
}

// Cover 返回曲目/歌单的 SVG 封面。未知 id 返回 false。
//
// 封面是"深绿底 + 米白几何图形"的抽象插画，不是图片资源：
// 后端现算既省仓库体积，也让每首曲子的封面有稳定的个性。
func (p *Provider) Cover(id string) ([]byte, bool) {
	id = strings.TrimSpace(id)
	known := p.HasTrack(id) || id == PlaylistID || id == "demo-night" || id == "playlist" || id == "playlist-night"
	if !known {
		return nil, false
	}
	coverCache.RLock()
	if b, ok := coverCache.m[id]; ok {
		coverCache.RUnlock()
		return b, true
	}
	coverCache.RUnlock()

	svg := buildCoverSVG(id)
	coverCache.Lock()
	coverCache.m[id] = svg
	coverCache.Unlock()
	return svg, true
}

// buildCoverSVG 生成 400x400 的抽象封面。
func buildCoverSVG(id string) []byte {
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	sum := h.Sum64()
	pal := palettes[int(sum%uint64(len(palettes)))]
	dark, mid, cream := pal[0], pal[1], pal[2]

	// 用哈希派生几个几何参数，保证同一 id 的封面永远一样。
	moonX := 120 + float64((sum>>8)%160)
	moonY := 90 + float64((sum>>16)%110)
	moonR := 34 + float64((sum>>24)%26)
	hill := 250 + float64((sum>>32)%60)
	rot := float64((sum>>40)%90) - 45

	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="400" height="400" viewBox="0 0 400 400">`)
	// 斜向渐变底
	b.WriteString(`<defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1">`)
	fmt.Fprintf(&b, `<stop offset="0" stop-color="%s"/><stop offset="0.62" stop-color="%s"/><stop offset="1" stop-color="%s"/>`, dark, mid, cream)
	b.WriteString(`</linearGradient>`)
	b.WriteString(`<linearGradient id="v" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#000" stop-opacity="0"/><stop offset="1" stop-color="#000" stop-opacity="0.32"/></linearGradient></defs>`)
	b.WriteString(`<rect width="400" height="400" fill="url(#g)"/>`)

	// 米白月轮
	fmt.Fprintf(&b, `<circle cx="%.1f" cy="%.1f" r="%.1f" fill="%s" opacity="0.92"/>`, moonX, moonY, moonR, cream)
	fmt.Fprintf(&b, `<circle cx="%.1f" cy="%.1f" r="%.1f" fill="%s" opacity="0.18"/>`, moonX+moonR*0.55, moonY-moonR*0.35, moonR*0.9, cream)

	// 细线网格：像乐谱又像夜色里的经纬
	b.WriteString(`<g stroke="` + cream + `" stroke-width="0.7" opacity="0.22">`)
	for i := 60; i < 400; i += 56 {
		fmt.Fprintf(&b, `<line x1="0" y1="%d" x2="400" y2="%d"/>`, i, i)
	}
	b.WriteString(`</g>`)

	// 山影（折线）
	fmt.Fprintf(&b, `<path d="M0 %.1f L90 %.1f L150 %.1f L230 %.1f L320 %.1f L400 %.1f L400 400 L0 400 Z" fill="%s" opacity="0.55"/>`,
		hill, hill-58, hill-20, hill-74, hill-30, hill-46, dark)
	fmt.Fprintf(&b, `<path d="M0 %.1f L70 %.1f L160 %.1f L250 %.1f L400 %.1f L400 400 L0 400 Z" fill="#000" opacity="0.22"/>`,
		hill+70, hill+30, hill+78, hill+16, hill+44)

	// 斜向几何色块 + 小圆点，给画面一点构建感
	fmt.Fprintf(&b, `<g transform="rotate(%.1f 200 200)" opacity="0.5">`, rot)
	fmt.Fprintf(&b, `<rect x="238" y="252" width="86" height="86" fill="none" stroke="%s" stroke-width="1.1"/>`, cream)
	fmt.Fprintf(&b, `<rect x="256" y="270" width="50" height="50" fill="%s" opacity="0.16"/>`, cream)
	b.WriteString(`</g>`)
	fmt.Fprintf(&b, `<circle cx="84" cy="316" r="3" fill="%s" opacity="0.8"/>`, cream)
	fmt.Fprintf(&b, `<circle cx="104" cy="316" r="1.6" fill="%s" opacity="0.55"/>`, cream)
	b.WriteString(`<rect width="400" height="400" fill="url(#v)"/>`)

	// 底部标题条
	b.WriteString(`<g font-family="'Songti SC','SimSun',serif" fill="` + cream + `">`)
	b.WriteString(`<text x="34" y="352" font-size="17" opacity="0.92">东方电台（演示）</text>`)
	fmt.Fprintf(&b, `<text x="34" y="374" font-size="10" letter-spacing="2" opacity="0.6">%s</text>`, strings.ToUpper(id))
	b.WriteString(`</g></svg>`)
	return []byte(b.String())
}

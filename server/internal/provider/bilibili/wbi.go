package bilibili

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

// mixinKeyEncTab 是 B 站 WBI 签名的固定重排表（64 位截断为 32 位 mixinKey）。
var mixinKeyEncTab = [64]int{
	46, 47, 18, 2, 53, 8, 23, 32, 15, 50, 10, 31, 58, 3, 45, 35, 27, 43, 5, 49,
	33, 9, 42, 19, 29, 28, 14, 39, 12, 38, 41, 13, 37, 48, 7, 16, 24, 55, 40,
	61, 26, 17, 0, 1, 60, 51, 30, 4, 22, 25, 54, 21, 56, 59, 6, 63, 57, 62, 11,
	36, 20, 34, 44, 52,
}

const (
	wbiCacheTTL   = 6 * time.Hour
	buvidCacheTTL = 24 * time.Hour
)

func getMixinKey(imgKey, subKey string) string {
	orig := imgKey + subKey
	if len(orig) < 64 {
		return ""
	}
	var b strings.Builder
	b.Grow(32)
	for i := 0; i < 32; i++ {
		idx := mixinKeyEncTab[i]
		if idx >= 0 && idx < len(orig) {
			b.WriteByte(orig[idx])
		}
	}
	return b.String()
}

func extractWBIKey(rawURL string) string {
	u := strings.TrimSpace(rawURL)
	if u == "" {
		return ""
	}
	if parsed, err := url.Parse(u); err == nil && parsed.Path != "" {
		u = parsed.Path
	}
	base := path.Base(u)
	if dot := strings.LastIndexByte(base, '.'); dot > 0 {
		base = base[:dot]
	}
	return strings.TrimSpace(base)
}

// filterWBIValue 剔除 B 站 WBI 规范要求过滤的特殊字符。
func filterWBIValue(v string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '!', '\'', '(', ')', '*':
			return -1
		default:
			return r
		}
	}, v)
}

// encodeWBICanonical 按 key 升序拼接，并使用 %20 编码空格（B 站 JS encodeURIComponent 语义，
// 避免 Go url.Values.Encode 把空格编成 '+' 导致带空格关键词报 -352）。
func encodeWBICanonical(params url.Values) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		val := filterWBIValue(params.Get(k))
		encK := strings.ReplaceAll(url.QueryEscape(k), "+", "%20")
		encV := strings.ReplaceAll(url.QueryEscape(val), "+", "%20")
		parts = append(parts, encK+"="+encV)
	}
	return strings.Join(parts, "&")
}

// signWBIQuery 计算 wts 与 w_rid，返回可直接拼在 URL 后的查询串。
func signWBIQuery(params url.Values, mixinKey string, now time.Time) string {
	cp := make(url.Values, len(params)+2)
	for k, vals := range params {
		if len(vals) > 0 {
			cp.Set(k, vals[0])
		}
	}
	cp.Set("wts", strconv.FormatInt(now.Unix(), 10))
	canonical := encodeWBICanonical(cp)
	sum := md5.Sum([]byte(canonical + mixinKey))
	return canonical + "&w_rid=" + hex.EncodeToString(sum[:])
}

func (p *Provider) invalidateWBI() {
	p.mu.Lock()
	p.wbiMixin = ""
	p.wbiExp = time.Time{}
	p.mu.Unlock()
}

func (p *Provider) ensureWBIKey(ctx context.Context) (string, error) {
	p.mu.RLock()
	mixin := p.wbiMixin
	exp := p.wbiExp
	p.mu.RUnlock()
	if mixin != "" && time.Now().Before(exp) {
		return mixin, nil
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			WbiImg struct {
				ImgURL string `json:"img_url"`
				SubURL string `json:"sub_url"`
			} `json:"wbi_img"`
		} `json:"data"`
	}
	// nav 接口即使未登录也会返回 wbi_img；携带当前 cookie 更稳定。
	if err := p.getJSON(ctx, p.apiEndpoint("/x/web-interface/nav"), p.outgoingCookie(ctx, false), &resp); err != nil {
		return "", fmt.Errorf("获取 B 站 WBI 密钥失败: %w", err)
	}
	imgKey := extractWBIKey(resp.Data.WbiImg.ImgURL)
	subKey := extractWBIKey(resp.Data.WbiImg.SubURL)
	mixin = getMixinKey(imgKey, subKey)
	if len(mixin) != 32 {
		return "", fmt.Errorf("B 站 WBI 密钥无效（img=%q sub=%q）", imgKey, subKey)
	}
	p.mu.Lock()
	p.wbiMixin = mixin
	p.wbiExp = time.Now().Add(wbiCacheTTL)
	p.mu.Unlock()
	return mixin, nil
}

// ensureBuvid 从 /x/frontend/finger/spi 补齐 buvid3/buvid4，防止仅有 SESSDATA 时搜索触发 -412。
func (p *Provider) ensureBuvid(ctx context.Context) (string, string) {
	p.mu.RLock()
	b3, b4, exp := p.buvid3, p.buvid4, p.buvidExp
	p.mu.RUnlock()
	if b3 != "" && time.Now().Before(exp) {
		return b3, b4
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			B3 string `json:"b_3"`
			B4 string `json:"b_4"`
		} `json:"data"`
	}
	// 单独短超时获取指纹，失败不阻断主请求。
	sctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	if err := p.getJSON(sctx, p.apiEndpoint("/x/frontend/finger/spi"), "", &resp); err != nil || resp.Code != 0 {
		return b3, b4
	}
	b3 = strings.TrimSpace(resp.Data.B3)
	b4 = strings.TrimSpace(resp.Data.B4)
	if b3 != "" {
		p.mu.Lock()
		p.buvid3 = b3
		p.buvid4 = b4
		p.buvidExp = time.Now().Add(buvidCacheTTL)
		p.mu.Unlock()
	}
	return b3, b4
}

func (p *Provider) getJSON(ctx context.Context, rawURL, cookie string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", biliUA)
	req.Header.Set("Referer", "https://www.bilibili.com/")
	req.Header.Set("Origin", "https://www.bilibili.com")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := p.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet(body))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("响应非 JSON: %w（%s）", err, snippet(body))
	}
	return nil
}

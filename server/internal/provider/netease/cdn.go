package netease

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// 网易云音频 CDN 的主机池。
//
// 背景：同一份签名（路径里的 vuutv / authSecret / cdntag 与主机无关）在
// m701 / m801 上能拿到 206，在 m704 / m804 / m10 上直接 403，m800 甚至超时。
// 这是节点级风控，跟签名是否过期、来源 IP 是不是 IPv6 都没关系：把 host
// 换成健康节点、路径和查询串原样重放就能正常拉流。
//
// 因此解析出来的地址先探一次，不通就并行试池子里的其它主机。
var cdnHostPool = []string{
	"m701", "m801", "m702", "m802", "m703", "m803", "m705", "m805",
}

const (
	// 探测是并行发的，整体上限就是这个时间。
	cdnProbeTimeout = 2500 * time.Millisecond
	// 探测结果的记忆时长：健康的节点没必要每次都再探一遍。
	cdnProbeMemo = 5 * time.Minute
	// 探测只要 1KB，别把整首拉下来。
	cdnProbeBytes = 1024
)

// audioUA 是拉音频时用的 UA，跟页面保持一致。
const audioUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

var cdnProbeClient = &http.Client{Timeout: cdnProbeTimeout}

type cdnMemoEntry struct {
	ok  bool
	exp time.Time
}

var (
	cdnMemoMu sync.Mutex
	cdnMemo   = map[string]cdnMemoEntry{}
)

// settleCDN 保证返回的地址指向一个真的能拉到数据的 CDN 节点。
// 第二个返回值 false 表示所有候选节点都不通（调用方可以重新要一次签名，
// 网易云每次可能给出不同的主机）；此时 url 仍是原地址，用于兜底。
func settleCDN(ctx context.Context, raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw, false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw, false
	}
	host := strings.Split(u.Host, ".")[0]
	if !strings.HasPrefix(host, "m") {
		// 不是 126.net 的 mXX 节点，不做改写，按可用处理。
		return raw, true
	}
	if ok, known := cdnKnown(host); known && ok {
		return raw, true
	}

	cands := make([]string, 0, len(cdnHostPool)+1)
	cands = append(cands, host)
	for _, h := range cdnHostPool {
		if h != host {
			cands = append(cands, h)
		}
	}

	// 并行探测：最坏情况也只等一个 cdnProbeTimeout。
	results := make(map[string]bool, len(cands))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, h := range cands {
		h := h
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok := probeAudioURL(ctx, raw, h)
			mu.Lock()
			results[h] = ok
			mu.Unlock()
			cdnRemember(h, ok)
		}()
	}
	wg.Wait()

	if results[host] {
		return raw, true
	}
	best := ""
	for _, h := range cdnHostPool {
		if results[h] {
			best = h
			break
		}
	}
	if best == "" {
		slog.Default().Warn("所有 CDN 节点都探测失败", "host", host)
		return raw, false
	}
	rewritten := *u
	rewritten.Host = best + ".music.126.net"
	slog.Default().Info("CDN 节点不通，已切换到健康节点", "from", host, "to", best)
	return rewritten.String(), true
}

// probeAudioURL 用 Range 拉 1KB 判断某个主机能不能出数据。
func probeAudioURL(ctx context.Context, raw, host string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	u.Host = host + ".music.126.net"
	reqCtx, cancel := context.WithTimeout(ctx, cdnProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, u.String(), nil)
	if err != nil {
		return false
	}
	req.Header.Set("Range", "bytes=0-1023")
	req.Header.Set("Referer", "https://music.163.com/")
	req.Header.Set("User-Agent", audioUA)
	resp, err := cdnProbeClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return false
	}
	n, _ := io.CopyN(io.Discard, resp.Body, cdnProbeBytes)
	return n > 0
}

func cdnKnown(host string) (ok, known bool) {
	cdnMemoMu.Lock()
	defer cdnMemoMu.Unlock()
	e, found := cdnMemo[host]
	if !found || time.Now().After(e.exp) {
		return false, false
	}
	return e.ok, true
}

func cdnRemember(host string, ok bool) {
	cdnMemoMu.Lock()
	defer cdnMemoMu.Unlock()
	if len(cdnMemo) > 64 {
		clear(cdnMemo)
	}
	cdnMemo[host] = cdnMemoEntry{ok: ok, exp: time.Now().Add(cdnProbeMemo)}
}

// DownloadAudio 把解析好的地址整段拉下来（缓存预热用），最多 limit 字节。
//
// truncated=true 表示体积超过 limit，调用方应当放弃缓存这首 —— 多吉云
// 简单上传接口上限 10MB，超了会传失败，白费一次下载。
func DownloadAudio(ctx context.Context, rawURL string, limit int64) (data []byte, truncated bool, err error) {
	reqCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Referer", "https://music.163.com/")
	req.Header.Set("User-Agent", audioUA)
	resp, err := (&http.Client{Timeout: 120 * time.Second}).Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return nil, false, fmt.Errorf("上游返回 HTTP %d", resp.StatusCode)
	}
	buf := make([]byte, 0, 1<<20)
	chunk := make([]byte, 64<<10)
	for {
		n, rerr := resp.Body.Read(chunk)
		if n > 0 {
			buf = append(buf, chunk[:n]...)
			if int64(len(buf)) > limit {
				return nil, true, nil
			}
		}
		if rerr == io.EOF {
			return buf, false, nil
		}
		if rerr != nil {
			return nil, false, rerr
		}
	}
}

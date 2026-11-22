package bilibili

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// QRSession 是 B 站网页扫码登录会话。
type QRSession struct {
	Key string `json:"key"`
	URL string `json:"url"`
}

// QRPollResult 是 B 站二维码轮询状态。
type QRPollResult struct {
	Status  string `json:"status"` // waiting | scanned | confirmed | expired
	Code    int    `json:"code"`
	Message string `json:"message"`
	Cookie  string `json:"-"` // 仅服务端落盘，绝不回传明文给前端
}

// AccountInfo 是当前 B 站 Cookie 的登录态检查结果。
type AccountInfo struct {
	Configured bool   `json:"configured"`
	UseLogin   bool   `json:"useLogin"`
	LoggedIn   bool   `json:"loggedIn"`
	UID        string `json:"uid,omitempty"`
	Username   string `json:"username,omitempty"`
	Face       string `json:"face,omitempty"`
	VIPStatus  int    `json:"vipStatus,omitempty"`
	VIPLabel   string `json:"vipLabel,omitempty"`
	Message    string `json:"message,omitempty"`
}

// CreateLoginQR 申请 B 站扫码登录二维码链接与 qrcode_key。
func (p *Provider) CreateLoginQR(ctx context.Context) (QRSession, error) {
	endpoint := p.passportEndpoint("/x/passport-login/web/qrcode/generate") + "?source=main-fe-header"
	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			URL       string `json:"url"`
			QRCodeKey string `json:"qrcode_key"`
		} `json:"data"`
	}
	if err := p.getJSON(ctx, endpoint, "", &resp); err != nil {
		return QRSession{}, fmt.Errorf("申请 B 站登录二维码失败: %w", err)
	}
	if resp.Code != 0 || strings.TrimSpace(resp.Data.URL) == "" || strings.TrimSpace(resp.Data.QRCodeKey) == "" {
		return QRSession{}, fmt.Errorf("B站二维码生成失败（code=%d %s）", resp.Code, strings.TrimSpace(resp.Message))
	}
	return QRSession{
		Key: strings.TrimSpace(resp.Data.QRCodeKey),
		URL: strings.TrimSpace(resp.Data.URL),
	}, nil
}

// PollLoginQR 轮询 B 站扫码状态；确认成功后提取完整登录 Cookie（含 SESSDATA / bili_jct / DedeUserID / buvid3）。
func (p *Provider) PollLoginQR(ctx context.Context, key string) (QRPollResult, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return QRPollResult{}, errors.New("缺少二维码 qrcode_key")
	}
	endpoint := p.passportEndpoint("/x/passport-login/web/qrcode/poll") + "?qrcode_key=" + url.QueryEscape(key)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return QRPollResult{}, err
	}
	req.Header.Set("User-Agent", biliUA)
	req.Header.Set("Referer", "https://www.bilibili.com/")
	req.Header.Set("Accept", "application/json, text/plain, */*")

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return QRPollResult{}, fmt.Errorf("查询 B 站扫码状态失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return QRPollResult{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return QRPollResult{}, fmt.Errorf("B站扫码轮询 HTTP %d: %s", resp.StatusCode, snippet(body))
	}

	var env struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			URL          string `json:"url"`
			RefreshToken string `json:"refresh_token"`
			Code         int    `json:"code"`
			Message      string `json:"message"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return QRPollResult{}, fmt.Errorf("解析 B 站扫码状态失败: %w", err)
	}
	if env.Code != 0 {
		return QRPollResult{}, fmt.Errorf("B站扫码轮询错误（code=%d %s）", env.Code, strings.TrimSpace(env.Message))
	}

	out := QRPollResult{
		Code:    env.Data.Code,
		Message: strings.TrimSpace(env.Data.Message),
	}
	switch env.Data.Code {
	case 86101:
		out.Status = "waiting"
		if out.Message == "" {
			out.Message = "等待哔哩哔哩 App 扫码"
		}
	case 86090:
		out.Status = "scanned"
		if out.Message == "" {
			out.Message = "已扫码，请在手机端确认登录"
		}
	case 86038:
		out.Status = "expired"
		if out.Message == "" {
			out.Message = "二维码已失效，请刷新重试"
		}
	case 0:
		out.Status = "confirmed"
		cookieMap := make(map[string]string)
		collectCookies(cookieMap, resp.Cookies())
		// 若新版接口把票据放在 data.url 中（crossDomain），访问该链接补收 Set-Cookie。
		if cookieMap["SESSDATA"] == "" && strings.TrimSpace(env.Data.URL) != "" {
			if extra := p.collectCrossDomainCookies(ctx, env.Data.URL); len(extra) > 0 {
				for k, v := range extra {
					if v != "" {
						cookieMap[k] = v
					}
				}
			}
		}
		// 同样解析 data.url 查询参数中可能直接带出的 SESSDATA/bili_jct/DedeUserID（兼容老响应）
		if parsed, perr := url.Parse(strings.TrimSpace(env.Data.URL)); perr == nil {
			q := parsed.Query()
			for _, name := range []string{"SESSDATA", "bili_jct", "DedeUserID", "DedeUserID__ckMd5"} {
				if cookieMap[name] == "" {
					if v := strings.TrimSpace(q.Get(name)); v != "" {
						cookieMap[name] = v
					}
				}
			}
		}
		if cookieMap["buvid3"] == "" {
			b3, b4 := p.ensureBuvid(ctx)
			if b3 != "" {
				cookieMap["buvid3"] = b3
			}
			if b4 != "" && cookieMap["buvid4"] == "" {
				cookieMap["buvid4"] = b4
			}
		}
		if cookieMap["SESSDATA"] == "" {
			return out, errors.New("B站扫码确认成功但未能提取到 SESSDATA Cookie")
		}
		out.Cookie = joinCookieMap(cookieMap)
		if out.Message == "" {
			out.Message = "B站登录成功"
		}
	default:
		out.Status = "expired"
		if out.Message == "" {
			out.Message = fmt.Sprintf("未知扫码状态（code=%d）", env.Data.Code)
		}
	}
	return out, nil
}

func (p *Provider) collectCrossDomainCookies(ctx context.Context, target string) map[string]string {
	out := make(map[string]string)
	u, err := url.Parse(strings.TrimSpace(target))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return out
	}
	jar, _ := cookiejar.New(nil)
	hc := &http.Client{Timeout: 8 * time.Second, Jar: jar}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return out
	}
	req.Header.Set("User-Agent", biliUA)
	req.Header.Set("Referer", "https://www.bilibili.com/")
	resp, err := hc.Do(req)
	if err != nil {
		return out
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 256<<10))
	collectCookies(out, resp.Cookies())
	if jar != nil {
		collectCookies(out, jar.Cookies(u))
		if biliURL, err := url.Parse("https://www.bilibili.com/"); err == nil {
			collectCookies(out, jar.Cookies(biliURL))
		}
	}
	return out
}

// AccountStatus 调用 /x/web-interface/nav 校验当前显式配置的 B 站 Cookie 登录态。
func (p *Provider) AccountStatus(ctx context.Context) (AccountInfo, error) {
	cookie := p.Cookie()
	useLogin := p.UseLogin()
	if cookie == "" {
		msg := "尚未配置 B 站 Cookie"
		if useLogin {
			msg = "尚未配置 B 站 Cookie（当前默认要求登录态）"
		}
		return AccountInfo{
			Configured: false,
			UseLogin:   useLogin,
			LoggedIn:   false,
			Message:    msg,
		}, nil
	}

	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			IsLogin   bool   `json:"isLogin"`
			Mid       int64  `json:"mid"`
			Uname     string `json:"uname"`
			Face      string `json:"face"`
			VipStatus int    `json:"vipStatus"`
			VipLabel  struct {
				Text string `json:"text"`
			} `json:"vip_label"`
		} `json:"data"`
	}
	if err := p.getJSON(ctx, p.apiEndpoint("/x/web-interface/nav"), cookie, &resp); err != nil {
		return AccountInfo{
			Configured: true,
			UseLogin:   useLogin,
			LoggedIn:   false,
			Message:    "校验 B 站登录态失败：" + err.Error(),
		}, err
	}
	if !resp.Data.IsLogin || resp.Data.Mid <= 0 {
		return AccountInfo{
			Configured: true,
			UseLogin:   useLogin,
			LoggedIn:   false,
			Message:    "B站 Cookie 已失效或未登录",
		}, nil
	}
	return AccountInfo{
		Configured: true,
		UseLogin:   useLogin,
		LoggedIn:   true,
		UID:        strconv.FormatInt(resp.Data.Mid, 10),
		Username:   strings.TrimSpace(resp.Data.Uname),
		Face:       forceHTTPS(resp.Data.Face),
		VIPStatus:  resp.Data.VipStatus,
		VIPLabel:   strings.TrimSpace(resp.Data.VipLabel.Text),
		Message:    "B站登录态有效",
	}, nil
}

func collectCookies(dst map[string]string, cookies []*http.Cookie) {
	for _, c := range cookies {
		if c == nil || c.MaxAge < 0 {
			continue
		}
		k := strings.TrimSpace(c.Name)
		v := strings.TrimSpace(c.Value)
		if k == "" || v == "" {
			continue
		}
		dst[k] = v
	}
}

func joinCookieMap(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k, v := range m {
		if strings.TrimSpace(k) != "" && strings.TrimSpace(v) != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+m[k])
	}
	return strings.Join(parts, "; ")
}

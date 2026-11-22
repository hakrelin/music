package netease

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// QRSession 是网易云二维码登录会话。
type QRSession struct {
	Key string `json:"key"`
	URL string `json:"url"`
}

// QRPollResult 表示网易云二维码轮询结果。
type QRPollResult struct {
	Status   string `json:"status"` // waiting | scanned | confirmed | expired
	Code     int    `json:"code"`
	Message  string `json:"message"`
	Nickname string `json:"nickname,omitempty"`
	Avatar   string `json:"avatar,omitempty"`
	Cookie   string `json:"-"` // 仅后端保存，绝不直接下发给前端
}

// AccountInfo 是当前网易云 Cookie 的账号校验结果。
type AccountInfo struct {
	Configured bool   `json:"configured"`
	LoggedIn   bool   `json:"loggedIn"`
	UID        string `json:"uid,omitempty"`
	Nickname   string `json:"nickname,omitempty"`
	Avatar     string `json:"avatar,omitempty"`
	VIPType    int    `json:"vipType,omitempty"`
	Message    string `json:"message,omitempty"`
}

// CreateLoginQR 申请网易云扫码登录 unikey 与扫码链接。
func (p *Provider) CreateLoginQR(ctx context.Context) (QRSession, error) {
	raw, _, err := p.weapiRaw(ctx, "login/qrcode/unikey", map[string]any{"type": 1}, false)
	if err != nil {
		return QRSession{}, fmt.Errorf("申请网易云登录二维码失败: %w", err)
	}
	var resp struct {
		Code    int    `json:"code"`
		Unikey  string `json:"unikey"`
		Message string `json:"message"`
		Msg     string `json:"msg"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return QRSession{}, fmt.Errorf("解析网易云二维码响应失败: %w", err)
	}
	if resp.Code != 200 || strings.TrimSpace(resp.Unikey) == "" {
		msg := strings.TrimSpace(resp.Message)
		if msg == "" {
			msg = strings.TrimSpace(resp.Msg)
		}
		return QRSession{}, fmt.Errorf("网易云二维码生成失败（code=%d %s）", resp.Code, msg)
	}
	key := strings.TrimSpace(resp.Unikey)
	return QRSession{
		Key: key,
		URL: "https://music.163.com/login?codekey=" + url.QueryEscape(key),
	}, nil
}

// PollLoginQR 轮询网易云扫码状态；code=803 时从响应 Set-Cookie 提取完整登录 Cookie。
func (p *Provider) PollLoginQR(ctx context.Context, key string) (QRPollResult, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return QRPollResult{}, errors.New("缺少二维码 key")
	}
	raw, cookies, err := p.weapiRaw(ctx, "login/qrcode/client/login", map[string]any{
		"key":  key,
		"type": 1,
	}, false)
	if err != nil {
		return QRPollResult{}, fmt.Errorf("查询网易云扫码状态失败: %w", err)
	}
	var resp struct {
		Code      int    `json:"code"`
		Message   string `json:"message"`
		Nickname  string `json:"nickname"`
		AvatarURL string `json:"avatarUrl"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return QRPollResult{}, fmt.Errorf("解析网易云扫码状态失败: %w", err)
	}

	out := QRPollResult{
		Code:     resp.Code,
		Message:  strings.TrimSpace(resp.Message),
		Nickname: strings.TrimSpace(resp.Nickname),
		Avatar:   forceHTTPS(resp.AvatarURL),
	}
	switch resp.Code {
	case 800:
		out.Status = "expired"
		if out.Message == "" {
			out.Message = "二维码已过期，请刷新重试"
		}
	case 801:
		out.Status = "waiting"
		if out.Message == "" {
			out.Message = "等待网易云 App 扫码"
		}
	case 802:
		out.Status = "scanned"
		if out.Message == "" {
			out.Message = "已扫码，请在手机端确认登录"
		}
	case 803:
		out.Status = "confirmed"
		out.Cookie = formatCookies(cookies)
		if out.Cookie == "" {
			return out, errors.New("网易云返回登录成功但未下发 Cookie")
		}
		if out.Message == "" {
			out.Message = "网易云登录成功"
		}
	default:
		out.Status = "expired"
		if out.Message == "" {
			out.Message = fmt.Sprintf("未知扫码状态（code=%d）", resp.Code)
		}
	}
	return out, nil
}

// AccountStatus 检查当前已配置的网易云 Cookie 是否有效及登录账号信息。
func (p *Provider) AccountStatus(ctx context.Context) (AccountInfo, error) {
	cookie := p.Cookie()
	if cookie == "" {
		return AccountInfo{Configured: false, LoggedIn: false, Message: "尚未配置网易云 Cookie"}, nil
	}
	var resp struct {
		Code    int `json:"code"`
		Account *struct {
			ID      int64 `json:"id"`
			VIPType int   `json:"vipType"`
		} `json:"account"`
		Profile *struct {
			UserID    int64  `json:"userId"`
			Nickname  string `json:"nickname"`
			AvatarURL string `json:"avatarUrl"`
			VIPType   int    `json:"vipType"`
		} `json:"profile"`
	}
	if err := p.weapi(ctx, "w/nuser/account/get", map[string]any{}, &resp); err != nil {
		return AccountInfo{Configured: true, LoggedIn: false, Message: "校验登录态失败：" + err.Error()}, err
	}
	if resp.Profile == nil || resp.Profile.UserID <= 0 {
		return AccountInfo{Configured: true, LoggedIn: false, Message: "Cookie 已失效或为匿名态"}, nil
	}
	vip := resp.Profile.VIPType
	if vip == 0 && resp.Account != nil {
		vip = resp.Account.VIPType
	}
	return AccountInfo{
		Configured: true,
		LoggedIn:   true,
		UID:        strconv.FormatInt(resp.Profile.UserID, 10),
		Nickname:   strings.TrimSpace(resp.Profile.Nickname),
		Avatar:     forceHTTPS(resp.Profile.AvatarURL),
		VIPType:    vip,
		Message:    "登录态有效",
	}, nil
}

func formatCookies(cookies []*http.Cookie) string {
	vals := make(map[string]string, len(cookies))
	for _, c := range cookies {
		if c == nil {
			continue
		}
		name := strings.TrimSpace(c.Name)
		val := strings.TrimSpace(c.Value)
		if name == "" || val == "" || c.MaxAge < 0 {
			continue
		}
		vals[name] = val
	}
	if len(vals) == 0 {
		return ""
	}
	names := make([]string, 0, len(vals))
	for k := range vals {
		names = append(names, k)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, k := range names {
		parts = append(parts, k+"="+vals[k])
	}
	return strings.Join(parts, "; ")
}

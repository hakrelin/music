// Package storage 用多吉云对象存储做音频缓存。
//
// 为什么要这一层：浏览器直连网易云 CDN 时，一部分节点会按来源/Referer 直接
// 返回 403（同一首、同一份签名，别人能放、你刷新后就放不了）。把听过的曲目
// 落到对象存储后，浏览器只跟 COS 说话，节点风控、签名过期都不再影响播放，
// 而且不吃服务器的 3Mbps 出网带宽（下载流量走对象存储）。
//
// 上传/删除走多吉云服务端 API（HMAC-SHA1 AccessToken，接口见
// https://docs.dogecloud.com/oss/api-access-token）；播放返回 COS 预签名
// 地址（临时密钥来自 /auth/tmp_token.json），浏览器 302 过去直连 COS。
package storage

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// apiBase 是多吉云服务端 API 入口。
const apiBase = "https://api.dogecloud.com"

// Account 是一个多吉云账号 + 目标存储空间（桶）。
type Account struct {
	Label  string `json:"label"`
	AK     string `json:"accessKey"`
	SK     string `json:"secretKey"`
	Bucket string `json:"bucket"`
	// Base 是桶绑定的加速域名（基础型必须走它）；为空时用 COS 预签名。
	Base string `json:"publicBase"`
}

// client 是多吉云服务端 API 的最小客户端。
type client struct {
	ak, sk string
	hc     *http.Client
	base   string
}

func newClient(acc Account, hc *http.Client) *client {
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	return &client{ak: acc.AK, sk: acc.SK, hc: hc, base: apiBase}
}

// Authorization = "TOKEN " + AK + ":" + hex(HMAC-SHA1(AK 的 SecretKey, path + "\n" + body))
func (c *client) authorization(path, body string) string {
	mac := hmac.New(sha1.New, []byte(c.sk))
	mac.Write([]byte(path + "\n" + body))
	return "TOKEN " + c.ak + ":" + hex.EncodeToString(mac.Sum(nil))
}

// apiResp 是服务端 API 的统一响应信封。
type apiResp struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

// call 发一次 API 请求。path 可以带 query，签名用的就是这个完整 path+query。
func (c *client) call(ctx context.Context, method, path string, body []byte, ctype string) (json.RawMessage, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", c.authorization(path, string(body)))
	if body != nil {
		if ctype == "" {
			ctype = "application/json"
		}
		req.Header.Set("Content-Type", ctype)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	var env apiResp
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("多吉云返回非 JSON（HTTP %d）: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	if env.Code != 200 {
		return nil, fmt.Errorf("多吉云错误 %d: %s", env.Code, env.Msg)
	}
	return env.Data, nil
}

// Upload 调「简单上传」把 ≤10MB 的文件写进桶里。
// 这个接口要求把 body 一起参与签名（PUT 方式可以不签 body，但没必要）。
func (c *client) Upload(ctx context.Context, bucket, key string, data []byte, ctype string) error {
	q := url.Values{}
	q.Set("bucket", bucket)
	q.Set("key", key)
	path := "/oss/upload/put.json?" + q.Encode()
	_, err := c.call(ctx, http.MethodPost, path, data, ctype)
	return err
}

// DeleteKeys 批量删除桶内文件。注意 bucket 必须是「空间名称」，body 是 JSON 数组。
func (c *client) DeleteKeys(ctx context.Context, bucket string, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	q := url.Values{}
	q.Set("bucket", bucket)
	path := "/oss/file/delete.json?" + q.Encode()
	body, err := json.Marshal(keys)
	if err != nil {
		return err
	}
	_, err = c.call(ctx, http.MethodPost, path, body, "application/json")
	return err
}

// FileInfo 是文件列表里的一条。
type FileInfo struct {
	Key      string `json:"key"`
	Size     int64  `json:"fsize"`
	Time     string `json:"time"`
	MimeType string `json:"mimeType"`
}

// ListFiles 列出以 prefix 开头的文件（一次最多 1000 条）。
func (c *client) ListFiles(ctx context.Context, bucket, prefix string, limit int) ([]FileInfo, error) {
	q := url.Values{}
	q.Set("bucket", bucket)
	if prefix != "" {
		q.Set("prefix", prefix)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	path := "/oss/file/list.json?" + q.Encode()
	data, err := c.call(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		return nil, err
	}
	var out struct {
		Files []FileInfo `json:"files"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out.Files, nil
}

// Credentials 是 S3 临时密钥（三段式）。
type Credentials struct {
	AccessKeyID     string `json:"accessKeyId"`
	SecretAccessKey string `json:"secretAccessKey"`
	SessionToken    string `json:"sessionToken"`
}

type tmpTokenResp struct {
	Credentials Credentials `json:"Credentials"`
	ExpiredAt   int64       `json:"ExpiredAt"`
	Buckets     []struct {
		Name           string `json:"name"`
		S3Bucket       string `json:"s3Bucket"`
		S3Endpoint     string `json:"s3Endpoint"`
		S3EndpointHost string `json:"s3EndpointHost"`
	} `json:"Buckets"`
}

// tempCredentials 取一次 OSS_FULL 临时密钥（最长 2 小时）。
func (c *client) tempCredentials(ctx context.Context) (*tmpTokenResp, error) {
	body, _ := json.Marshal(map[string]any{
		"channel": "OSS_FULL",
		"scopes":  []string{"*"},
		"ttl":     7200,
	})
	data, err := c.call(ctx, http.MethodPost, "/auth/tmp_token.json", body, "application/json")
	if err != nil {
		return nil, err
	}
	var out tmpTokenResp
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// presignGet 生成 COS 预签名 GET 地址（五分钟签名算法的 URL 形式）。
//
// 桶是私有的，裸地址会 403；带上 STS 三段式密钥的签名后浏览器可以直连，
// 而且不需要 CORS（媒体元素允许跨域播放）。
func presignGet(endpointHost, key string, cred Credentials, ttl time.Duration) (string, error) {
	host := strings.TrimPrefix(strings.TrimPrefix(endpointHost, "https://"), "http://")
	host = strings.TrimSuffix(host, "/")
	if host == "" {
		return "", fmt.Errorf("缺少 COS endpoint")
	}
	objPath := "/" + strings.TrimPrefix(key, "/")
	start := time.Now().Add(-60 * time.Second).Unix()
	end := time.Now().Add(ttl).Unix()
	signTime := strconv.FormatInt(start, 10) + ";" + strconv.FormatInt(end, 10)

	urlParam := "x-cos-security-token=" + url.QueryEscape(cred.SessionToken)
	header := "host=" + url.QueryEscape(host)
	httpString := "get\n" + objPath + "\n" + urlParam + "\n" + header + "\n"
	sum := sha1.Sum([]byte(httpString))
	stringToSign := "sha1\n" + signTime + "\n" + hex.EncodeToString(sum[:]) + "\n"

	mac := hmac.New(sha1.New, []byte(cred.SecretAccessKey))
	mac.Write([]byte(signTime))
	signKey := hex.EncodeToString(mac.Sum(nil))
	mac2 := hmac.New(sha1.New, []byte(signKey))
	mac2.Write([]byte(stringToSign))
	signature := hex.EncodeToString(mac2.Sum(nil))

	q := url.Values{}
	q.Set("q-sign-algorithm", "sha1")
	q.Set("q-ak", cred.AccessKeyID)
	q.Set("q-sign-time", signTime)
	q.Set("q-key-time", signTime)
	q.Set("q-header-list", "host")
	q.Set("q-url-param-list", "x-cos-security-token")
	q.Set("q-signature", signature)
	q.Set("x-cos-security-token", cred.SessionToken)
	return "https://" + host + objPath + "?" + q.Encode(), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ---- 媒体类型推断 ----

// contentTypeFor 按 key 的扩展名给 Content-Type。mp3 用 audio/mpeg，
// 浏览器 <audio> 拿到 octet-stream 也能放，但标对了更利于缓存与拖动。
func contentTypeFor(key string) string {
	switch {
	case strings.HasSuffix(key, ".mp3"):
		return "audio/mpeg"
	case strings.HasSuffix(key, ".m4a"), strings.HasSuffix(key, ".mp4"):
		return "audio/mp4"
	case strings.HasSuffix(key, ".flac"):
		return "audio/flac"
	case strings.HasSuffix(key, ".ogg"):
		return "audio/ogg"
	default:
		return "application/octet-stream"
	}
}

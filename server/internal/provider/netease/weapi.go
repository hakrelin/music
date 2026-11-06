package netease

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// 本文件是 weapi 传输层：加密协议 + 带重试的 HTTP 调用。
//
// 网易云网页端的 weapi 协议（网页端 JS 里那一套）：
//  1. 把 JSON 明文用固定 key 做一次 AES-128-CBC（PKCS7）得到 base64 串；
//  2. 再把这个 base64 串用 16 位随机 secret 做一次 AES-128-CBC，得到最终 params；
//  3. secret 反转后当作大端整数，用固定 RSA 公钥做「原始 RSA」加密（无填充），
//     结果补零成 128 字节，转十六进制得到 encSecKey。
//
// 注意第 3 步：网上有些 Go 实现用 rsa.EncryptPKCS1v15 并拼 secret+reverse(secret)，
// 实测服务端会返回 HTTP 200 + 空响应体（静默拒绝）。必须用无填充的原始 RSA。
const (
	// weapiBaseURL 是 weapi 入口，Provider.baseURL 可被测试覆盖。
	weapiBaseURL = "https://music.163.com/weapi/"

	// weapiPresetKey / weapiIV 是网页端写死的第一次 AES 参数。
	weapiPresetKey = "0CoJUm6Qyw8W8jud"
	weapiIV        = "0102030405060708"

	// secret 的字母表与长度与网页端一致（大小写字母 + 数字，16 位）。
	secretAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	secretLen      = 16

	// weapiRSAExponent / weapiRSAModulus 是网页端固定 RSA 公钥：e=65537，1024 位模数。
	weapiRSAExponent = 65537
	weapiRSAModulus  = "00e0b509f6259df8642dbc35662901477df22677ec152b5ff68ace615bb7b725152b3ab17a876aea8a5aa76d2e417629ec4ee341f56135fccf695280104e0312ecbda92557c93870114af6c9d05c4f7f0c3685b7a46bee255932575cce10b424d813cfe4875d3e82047b97ddef52741d546b8e289dc6935b3ece0462db0a22b8e7"

	// rsaBlockBytes 是原始 RSA 输出块大小（128 字节 → 256 个十六进制字符）。
	rsaBlockBytes = 128

	// maxWeapiBodyBytes 限制响应体大小：正常响应几十 KB，8 MB 足够，防止被大响应拖死。
	maxWeapiBodyBytes = 8 << 20

	// 重试策略：最多重试 2 次，指数退避（400ms、800ms）。
	weapiMaxRetries = 2
	weapiRetryBase  = 400 * time.Millisecond

	// weapiUserAgent 用桌面 Chrome 的 UA；部分接口会看 UA，缺省容易被风控。
	weapiUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

// retryableError 包装「网络故障 / 5xx / 响应不是 JSON」等临时错误，weapi 会据此重试。
// 业务错误（code != 200）不重试。
type retryableError struct{ err error }

func (e *retryableError) Error() string { return e.err.Error() }
func (e *retryableError) Unwrap() error { return e.err }

// weapiError 是 weapi 的业务错误（HTTP 200 但 code != 200）。
// 单独成类型是为了让调用方能区分「网络挂了」和「接口拒绝」（例如 Search 的匿名降级）。
type weapiError struct {
	Endpoint string
	Code     int
	Message  string
}

func (e *weapiError) Error() string {
	if strings.TrimSpace(e.Message) == "" {
		return fmt.Sprintf("weapi %s 返回 code=%d", e.Endpoint, e.Code)
	}
	return fmt.Sprintf("weapi %s 返回 code=%d: %s", e.Endpoint, e.Code, e.Message)
}

// randomSecret 生成 16 位随机 secret（字母表与网页端一致）。
func randomSecret() ([]byte, error) {
	buf := make([]byte, secretLen)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("生成随机 secret 失败: %w", err)
	}
	for i, b := range buf {
		buf[i] = secretAlphabet[int(b)%len(secretAlphabet)]
	}
	return buf, nil
}

// pkcs7Pad 做 PKCS7 填充；AES 是块加密，明文必须补齐到块大小整数倍。
func pkcs7Pad(plain []byte, blockSize int) []byte {
	padding := blockSize - len(plain)%blockSize
	out := make([]byte, len(plain)+padding)
	copy(out, plain)
	for i := len(plain); i < len(out); i++ {
		out[i] = byte(padding)
	}
	return out
}

// pkcs7Unpad 去掉 PKCS7 填充（单测解密用）。
func pkcs7Unpad(padded []byte, blockSize int) ([]byte, error) {
	if len(padded) == 0 || len(padded)%blockSize != 0 {
		return nil, errors.New("填充长度非法")
	}
	padding := int(padded[len(padded)-1])
	if padding == 0 || padding > blockSize || padding > len(padded) {
		return nil, errors.New("填充值非法")
	}
	for _, b := range padded[len(padded)-padding:] {
		if int(b) != padding {
			return nil, errors.New("填充内容非法")
		}
	}
	return padded[:len(padded)-padding], nil
}

// aesEncryptBase64 用 AES-128-CBC + PKCS7 加密，返回 base64 字符串的字节。
// 网易云的 AES 用固定 IV（不是随机 IV），且第二次加密的输入是第一次的 base64 文本。
func aesEncryptBase64(plain, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("AES key 非法: %w", err)
	}
	padded := pkcs7Pad(plain, aes.BlockSize)
	cipherText := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, []byte(weapiIV)).CryptBlocks(cipherText, padded)
	out := make([]byte, base64.StdEncoding.EncodedLen(len(cipherText)))
	base64.StdEncoding.Encode(out, cipherText)
	return out, nil
}

// aesDecryptBase64 是 aesEncryptBase64 的逆操作，仅用于单测验证加密自洽。
func aesDecryptBase64(b64, key []byte) ([]byte, error) {
	raw := make([]byte, base64.StdEncoding.DecodedLen(len(b64)))
	n, err := base64.StdEncoding.Decode(raw, b64)
	if err != nil {
		return nil, fmt.Errorf("base64 解码失败: %w", err)
	}
	raw = raw[:n]
	if len(raw) == 0 || len(raw)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("密文长度 %d 不是 AES 块大小整数倍", len(raw))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("AES key 非法: %w", err)
	}
	plain := make([]byte, len(raw))
	cipher.NewCBCDecrypter(block, []byte(weapiIV)).CryptBlocks(plain, raw)
	return pkcs7Unpad(plain, aes.BlockSize)
}

// reverseString 反转字符串字节（secret 只含 ASCII，字节反转即可）。
func reverseString(s string) string {
	b := []byte(s)
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return string(b)
}

// rsaEncryptSecret 用固定公钥对 secret 做原始 RSA（无填充）加密。
// 网易云把 secret 反转后当成大端整数直接做模幂运算，结果补零到 128 字节。
func rsaEncryptSecret(secret []byte) (string, error) {
	modulus, ok := new(big.Int).SetString(weapiRSAModulus, 16)
	if !ok {
		return "", errors.New("网易云 RSA 模数常量非法")
	}
	m := new(big.Int).SetBytes([]byte(reverseString(string(secret))))
	if m.Sign() <= 0 {
		return "", errors.New("secret 不能为空")
	}
	c := new(big.Int).Exp(m, big.NewInt(weapiRSAExponent), modulus)
	hexStr := c.Text(16)
	if len(hexStr) < rsaBlockBytes*2 {
		hexStr = strings.Repeat("0", rsaBlockBytes*2-len(hexStr)) + hexStr
	}
	return hexStr, nil
}

// encryptParams 用给定的 secret 生成 params 与 encSecKey。
// 单独拆出来是为了让单测能用固定 secret 做可复现的断言。
func encryptParams(payload map[string]any, secret []byte) (params, encSecKey string, err error) {
	plain, err := json.Marshal(payload)
	if err != nil {
		return "", "", fmt.Errorf("序列化请求参数失败: %w", err)
	}
	once, err := aesEncryptBase64(plain, []byte(weapiPresetKey))
	if err != nil {
		return "", "", err
	}
	twice, err := aesEncryptBase64(once, secret)
	if err != nil {
		return "", "", err
	}
	encSecKey, err = rsaEncryptSecret(secret)
	if err != nil {
		return "", "", err
	}
	return string(twice), encSecKey, nil
}

// weapiEncrypt 是完整加密流程：随机 secret → 两次 AES → RSA。
func weapiEncrypt(payload map[string]any) (params, encSecKey string, secret []byte, err error) {
	secret, err = randomSecret()
	if err != nil {
		return "", "", nil, err
	}
	params, encSecKey, err = encryptParams(payload, secret)
	return params, encSecKey, secret, err
}

// endpointURL 拼出完整 URL；baseURL 留空时用默认值（方便单测替换）。
func (p *Provider) endpointURL(endpoint string) string {
	base := p.baseURL
	if base == "" {
		base = weapiBaseURL
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(endpoint, "/")
}

// weapi 发送一次 weapi POST，并把 JSON 响应解码进 out。
//
// 重试：只有网络错误、5xx/429、响应不是 JSON/空响应才重试（最多 2 次，400ms/800ms 退避）；
// 业务错误（code != 200）不重试，直接返回 *weapiError。
func (p *Provider) weapi(ctx context.Context, endpoint string, payload map[string]any, out any) error {
	// 每次都带 csrf_token：登录态下部分接口要求它与 cookie 里的 __csrf 一致。
	body := make(map[string]any, len(payload)+1)
	for k, v := range payload {
		body[k] = v
	}
	if _, ok := body["csrf_token"]; !ok {
		body["csrf_token"] = p.csrf
	}

	var lastErr error
	for attempt := 0; attempt <= weapiMaxRetries; attempt++ {
		if attempt > 0 {
			delay := weapiRetryBase
			for i := 1; i < attempt; i++ {
				delay *= 2
			}
			select {
			case <-ctx.Done():
				return fmt.Errorf("weapi %s 等待重试时被取消: %w", endpoint, ctx.Err())
			case <-time.After(delay):
			}
		}
		err := p.weapiOnce(ctx, endpoint, body, out)
		if err == nil {
			return nil
		}
		lastErr = err
		var retryable *retryableError
		if !errors.As(err, &retryable) {
			return err
		}
	}
	return fmt.Errorf("weapi %s 重试 %d 次后仍失败: %w", endpoint, weapiMaxRetries, lastErr)
}

// weapiOnce 是单次请求（不含重试逻辑）。
func (p *Provider) weapiOnce(ctx context.Context, endpoint string, payload map[string]any, out any) error {
	params, encSecKey, _, err := weapiEncrypt(payload)
	if err != nil {
		return fmt.Errorf("weapi %s 加密失败: %w", endpoint, err)
	}

	form := url.Values{}
	form.Set("params", params)
	form.Set("encSecKey", encSecKey)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpointURL(endpoint), strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("weapi %s 构造请求失败: %w", endpoint, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", "https://music.163.com/")
	req.Header.Set("Origin", "https://music.163.com")
	req.Header.Set("User-Agent", weapiUserAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	if cookie := p.cfg.Cookie; cookie != "" {
		req.Header.Set("Cookie", cookie)
	}

	client := p.client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("weapi %s 请求被取消: %w", endpoint, ctx.Err())
		}
		return &retryableError{err: fmt.Errorf("weapi %s 请求失败: %w", endpoint, err)}
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxWeapiBodyBytes))
	if err != nil {
		return &retryableError{err: fmt.Errorf("weapi %s 读取响应失败: %w", endpoint, err)}
	}
	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("weapi %s HTTP %d: %s", endpoint, resp.StatusCode, snippet(raw))
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			return &retryableError{err: err}
		}
		return err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		// 网易云对「参数/加密不对」的请求会回 200 + 空 body，这属于可重试的坏响应。
		return &retryableError{err: fmt.Errorf("weapi %s 返回空响应（加密参数可能被拒或触发风控）", endpoint)}
	}

	var head struct {
		Code    int    `json:"code"`
		Msg     string `json:"msg"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return &retryableError{err: fmt.Errorf("weapi %s 响应不是 JSON（可能触发风控）: %s", endpoint, snippet(raw))}
	}
	if head.Code != 200 {
		msg := head.Msg
		if strings.TrimSpace(msg) == "" {
			msg = head.Message
		}
		return &weapiError{Endpoint: endpoint, Code: head.Code, Message: msg}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("weapi %s 响应解析失败: %w", endpoint, err)
	}
	return nil
}

// snippet 截断响应体用于错误信息，避免把整页 HTML 塞进错误。
func snippet(b []byte) string {
	s := strings.Join(strings.Fields(string(b)), " ")
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

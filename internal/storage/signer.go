// Package storage 提供 R2（S3 兼容）素材分发的预签名 URL（ARCHITECTURE §2.7）。
//
// 只做 SigV4 查询串签名（GET 预签名，决策下发用）——纯内存 HMAC 计算，
// 微秒级、无网络 IO，不触碰决策路径延迟预算。
// 写侧（presigned PUT）按架构分工在 Next.js BFF 用 @aws-sdk 生成，
// 写凭证不进 Go 进程。
package storage

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Signer R2 预签名器（只读凭证）。零值不可用，用 New 构造。
type Signer struct {
	accountID string // R2 账户 ID（端点子域）
	bucket    string
	accessKey string
	secretKey string
	region    string // R2 固定 "auto"

	// CDN 前置（可选）：R2 自定义域 / Cloudflare Worker 代理域名。
	// 非空时 PresignGET 签发 CDN 域名 URL；PresignGETDirect 始终直连 R2 兜底。
	cdnBase         string
	cdnBucketInPath bool // false 时路径去掉桶前缀（R2 自定义域把桶绑到域名）
	expiry          time.Duration // 预签名默认有效期（默认 24h，可由 R2_SIGN_TTL 覆盖）

	mu    sync.Mutex
	cache map[string]urlCacheEntry // 进程内签名缓存：同一 (host,key,TTL) 窗口内只签一次
}

// New 创建签名器。
func New(accountID, bucket, accessKey, secretKey string) *Signer {
	return &Signer{
		accountID: accountID, bucket: bucket,
		accessKey: accessKey, secretKey: secretKey,
		region:           "auto",
		cdnBucketInPath: true,
		expiry:          24 * time.Hour,
		cache:           make(map[string]urlCacheEntry),
	}
}

// Endpoint R2 S3 兼容端点（P0 直连；P1 换自定义域 CDN）。
func (s *Signer) Endpoint() string {
	return "https://" + s.r2Host()
}

func (s *Signer) r2Host() string {
	return s.accountID + ".r2.cloudflarestorage.com"
}

// SetCDN 配置 CDN 前置域名（R2 自定义域 / Cloudflare Worker 代理）。
// base 为空则保持直连 R2（默认）。bucketInPath=false 时路径去掉桶前缀
// （R2 自定义域把桶绑定到域名，路径仅为 /objectKey）；Worker 代理若仍用
// /bucket/objectKey 转发则保持 true。
func (s *Signer) SetCDN(base string, bucketInPath bool) {
	s.cdnBase = base
	s.cdnBucketInPath = bucketInPath
}

// SetSignTTL 覆盖预签名默认有效期（默认 24h）。<=0 忽略。
func (s *Signer) SetSignTTL(d time.Duration) {
	if d > 0 {
		s.expiry = d
	}
}

// DefaultExpiry 返回当前预签名默认有效期。
func (s *Signer) DefaultExpiry() time.Duration {
	return s.expiry
}

// PresignGET 生成对象下载 URL。
// 已配置 CDN 前置（SetCDN，公开自定义域）→ 返回纯净无签名 URL（Cloudflare 边缘回源读私有 R2）。
// 未配置 CDN（冷启动/直连 R2 私有桶）→ 返回 SigV4 预签名 URL（桶私有必须签名才能取）。
func (s *Signer) PresignGET(objectKey string, expires time.Duration) string {
	if s.cdnBase != "" {
		// 公开自定义域（绑定私有 R2 的 CDN 前门）：直接返回纯净 URL，无需签名。
		key := objectKey
		if s.cdnBucketInPath {
			key = s.bucket + "/" + objectKey
		}
		return strings.TrimSuffix(s.cdnBase, "/") + "/" + strings.TrimPrefix(key, "/")
	}
	// 无 CDN（冷启动/直连 R2 私有桶）：用 SigV4 预签名 GET。
	return s.cachedPresign(s.r2Host(), s.bucket+"/"+objectKey, expires)
}

// cachedPresign 带进程内缓存的签名：同一 (host,key,TTL) 在窗口内只签一次，
// 降低高 QPS 决策路径的重复 HMAC（虽微秒级，热门素材可复用）。
func (s *Signer) cachedPresign(host, objectKey string, expires time.Duration) string {
	now := time.Now()
	amz := now.UTC().Format("20060102T150405Z")
	if s.cache != nil {
		ck := host + "|" + objectKey + "|" + strconv.Itoa(int(expires.Seconds()))
		s.mu.Lock()
		if e, ok := s.cache[ck]; ok && now.Before(e.expire) {
			url := e.url
			s.mu.Unlock()
			return url
		}
		s.mu.Unlock()
	}
	url := s.presignAt(host, objectKey, expires, amz)
	if s.cache != nil {
		cacheFor := expires
		if cacheFor > 60*time.Second {
			cacheFor -= 60 * time.Second // 预留余量，避免下发即将过期的 URL
		}
		ck := host + "|" + objectKey + "|" + strconv.Itoa(int(expires.Seconds()))
		s.mu.Lock()
		s.cache[ck] = urlCacheEntry{url: url, expire: now.Add(cacheFor)}
		s.mu.Unlock()
	}
	return url
}

type urlCacheEntry struct {
	url    string
	expire time.Time
}

// presignAt SigV4 查询串签名核心（host/时间可注入，供测试向量对拍）。
func (s *Signer) presignAt(host, objectKey string, expires time.Duration, amzDate string) string {
	date := amzDate[:8]
	scope := fmt.Sprintf("%s/%s/s3/aws4_request", date, s.region)
	cred := fmt.Sprintf("%s/%s", s.accessKey, scope)

	q := url.Values{}
	q.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	q.Set("X-Amz-Credential", cred)
	q.Set("X-Amz-Date", amzDate)
	q.Set("X-Amz-Expires", fmt.Sprintf("%d", int(expires.Seconds())))
	q.Set("X-Amz-SignedHeaders", "host")

	// 规范查询串：键排序后按 AWS 规则编码（值中的 / → %2F）
	canonicalQuery := canonicalQueryEncode(q)

	canonicalRequest := strings.Join([]string{
		"GET",
		canonicalURI(objectKey),
		canonicalQuery,
		"host:" + host,
		"", // CanonicalHeaders 后的空行（join 分隔符提供）
		"host",
		"UNSIGNED-PAYLOAD",
	}, "\n")

	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	sig := hex.EncodeToString(hmacSHA256(signingKey(s.secretKey, date, s.region, "s3"), []byte(stringToSign)))

	return fmt.Sprintf("https://%s%s?%s&X-Amz-Signature=%s",
		host, canonicalURI(objectKey), canonicalQuery, sig)
}

// canonicalURI S3 规范路径：逐段 RFC3986 编码（空格= %20，非 +），/ 保留。
func canonicalURI(key string) string {
	segs := strings.Split(strings.Trim(key, "/"), "/")
	for i, seg := range segs {
		segs[i] = awsEncode(seg)
	}
	return "/" + strings.Join(segs, "/")
}

// canonicalQueryEncode AWS 规范查询串：键排序，键值均严格编码（含 /）。
func canonicalQueryEncode(q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		for _, v := range q[k] {
			parts = append(parts, awsEncode(k)+"="+awsEncode(v))
		}
	}
	return strings.Join(parts, "&")
}

// awsEncode RFC 3986 未保留字符集之外全部百分号编码（含 / 与 =）。
func awsEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '.' || c == '_' || c == '~' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func signingKey(secret, date, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(date))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	return hmacSHA256(kService, []byte("aws4_request"))
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

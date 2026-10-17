package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"adcenter/internal/config"
	"adcenter/internal/engine"
)

// adRequest 下发决策请求体（resolveDecision 使用；旧 /v1/ad/req 已废弃）。
type adRequest struct {
	Style    string `json:"style"`    // 展现样式：splash / rewarded_video / interstitial / feed / banner
	DeviceID string `json:"deviceId"` // 设备 ID（频控主体）
	Country  string `json:"country,omitempty"`
	Language string `json:"language,omitempty"`
	Count    int    `json:"count,omitempty"` // 批量条数（默认 1，上限 20）
}

// resolveDecision 决策 + 结果缓存（Redis）。
//
// 命中缓存时复用已算好的候选（评分/排序/频控/保量等重计算被跳过，预算/频控
// 在缓存窗口内少量超发，符合"近似即可、预算可扣负"的取舍）；但 expire_at 这类
// 时间敏感字段在返回前用当前时间与广告主配置重算，保证客户端过期判断准确。
func (s *Server) resolveDecision(r *http.Request, snap *config.Snapshot, app *config.App, req adRequest, now time.Time) engine.Response {
	cfg := snap.DecisionCacheConfig()
	if s.DecisionCache != nil && cfg.Enabled {
		key := decisionCacheKey(app.ID, req.Style, req)
		if b, hit, err := s.DecisionCache.Get(r.Context(), key); err == nil && hit {
			var cached engine.Response
			if json.Unmarshal(b, &cached) == nil {
				recomputeExpireAt(snap, cached.Items, now)
				return cached
			}
		}
		// miss：实时计算并回填缓存
		resp := s.Engine.Decide(snap, engine.Request{
			App: app, Style: req.Style, DeviceID: req.DeviceID,
			Country: req.Country, Language: req.Language,
			Count: req.Count, Now: now,
		})
		if b, err := json.Marshal(resp); err == nil {
			_ = s.DecisionCache.Set(r.Context(), key, b, time.Duration(cfg.TTLSeconds)*time.Second)
		}
		return resp
	}
	return s.Engine.Decide(snap, engine.Request{
		App: app, Style: req.Style, DeviceID: req.DeviceID,
		Country: req.Country, Language: req.Language,
		Count: req.Count, Now: now,
	})
}

// contentHashRe 从对象 key（creatives/{sha256}.ext）末尾提取内容 SHA-256。
var contentHashRe = regexp.MustCompile(`([0-9a-f]{64})\.[a-z0-9]+$`)

// contentHash 提取素材内容哈希；非内容寻址命名（如历史 UUID）或 html 外链返回空。
func contentHash(storagePath string) string {
	if m := contentHashRe.FindStringSubmatch(storagePath); m != nil {
		return m[1]
	}
	return ""
}

// recomputeExpireAt 缓存命中后，按当前时间与各广告主 deliver_ttl 重算 expire_at。
func recomputeExpireAt(snap *config.Snapshot, items []engine.Item, now time.Time) {
	for i := range items {
		it := &items[i]
		ttl := config.DefaultDeliverTTLMinutes
		if it.Creative != nil {
			if campID, ok := snap.CreativeCampaign[it.Creative.ID]; ok {
				if camp := snap.Campaigns[campID]; camp != nil {
					ttl = camp.DeliverTTL()
				}
			}
		}
		it.ExpireAt = now.Add(time.Duration(ttl) * time.Minute).Unix()
	}
}

// decisionCacheKey 决策缓存键：由所有影响决策的输入决定（app/style/device/
// country/language/count）。Now 不入键——同一窗口内相同输入返回同一结果。
// Count 按引擎规则归一化（<1→1，>MaxCount→MaxCount）以对齐引擎实际行为。
func decisionCacheKey(appID, style string, req adRequest) string {
	c := req.Count
	if c < 1 {
		c = 1
	}
	if c > engine.MaxCount {
		c = engine.MaxCount
	}
	h := sha256.New()
	h.Write([]byte(appID))
	h.Write([]byte{0})
	h.Write([]byte(style))
	h.Write([]byte{0})
	h.Write([]byte(req.DeviceID))
	h.Write([]byte{0})
	h.Write([]byte(req.Country))
	h.Write([]byte{0})
	h.Write([]byte(req.Language))
	h.Write([]byte{0})
	h.Write([]byte(strconv.Itoa(c)))
	return "decision:" + hex.EncodeToString(h.Sum(nil))
}

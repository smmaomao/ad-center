package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ClickRecord 一次点击的归因登记行：点击发生时服务端快照（app/style/advertiser/
// device/creative），S2S 转化回调凭 clickid 反查还原归属。归属只信服务端自己的
// 点击登记，不信回调自报归属参数（防伪造归因）。
type ClickRecord struct {
	AppID, AdvertiserID, Style string
	DeviceID, CreativeID       string
}

// ClickTTL 点击登记的有效归因窗口：超过则视为不可归因，防止 clicks 表无限增长
// 且旧点击被误归因。取保守 30 天，与广告主常见 7~30 天窗口对齐。
const ClickTTL = 30 * 24 * time.Hour

// RegisterClick 登记一次点击（幂等：同 click_id 不覆盖，避免重复上报覆盖上下文）。
func (s *Store) RegisterClick(ctx context.Context, clickID string, c ClickRecord) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO clicks (click_id, app_code, advertiser_id, style, device_id, creative_id)
		VALUES ($1, $2, $3::bigint, $4, $5, $6::bigint)
		ON CONFLICT (click_id) DO NOTHING`,
		clickID, c.AppID, nilIfEmpty(c.AdvertiserID), c.Style,
		nilIfEmpty(c.DeviceID), nilIfEmpty(c.CreativeID))
	return err
}

// ResolveClick 反查点击登记（S2S 转化归因用）。点击不存在或已超 ClickTTL
// 一律返回 (nil, false, nil)——调用方据此判"不可归因，只确认不扣费"。
func (s *Store) ResolveClick(ctx context.Context, clickID string) (*ClickRecord, bool, error) {
	var c ClickRecord
	var createdAt time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT app_code, advertiser_id, style, device_id, creative_id, created_at
		FROM clicks WHERE click_id = $1`, clickID).
		Scan(&c.AppID, &c.AdvertiserID, &c.Style, &c.DeviceID, &c.CreativeID, &createdAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if time.Since(createdAt) > ClickTTL {
		return nil, false, nil
	}
	return &c, true, nil
}

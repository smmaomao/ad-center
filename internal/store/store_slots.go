package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// ============================================================
// 广告位 CRUD + 填充优先级（拖拽排序 = 全量替换，position = 数组下标）
// ============================================================

// PriorityInput 填充优先级入参（创建/替换共用）。
type PriorityInput struct {
	SourceType      string  `json:"source_type"` // advertiser / max / fallback
	AdvertiserID    string  `json:"advertiser_id,omitempty"`
	ExpectedECPM    float64 `json:"expected_ecpm,omitempty"`
	GuaranteedShare float64 `json:"guaranteed_share,omitempty"`
	Weight          float64 `json:"weight,omitempty"`
	Enabled         bool    `json:"enabled,omitempty"`
}

// SlotDetail 广告位详情（含全部优先级，含 disabled 行）。
type SlotDetail struct {
	AdminSlot
	Priorities []AdminPriority `json:"priorities"`
}

// AdminPriority 优先级行视图。
type AdminPriority struct {
	ID              string  `json:"id"`
	SourceType      string  `json:"source_type"`
	AdvertiserID    string  `json:"advertiser_id,omitempty"`
	AdvertiserName  string  `json:"advertiser_name,omitempty"`
	ExpectedECPM    float64 `json:"expected_ecpm"`
	GuaranteedShare float64 `json:"guaranteed_share"`
	Weight          float64 `json:"weight"`
	Enabled         bool    `json:"enabled"`
	Position        int     `json:"position"`
}

// slotWritableCols 广告位可写列（白名单防注入；slot_key/app_id 创建后不可改：
// 客户端以 slot_key 为稳定标识，换 App 需重建广告位）。
var slotWritable = map[string]bool{
	"name": true, "type": true, "status": true,
	"freq_daily_limit": true, "freq_interval_minutes": true, "freq_fatigue_window": true,
	"ai_agent_enabled": true, "ai_agent_goal": true, "updated_by": true,
}

// CreateSlot 新建广告位（+ 优先级，事务）。slot_key 全局唯一，冲突报错。
func (s *Store) CreateSlot(ctx context.Context, fields map[string]any, priorities []PriorityInput) (string, error) {
	for _, k := range []string{"app_id", "slot_key", "name", "type"} {
		if _, ok := fields[k]; !ok {
			return "", fmt.Errorf("%s required", k)
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	// slot_key/app_id 只在创建时写入
	cols, placeholders, args := buildInsert(fields, mergeWritable(slotWritable,
		map[string]bool{"app_id": true, "slot_key": true, "created_by": true}))
	var id string
	err = tx.QueryRow(ctx, fmt.Sprintf(
		`INSERT INTO ad_slots (%s) VALUES (%s) RETURNING slot_id::text`,
		cols, placeholders), args...).Scan(&id)
	if err != nil {
		return "", err
	}
	if err := insertPriorities(ctx, tx, id, priorities); err != nil {
		return "", err
	}
	return id, tx.Commit(ctx)
}

// GetSlotDetail 广告位详情（未删除），含优先级（position 升序，含停用行）。
func (s *Store) GetSlotDetail(ctx context.Context, id string) (*SlotDetail, error) {
	d := &SlotDetail{}
	err := s.pool.QueryRow(ctx, `
		SELECT s.slot_id::text, s.app_id::text, a.name, s.slot_key, s.name, s.type, s.status,
		       s.freq_daily_limit, s.freq_interval_minutes, s.freq_fatigue_window,
		       (SELECT count(*) FROM fill_priorities f WHERE f.slot_id = s.slot_id AND f.enabled)
		FROM ad_slots s JOIN apps a ON a.app_id = s.app_id
		WHERE s.slot_id = $1 AND s.deleted_at IS NULL`, id).
		Scan(&d.ID, &d.AppID, &d.AppName, &d.Key, &d.Name, &d.Type, &d.Status,
			&d.FreqDailyLimit, &d.FreqIntervalMinutes, &d.FreqFatigueWindow, &d.FillCount)
	if err != nil {
		return nil, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT f.id::text, f.source_type, COALESCE(f.advertiser_id::text, ''),
		       COALESCE(adv.name, ''), f.expected_ecpm::float8,
		       f.guaranteed_share::float8, f.weight::float8, f.enabled, f.position
		FROM fill_priorities f
		LEFT JOIN advertisers adv ON adv.advertiser_id = f.advertiser_id
		WHERE f.slot_id = $1 ORDER BY f.position`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		p := AdminPriority{}
		if err := rows.Scan(&p.ID, &p.SourceType, &p.AdvertiserID, &p.AdvertiserName,
			&p.ExpectedECPM, &p.GuaranteedShare, &p.Weight, &p.Enabled, &p.Position); err != nil {
			return nil, err
		}
		d.Priorities = append(d.Priorities, p)
	}
	return d, rows.Err()
}

// UpdateSlot 部分更新广告位；priorities 非 nil 时全量替换（拖拽排序语义）。
func (s *Store) UpdateSlot(ctx context.Context, id string, fields map[string]any, priorities []PriorityInput, hasPriorities bool) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if len(fields) > 0 {
		sets, args := buildUpdate(fields, slotWritable)
		if len(sets) > 0 {
			args = append(args, id)
			tag, err := tx.Exec(ctx, fmt.Sprintf(
				`UPDATE ad_slots SET %s, updated_at = now()
				 WHERE slot_id = $%d AND deleted_at IS NULL`,
				strings.Join(sets, ", "), len(args)), args...)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return fmt.Errorf("slot not found: %s", id)
			}
		}
	}

	if hasPriorities {
		if _, err := tx.Exec(ctx, `DELETE FROM fill_priorities WHERE slot_id = $1`, id); err != nil {
			return err
		}
		if err := insertPriorities(ctx, tx, id, priorities); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// SoftDeleteSlot 软删广告位（优先级行保留供审计，快照不再加载）。
func (s *Store) SoftDeleteSlot(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE ad_slots SET deleted_at = now(), status = 'paused'
		 WHERE slot_id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("slot not found: %s", id)
	}
	return nil
}

// insertPriorities 按数组顺序写入（position = 下标）。nil 广告主 → NULL。
// 注意：enabled 缺省为 false（JSON 无法区分"未传"与"false"），
// 前端全量替换时每行必须显式传 enabled。
func insertPriorities(ctx context.Context, tx pgx.Tx, slotID string, priorities []PriorityInput) error {
	for pos, p := range priorities {
		var advID any
		if p.AdvertiserID != "" {
			advID = p.AdvertiserID
		}
		weight := p.Weight
		if weight <= 0 {
			weight = 1.0
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO fill_priorities
			    (slot_id, source_type, advertiser_id, expected_ecpm, guaranteed_share, weight, enabled, position)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			slotID, p.SourceType, advID, p.ExpectedECPM, p.GuaranteedShare, weight, p.Enabled, pos); err != nil {
			return err
		}
	}
	return nil
}

func mergeWritable(a, b map[string]bool) map[string]bool {
	out := make(map[string]bool, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

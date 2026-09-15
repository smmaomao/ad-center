package store

import (
	"context"
	"fmt"
	"strings"
)

// ============================================================
// 素材 CRUD（上传本身走 R2 presigned PUT 直传，这里只管元数据注册）
// ============================================================

// AdminCreative 素材管理视图（纯资产属性；商业与策略在 campaign 维度）。
type AdminCreative struct {
	ID            string `json:"id"`
	AdvertiserID  string `json:"advertiser_id"`
	Name          string `json:"name"`
	MediaType     string `json:"media_type"`
	StoragePath   string `json:"storage_path"`
	FileSizeBytes int64  `json:"file_size_bytes"`
	Orientation   string `json:"orientation"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	DurationMS    int    `json:"duration_ms"`
	Status        string `json:"status"`
	ABGroup       string `json:"ab_group"`

	// 展现样式（多选）：splash / rewarded_video / interstitial / feed / banner
	Styles []string `json:"styles"`
	// 投放目标 App（多选，空 = 全部 App）
	TargetApps []string `json:"target_apps"`
}

const adminCreativeCols = `
	id::text, COALESCE(advertiser_id::text, ''), name, media_type, storage_path,
	file_size_bytes, orientation, COALESCE(width, 0), COALESCE(height, 0),
	COALESCE(duration_ms, 0), status, COALESCE(ab_group, ''),
	COALESCE(styles, '{}'), COALESCE(target_apps, '{}')::text[]`

func scanAdminCreative(scan func(...any) error) (*AdminCreative, error) {
	c := &AdminCreative{}
	if err := scan(&c.ID, &c.AdvertiserID, &c.Name, &c.MediaType, &c.StoragePath,
		&c.FileSizeBytes, &c.Orientation, &c.Width, &c.Height,
		&c.DurationMS, &c.Status, &c.ABGroup,
		&c.Styles, &c.TargetApps); err != nil {
		return nil, err
	}
	return c, nil
}

// ListCreatives 素材列表（可按广告主过滤，未删除）。
func (s *Store) ListCreatives(ctx context.Context, advertiserID string) ([]*AdminCreative, error) {
	q := `SELECT ` + adminCreativeCols + ` FROM creatives WHERE deleted_at IS NULL`
	var args []any
	if advertiserID != "" {
		q += ` AND advertiser_id = $1::bigint`
		args = append(args, advertiserID)
	}
	q += ` ORDER BY created_at DESC`
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AdminCreative
	for rows.Next() {
		c, err := scanAdminCreative(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetCreative 按 id 取单条素材（未删除）。找不到返回 (nil, pgx.ErrNoRows)。
func (s *Store) GetCreative(ctx context.Context, id string) (*AdminCreative, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+adminCreativeCols+`
		FROM creatives WHERE id = $1::bigint AND deleted_at IS NULL`, id)
	return scanAdminCreative(row.Scan)
}

// creativeWritable 素材可写列（storage_path 创建时写入，更新时也允许替换为新上传的对象 key）。
var creativeWritable = map[string]bool{
	"advertiser_id": true, "name": true, "media_type": true, "storage_path": true,
	"file_size_bytes": true, "status": true, "ab_group": true,
	"orientation": true, "width": true, "height": true, "duration_ms": true,
	// 展现样式 / 投放 App
	"styles": true, "target_apps": true,
	"updated_by": true,
}

// CreateCreative 注册素材元数据（R2 直传完成后由前端调用）。
// 素材库为公共库：advertiser_id 可空（不绑定具体广告主）。
func (s *Store) CreateCreative(ctx context.Context, fields map[string]any) (string, error) {
	for _, k := range []string{"name", "media_type", "storage_path"} {
		if _, ok := fields[k]; !ok {
			return "", fmt.Errorf("%s required", k)
		}
	}
	normalizeCreativeArrays(fields)
	cols, placeholders, args := buildInsert(fields, creativeWritable, creativeIntCols)
	var id string
	err := s.pool.QueryRow(ctx, fmt.Sprintf(
		`INSERT INTO creatives (%s) VALUES (%s) RETURNING id::text`,
		cols, placeholders), args...).Scan(&id)
	return id, err
}

// creativeUpdatable 可更新白名单（创建后允许改投放相关属性，含重新上传后更换 storage_path）。
var creativeUpdatable = map[string]bool{
	"name": true, "status": true, "ab_group": true, "media_type": true,
	"orientation": true, "width": true, "height": true, "duration_ms": true,
	"storage_path": true,
	"styles":       true, "target_apps": true,
	"updated_by": true,
}

// creativeIntCols 取值需以 bigint 绑定的列（由 uuid 迁移而来的 id 列）。
// advertiser_id 可空（素材公共库不绑定广告主），空字符串按 NULL 绑定。
var creativeIntCols = map[string]bool{"advertiser_id": true}

// normalizeCreativeArrays 把 JSON 解码出的 []any 转成 []string。
//
// 为什么需要：管理 API 收到的是前端 JSON，styles / target_apps 解码后是
// []any，pgx 无法把它编码进 text[] / uuid[] 列；显式转换后才能正确落库。
func normalizeCreativeArrays(fields map[string]any) {
	for _, k := range []string{"styles", "target_apps"} {
		v, ok := fields[k]
		if !ok || v == nil {
			continue
		}
		switch t := v.(type) {
		case []string:
			fields[k] = t
		case []any:
			out := make([]string, 0, len(t))
			for _, x := range t {
				if s, ok := x.(string); ok && s != "" {
					out = append(out, s)
				}
			}
			fields[k] = out
		default:
			delete(fields, k) // 类型不对：丢弃该字段，避免写库报错
		}
	}
}

// UpdateCreative 部分更新（白名单列）。
func (s *Store) UpdateCreative(ctx context.Context, id string, fields map[string]any) error {
	normalizeCreativeArrays(fields)
	sets, args := buildUpdate(fields, creativeUpdatable, creativeIntCols)
	if len(sets) == 0 {
		return fmt.Errorf("no writable fields")
	}
	args = append(args, id)
	tag, err := s.pool.Exec(ctx, fmt.Sprintf(
		`UPDATE creatives SET %s, updated_at = now()
		 WHERE id = $%d::bigint AND deleted_at IS NULL`,
		strings.Join(sets, ", "), len(args)), args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("creative not found: %s", id)
	}
	return nil
}

// SoftDeleteCreative 软删素材（快照即刻不再下发；ad_events FK 保留审计轨迹）。
func (s *Store) SoftDeleteCreative(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE creatives SET deleted_at = now(), status = 'paused'
		 WHERE id = $1::bigint AND deleted_at IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("creative not found: %s", id)
	}
	return nil
}

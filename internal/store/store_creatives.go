package store

import (
	"context"
	"fmt"
	"strings"
)

// ============================================================
// 素材 CRUD（上传本身走 R2 presigned PUT 直传，这里只管元数据注册）
// ============================================================

// AdminCreative 素材管理视图。
type AdminCreative struct {
	ID            string  `json:"id"`
	AdvertiserID  string  `json:"advertiser_id"`
	Name          string  `json:"name"`
	MediaType     string  `json:"media_type"`
	StoragePath   string  `json:"storage_path"`
	FileSizeBytes int64   `json:"file_size_bytes"`
	Orientation   string  `json:"orientation"`
	Width         int     `json:"width"`
	Height        int     `json:"height"`
	DurationMS    int     `json:"duration_ms"`
	Status        string  `json:"status"`
	Weight        float64 `json:"weight"`
	ABGroup       string  `json:"ab_group"`
}

const adminCreativeCols = `
	creative_id::text, advertiser_id::text, name, media_type, storage_path,
	file_size_bytes, orientation, COALESCE(width, 0), COALESCE(height, 0),
	COALESCE(duration_ms, 0), status, weight::float8, COALESCE(ab_group, '')`

func scanAdminCreative(scan func(...any) error) (*AdminCreative, error) {
	c := &AdminCreative{}
	if err := scan(&c.ID, &c.AdvertiserID, &c.Name, &c.MediaType, &c.StoragePath,
		&c.FileSizeBytes, &c.Orientation, &c.Width, &c.Height,
		&c.DurationMS, &c.Status, &c.Weight, &c.ABGroup); err != nil {
		return nil, err
	}
	return c, nil
}

// ListCreatives 素材列表（可按广告主过滤，未删除）。
func (s *Store) ListCreatives(ctx context.Context, advertiserID string) ([]*AdminCreative, error) {
	q := `SELECT ` + adminCreativeCols + ` FROM creatives WHERE deleted_at IS NULL`
	var args []any
	if advertiserID != "" {
		q += ` AND advertiser_id = $1`
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

// creativeWritable 素材可写列（storage_path 不可改——对象 key 与文件一一对应，
// 换素材 = 新建记录）。
var creativeWritable = map[string]bool{
	"advertiser_id": true, "name": true, "media_type": true, "storage_path": true,
	"file_size_bytes": true, "status": true, "weight": true, "ab_group": true,
	"orientation": true, "width": true, "height": true, "duration_ms": true,
	"updated_by": true,
}

// CreateCreative 注册素材元数据（R2 直传完成后由前端调用）。
func (s *Store) CreateCreative(ctx context.Context, fields map[string]any) (string, error) {
	for _, k := range []string{"advertiser_id", "name", "media_type", "storage_path"} {
		if _, ok := fields[k]; !ok {
			return "", fmt.Errorf("%s required", k)
		}
	}
	cols, placeholders, args := buildInsert(fields, creativeWritable)
	var id string
	err := s.pool.QueryRow(ctx, fmt.Sprintf(
		`INSERT INTO creatives (%s) VALUES (%s) RETURNING creative_id::text`,
		cols, placeholders), args...).Scan(&id)
	return id, err
}

// creativeUpdatable 可更新白名单（创建后只允许改投放相关属性）。
var creativeUpdatable = map[string]bool{
	"name": true, "status": true, "weight": true, "ab_group": true,
	"orientation": true, "width": true, "height": true, "duration_ms": true,
	"updated_by": true,
}

// UpdateCreative 部分更新（白名单列）。
func (s *Store) UpdateCreative(ctx context.Context, id string, fields map[string]any) error {
	sets, args := buildUpdate(fields, creativeUpdatable)
	if len(sets) == 0 {
		return fmt.Errorf("no writable fields")
	}
	args = append(args, id)
	tag, err := s.pool.Exec(ctx, fmt.Sprintf(
		`UPDATE creatives SET %s, updated_at = now()
		 WHERE creative_id = $%d AND deleted_at IS NULL`,
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
		 WHERE creative_id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("creative not found: %s", id)
	}
	return nil
}

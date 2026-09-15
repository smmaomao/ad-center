package store

import (
	"context"
	"fmt"
	"strings"
)

// ============================================================
// 产品（Product）CRUD
// 产品挂在广告主下（advertiser → product → campaign），用于多产品广告主的归集：
// 一个广告主公司旗下多款产品各自跑 campaign，按产品维度看预算 / KPI / 报表。
// ============================================================

// AdminProduct 产品管理视图。
type AdminProduct struct {
	ID             string  `json:"id"`
	AdvertiserID   string  `json:"advertiser_id"`
	AdvertiserName string  `json:"advertiser_name"`
	Name           string  `json:"name"`
	Status         string  `json:"status"`
	DailyBudget    float64 `json:"daily_budget"`
	Notes          string  `json:"notes"`
	CreatedAt      *string `json:"created_at"`
	UpdatedAt      *string `json:"updated_at"`
}

const productCols = `
	p.id::text, p.advertiser_id::text, a.name, p.name, p.status,
	p.daily_budget::float8, COALESCE(p.notes, ''),
	p.created_at::text, p.updated_at::text`

func scanProduct(scan func(...any) error) (*AdminProduct, error) {
	p := &AdminProduct{}
	if err := scan(&p.ID, &p.AdvertiserID, &p.AdvertiserName, &p.Name, &p.Status,
		&p.DailyBudget, &p.Notes, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	return p, nil
}

// ListProducts 产品列表（按广告主过滤可选，未删除，按创建时间倒序）。
func (s *Store) ListProducts(ctx context.Context, advertiserID string) ([]*AdminProduct, error) {
	q := `
		SELECT ` + productCols + `
		FROM products p JOIN advertisers a ON a.id = p.advertiser_id
		WHERE p.deleted_at IS NULL`
	args := []any{}
	if advertiserID != "" {
		q += ` AND p.advertiser_id = $1::bigint`
		args = append(args, advertiserID)
	}
	q += ` ORDER BY p.created_at DESC`
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AdminProduct
	for rows.Next() {
		p, err := scanProduct(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetProduct 单个产品详情。
func (s *Store) GetProduct(ctx context.Context, id string) (*AdminProduct, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT `+productCols+`
		FROM products p JOIN advertisers a ON a.id = p.advertiser_id
		WHERE p.id = $1::bigint AND p.deleted_at IS NULL`, id)
	return scanProduct(row.Scan)
}

var productWritable = map[string]bool{
	"advertiser_id": true, "name": true, "status": true,
	"daily_budget": true, "notes": true, "created_by": true, "updated_by": true,
}

// productIntCols 取值需以 bigint 绑定的列（由 uuid 迁移而来的 id 列）。
var productIntCols = map[string]bool{"advertiser_id": true}

// CreateProduct 新建产品。
func (s *Store) CreateProduct(ctx context.Context, fields map[string]any) (string, error) {
	for _, k := range []string{"advertiser_id", "name"} {
		if v, ok := fields[k]; !ok || v == "" {
			return "", fmt.Errorf("%s required", k)
		}
	}
	cols, placeholders, args := buildInsert(fields, productWritable, productIntCols)
	var id string
	err := s.pool.QueryRow(ctx, fmt.Sprintf(
		`INSERT INTO products (%s) VALUES (%s) RETURNING id::text`,
		cols, placeholders), args...).Scan(&id)
	return id, err
}

var productUpdatable = map[string]bool{
	"name": true, "status": true, "daily_budget": true, "notes": true,
	"updated_by": true,
}

// UpdateProduct 部分更新（白名单列）。
func (s *Store) UpdateProduct(ctx context.Context, id string, fields map[string]any) error {
	sets, args := buildUpdate(fields, productUpdatable, productIntCols)
	if len(sets) == 0 {
		return fmt.Errorf("no writable fields")
	}
	args = append(args, id)
	tag, err := s.pool.Exec(ctx, fmt.Sprintf(
		`UPDATE products SET %s, updated_at = now()
		 WHERE id = $%d::bigint AND deleted_at IS NULL`,
		strings.Join(sets, ", "), len(args)), args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("product not found: %s", id)
	}
	return nil
}

// SoftDeleteProduct 软删产品（保留审计轨迹）。
func (s *Store) SoftDeleteProduct(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE products SET deleted_at = now() WHERE id = $1::bigint AND deleted_at IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("product not found: %s", id)
	}
	return nil
}

// ProductAdvertiser 返回产品所属广告主（campaign 反查 advertiser_id 用）。
func (s *Store) ProductAdvertiser(ctx context.Context, productID string) (string, error) {
	var adv string
	err := s.pool.QueryRow(ctx,
		`SELECT advertiser_id::text FROM products WHERE id = $1::bigint AND deleted_at IS NULL`,
		productID).Scan(&adv)
	if err != nil {
		return "", fmt.Errorf("product not found: %s", productID)
	}
	return adv, nil
}

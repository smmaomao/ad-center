package store

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// ============================================================
// 管理 API 数据访问（Next.js BFF 调用，写操作触发 NOTIFY 自动刷新缓存）
// ============================================================

// AdminAdvertiser 管理 API 的广告主视图（身份 + 计费锚点；KPI/排期在广告任务维度）。
type AdminAdvertiser struct {
	ID              string                `json:"id"`
	Name            string                `json:"name"`
	Status          string                `json:"status"`
	BiddingPrice    float64               `json:"bidding_price"`
	BiddingPriceMin float64               `json:"bidding_price_min"` // 单价下限；0=未配置→固定 BiddingPrice
	BillingMode     string                `json:"billing_mode"`      // 计费方式（cpm/cpc/cpa），扣费锚点
	CPAEventPrices  map[string][2]float64 `json:"cpa_event_prices"`  // 事件→[min,max] 区间
	WalletEnabled   bool                  `json:"wallet_enabled"`    // 是否启用总钱包闸（首次充值置 true）
	WalletBalance   float64               `json:"wallet_balance"`    // 总余额 = 累计充值 - 累计扣费
	Contact         string                `json:"contact"`
	Notes           string                `json:"notes"`
	CreatedAt       string                `json:"created_at"` // 创建日期（YYYY-MM-DD）
}

// adminAdvertiserCols 管理端广告主列。
//
// 广告主只承载身份 + 计费锚点；KPI/排期/消耗节奏/下发有效期已下沉到 campaign。
// 广告主维度达成率由旗下 campaign 汇总（handleRollupAdvertiserKPIs →
// CampaignRollup），其口径与 config.Campaign.Achievement() 一致
// （target/actual、actual≤0 取 1、clamp [0.25, 4.0]）。改
// config.minAchievement/maxAchievement 时同步改 store_campaigns.go 的 SQL。
const adminAdvertiserCols = `
	id::text, name, status,
	bidding_price::float8, bidding_price_min::float8,
	billing_mode, cpa_event_prices::text,
	wallet_enabled, wallet_balance::float8,
	COALESCE(contact, ''),
	COALESCE(notes, ''), to_char(created_at, 'YYYY-MM-DD HH24:MI:SS')`

func scanAdminAdvertiser(scan func(...any) error) (*AdminAdvertiser, error) {
	a := &AdminAdvertiser{}
	var cpaPrices []byte
	if err := scan(&a.ID, &a.Name, &a.Status,
		&a.BiddingPrice, &a.BiddingPriceMin,
		&a.BillingMode, &cpaPrices,
		&a.WalletEnabled, &a.WalletBalance,
		&a.Contact, &a.Notes, &a.CreatedAt); err != nil {
		return nil, err
	}
	if len(cpaPrices) > 0 {
		if err := json.Unmarshal(cpaPrices, &a.CPAEventPrices); err != nil {
			return nil, err
		}
	}
	return a, nil
}

// ListAdvertisers 广告主列表（未删除，按 tier/创建时间）。
func (s *Store) ListAdvertisers(ctx context.Context) ([]*AdminAdvertiser, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+adminAdvertiserCols+`
		FROM advertisers WHERE deleted_at IS NULL
		ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AdminAdvertiser
	for rows.Next() {
		a, err := scanAdminAdvertiser(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetAdvertiser 单个广告主详情。
func (s *Store) GetAdvertiser(ctx context.Context, id string) (*AdminAdvertiser, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT `+adminAdvertiserCols+`
		FROM advertisers WHERE id = $1::bigint AND deleted_at IS NULL`, id)
	return scanAdminAdvertiser(row.Scan)
}

// advertiserCols 管理端可写列（更新白名单，防注入）。
var advertiserWritable = map[string]bool{
	"name": true, "status": true,
	"bidding_price": true, "bidding_price_min": true,
	"billing_mode": true, "cpa_event_prices": true,
	"contact": true, "notes": true, "updated_by": true,
}

// normalizeJSONFields 把 map/slice 形式的 JSON 列值显式序列化为 []byte。
//
// 为什么需要：管理 API 接收的是前端 JSON，cpa_event_prices 解码后是
// map[string]any；直接交给 pgx 依赖驱动的 OID 推断，显式 marshal 可确保一定
// 以 jsonb 对象落库（而不是被当成 text 或报错）。已是字符串/字节的值原样透传。
func normalizeJSONFields(fields map[string]any, keys ...string) error {
	for _, k := range keys {
		v, ok := fields[k]
		if !ok || v == nil {
			continue
		}
		switch v.(type) {
		case []byte, string:
		default:
			b, err := json.Marshal(v)
			if err != nil {
				return fmt.Errorf("%s: %w", k, err)
			}
			fields[k] = b
		}
	}
	return nil
}

// CreateAdvertiser 新建广告主。
func (s *Store) CreateAdvertiser(ctx context.Context, fields map[string]any) (string, error) {
	if _, ok := fields["name"]; !ok {
		return "", fmt.Errorf("name required")
	}
	if err := normalizeJSONFields(fields, "cpa_event_prices"); err != nil {
		return "", err
	}
	cols, placeholders, args := buildInsert(fields, advertiserWritable, nil)
	var id string
	err := s.pool.QueryRow(ctx, fmt.Sprintf(
		`INSERT INTO advertisers (%s) VALUES (%s) RETURNING id::text`,
		cols, placeholders), args...).Scan(&id)
	return id, err
}

// UpdateAdvertiser 部分更新（白名单列）。
func (s *Store) UpdateAdvertiser(ctx context.Context, id string, fields map[string]any) error {
	if err := normalizeJSONFields(fields, "cpa_event_prices"); err != nil {
		return err
	}
	sets, args := buildUpdate(fields, advertiserWritable, nil)
	if len(sets) == 0 {
		return fmt.Errorf("no writable fields")
	}
	args = append(args, id)
	tag, err := s.pool.Exec(ctx, fmt.Sprintf(
		`UPDATE advertisers SET %s WHERE id = $%d::bigint AND deleted_at IS NULL`,
		strings.Join(sets, ", "), len(args)), args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("advertiser not found: %s", id)
	}
	return nil
}

// SoftDeleteAdvertiser 软删（保留审计轨迹）。
func (s *Store) SoftDeleteAdvertiser(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE advertisers SET deleted_at = now() WHERE id = $1::bigint AND deleted_at IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("advertiser not found: %s", id)
	}
	return nil
}

// AdminSlot 广告位管理视图。
type AdminSlot struct {
	ID                  string `json:"id"`
	AppID               string `json:"app_code"`
	AppName             string `json:"app_name"`
	Key                 string `json:"key"`
	Name                string `json:"name"`
	Type                string `json:"type"`
	Status              string `json:"status"`
	FreqDailyLimit      int    `json:"freq_daily_limit"`
	FreqIntervalMinutes int    `json:"freq_interval_minutes"`
	FreqFatigueWindow   int    `json:"freq_fatigue_window"`
	FillCount           int    `json:"fill_count"` // 启用的填充来源数
	AIAgentEnabled      bool   `json:"ai_agent_enabled"`
	AIAgentGoal         string `json:"ai_agent_goal"`
}

const slotCols = `
	s.id::text, s.app_code::text, a.name, s.slot_key, s.name, s.type, s.status,
	s.freq_daily_limit, s.freq_interval_minutes, s.freq_fatigue_window, %s
	s.ai_agent_enabled, s.ai_agent_goal`

// ListSlots 广告位列表（带 App 名与填充来源计数）。
func (s *Store) ListSlots(ctx context.Context) ([]*AdminSlot, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+fmt.Sprintf(slotCols, `
		       (SELECT count(*) FROM fill_priorities f WHERE f.slot_code = s.id AND f.enabled),`)+`
		FROM ad_slots s JOIN apps a ON a.code = s.app_code
		WHERE s.deleted_at IS NULL
		ORDER BY a.name, s.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AdminSlot
	for rows.Next() {
		sl := &AdminSlot{}
		if err := rows.Scan(&sl.ID, &sl.AppID, &sl.AppName, &sl.Key, &sl.Name, &sl.Type, &sl.Status,
			&sl.FreqDailyLimit, &sl.FreqIntervalMinutes, &sl.FreqFatigueWindow, &sl.FillCount,
			&sl.AIAgentEnabled, &sl.AIAgentGoal); err != nil {
			return nil, err
		}
		out = append(out, sl)
	}
	return out, rows.Err()
}

// AdminApp App 管理视图（密钥仅展示当前值，用于复制）。
type AdminApp struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Status      string  `json:"status"`
	CallbackURL *string `json:"callback_url"` // 业务后端 S2S 接收地址
	SecretKey   string  `json:"secret_key"`   // S2S 签名密钥（支持重置）
	APIKey      *string `json:"api_key"`      // API Key 明文（仅展示/交付；鉴权走 hash）
	CreatedAt   *string `json:"created_at"`   // 创建日期（YYYY-MM-DD）
}

// ListApps App 列表（已软删的不展示）。
func (s *Store) ListApps(ctx context.Context) ([]*AdminApp, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT code, name, status,
		        callback_url, secret_key, api_key, to_char(created_at, 'YYYY-MM-DD HH24:MI:SS')
		 FROM apps WHERE deleted_at IS NULL ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AdminApp
	for rows.Next() {
		a := &AdminApp{}
		if err := rows.Scan(&a.ID, &a.Name, &a.Status,
			&a.CallbackURL, &a.SecretKey, &a.APIKey, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SoftDeleteApp 软删 App（保留历史归因与事件数据；快照与列表均按 deleted_at 过滤）。
func (s *Store) SoftDeleteApp(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE apps SET deleted_at = now() WHERE code = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("app not found: %s", id)
	}
	return nil
}

// CreateApp 注册 App：app_code 短字符串由本方法生成（10 位 数字+小写字母），
// api key 哈希由调用方生成，明文一并落库（仅供后台展示/复制，鉴权仍走 hash）。
// app_code（apps.code）全局唯一由 apps 的 code 唯一约束保证，极小概率冲突时自动换一个重试。
func (s *Store) CreateApp(ctx context.Context, name, key, keyHash string) (string, error) {
	for attempt := 0; attempt < 5; attempt++ {
		id, err := generateAppID()
		if err != nil {
			return "", err
		}
		if _, err = s.pool.Exec(ctx, `
			INSERT INTO apps (code, name, api_key, api_key_hash)
			VALUES ($1, $2, $3, $4)`, id, name, key, keyHash); err != nil {
			if isUniqueViolation(err) {
				continue
			}
			return "", err
		}
		return id, nil
	}
	return "", fmt.Errorf("alloc app id: exhausted retries")
}

// ResetAppKey 重新生成 API Key（旧密钥立即失效）：key/hash 由调用方生成，
// 明文与哈希一并更新。
func (s *Store) ResetAppKey(ctx context.Context, id, key, keyHash string) (string, error) {
	if _, err := s.pool.Exec(ctx, `
		UPDATE apps SET api_key = $1, api_key_hash = $2, updated_at = now()
		WHERE code = $3`, key, keyHash, id); err != nil {
		return "", err
	}
	return key, nil
}

// appIDAlphabet 应用短 ID 字符集：数字 + 小写字母（36 进制，ID/URL 安全）。
const appIDAlphabet = "0123456789abcdefghijklmnopqrstuvwxyz"

// generateAppID 生成 10 位「数字+小写字母」的短应用 ID。
func generateAppID() (string, error) {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, 10)
	for i, c := range b {
		out[i] = appIDAlphabet[int(c)%len(appIDAlphabet)]
	}
	return string(out), nil
}

// isUniqueViolation 判断是否是主键/唯一约束冲突（并发生成相同 app_code 时安全重试）。
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

// appUpdatable App 可更新列。api_key_hash 不在其中——换 key 必须走专门的
// 轮换流程（重新生成并安全交付给客户端），不能被普通编辑接口覆盖。
var appUpdatable = map[string]bool{
	"name": true, "status": true, "callback_url": true,
}

// UpdateApp 更新 App 名称 / 状态 / 业务回调地址（部分更新）。
func (s *Store) UpdateApp(ctx context.Context, id string, fields map[string]any) error {
	sets, args := buildUpdate(fields, appUpdatable, nil)
	if len(sets) == 0 {
		return fmt.Errorf("no writable fields")
	}
	args = append(args, id)
	tag, err := s.pool.Exec(ctx, fmt.Sprintf(
		`UPDATE apps SET %s WHERE code = $%d`,
		strings.Join(sets, ", "), len(args)), args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("app not found: %s", id)
	}
	return nil
}

// ResetAppSecret 重新生成 S2S 签名密钥（旧密钥立即失效，需同步改业务后端）。
func (s *Store) ResetAppSecret(ctx context.Context, id string) (string, error) {
	// 用内置 gen_random_uuid()（PG13+ 自带）：pgcrypto 的 gen_random_bytes 在
	// 本实例的 search_path 下不可见，直接用会让重置失败。
	var secret string
	err := s.pool.QueryRow(ctx, `
		UPDATE apps
		   SET secret_key = replace(gen_random_uuid()::text || gen_random_uuid()::text, '-', '')
		 WHERE code = $1
		RETURNING secret_key`, id).Scan(&secret)
	if err != nil {
		return "", err
	}
	return secret, nil
}

// buildInsert 由字段白名单构造 INSERT 片段。
//
// intCols 标记取值需以 bigint 绑定的列（由 uuid 迁移而来的 id 列）。这些列的字符串
// 取值需在 SQL 端显式 ::bigint 转型（pgx 发送 string 为 text OID，无 text→bigint 隐式
// 转换）；空字符串按 NULL 绑定（如素材可绑定空广告主）。
func buildInsert(fields map[string]any, allowed, intCols map[string]bool) (cols, placeholders string, args []any) {
	i := 0
	var colList, phList []string
	for k, v := range fields {
		if !allowed[k] {
			continue
		}
		i++
		colList = append(colList, k)
		if intCols != nil && intCols[k] {
			if s, ok := v.(string); ok && s == "" {
				v = nil
			}
			phList = append(phList, fmt.Sprintf("$%d::bigint", i))
		} else {
			phList = append(phList, fmt.Sprintf("$%d", i))
		}
		args = append(args, v)
	}
	return strings.Join(colList, ", "), strings.Join(phList, ", "), args
}

// buildUpdate 由字段白名单构造 SET 片段。intCols 语义同 buildInsert。
func buildUpdate(fields map[string]any, allowed, intCols map[string]bool) (sets []string, args []any) {
	for k, v := range fields {
		if !allowed[k] {
			continue
		}
		if intCols != nil && intCols[k] {
			if s, ok := v.(string); ok && s == "" {
				v = nil
			}
			sets = append(sets, fmt.Sprintf("%s = $%d::bigint", k, len(args)+1))
		} else {
			sets = append(sets, fmt.Sprintf("%s = $%d", k, len(args)+1))
		}
		args = append(args, v)
	}
	return sets, args
}

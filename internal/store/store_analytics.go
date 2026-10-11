package store

import (
	"context"
	"strings"
	"time"
)

// CreativeStat 单个素材的曝光/点击聚合。
type CreativeStat struct {
	CreativeID  string
	Impressions int64
	Clicks      int64
}

// CreativeStats 按素材聚合指定时间区间内的曝光(impression)与点击(click)事件。
//
// 数据源：ad_events（按月分区，按 ts 裁剪）。creative_id 列为 bigint。
// creativeIDs 为空时直接返回空 map（调用方应保证非空，或自行决定统计范围）。
// 分母口径：曝光 = impression 事件（真实展示），点击 = click 事件；fill（下发）不计入。
func (s *Store) CreativeStats(ctx context.Context, creativeIDs []string, from, to time.Time) (map[string]*CreativeStat, error) {
	out := make(map[string]*CreativeStat, len(creativeIDs))
	if len(creativeIDs) == 0 {
		return out, nil
	}
	// 构造 bigint 数组字面量传给 ANY(...)
	lit := "{" + strings.Join(creativeIDs, ",") + "}"
	rows, err := s.pool.Query(ctx, `
		SELECT creative_id::text, event_type, COUNT(*)
		FROM ad_events
		WHERE ts >= $1 AND ts < $2
		  AND event_type IN ('impression', 'click')
		  AND creative_id = ANY($3::bigint[])
		GROUP BY creative_id, event_type`,
		from, to, lit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, etype string
		var n int64
		if err := rows.Scan(&cid, &etype, &n); err != nil {
			return nil, err
		}
		st := out[cid]
		if st == nil {
			st = &CreativeStat{CreativeID: cid}
			out[cid] = st
		}
		switch etype {
		case "impression":
			st.Impressions = n
		case "click":
			st.Clicks = n
		}
	}
	return out, rows.Err()
}

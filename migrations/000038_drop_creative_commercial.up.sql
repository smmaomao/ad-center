-- 素材回归「纯资产」定位：商业与策略字段（billing_mode / price / cpa_event /
-- weight / start_at / end_at）属于投放计划（campaign）维度。引擎排序打分与排期
-- 已按 campaign 读取计费方式 / 出价 / 优先级 / 上下架时间（见 engine.go:scoreCreative
-- 与 store.go 的 snapshot 构建），素材层这些列是冗余历史包袱，删除之。
ALTER TABLE ads_center.creatives
  DROP COLUMN IF EXISTS billing_mode,
  DROP COLUMN IF EXISTS price,
  DROP COLUMN IF EXISTS cpa_event,
  DROP COLUMN IF EXISTS weight,
  DROP COLUMN IF EXISTS start_at,
  DROP COLUMN IF EXISTS end_at;

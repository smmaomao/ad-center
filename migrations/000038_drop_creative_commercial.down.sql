-- 回滚：恢复素材层的商业与策略列（类型对齐原迁移 000020 / 000005）。
ALTER TABLE ads_center.creatives
  ADD COLUMN IF NOT EXISTS billing_mode TEXT NOT NULL DEFAULT 'cpm'
      CHECK (billing_mode IN ('cpm', 'cpc', 'cpa')),
  ADD COLUMN IF NOT EXISTS price        NUMERIC(12, 4) NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS cpa_event    TEXT,
  ADD COLUMN IF NOT EXISTS weight       NUMERIC(8, 4) NOT NULL DEFAULT 1.0,
  ADD COLUMN IF NOT EXISTS start_at     TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS end_at       TIMESTAMPTZ;

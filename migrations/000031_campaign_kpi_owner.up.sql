-- 让广告任务（Campaign）成为 KPI / 预算的真正持有者（执行粒度）。
-- 原模型 KPI 全在 advertisers 上、决策引擎也按广告主粒度跑；按既定
-- advertiser → product → campaign 分层，把运行期 KPI 下放到 campaign：
--   actual_cpi / spent_today / consume_speed / guaranteed_* / priority_score
-- advertisers 上的同名字段保留为「账户级默认 / 总预算闸」，引擎仍按其做总预算控制。
ALTER TABLE ads_center.campaigns
    ADD COLUMN IF NOT EXISTS actual_cpi            NUMERIC(12, 6) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS spent_today           NUMERIC(12, 2) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS consume_speed         TEXT NOT NULL DEFAULT 'even'
        CHECK (consume_speed IN ('even', 'accelerated', 'asap')),
    ADD COLUMN IF NOT EXISTS guaranteed_enabled    BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS guaranteed_min_share NUMERIC(5, 4) NOT NULL DEFAULT 0
        CHECK (guaranteed_min_share BETWEEN 0 AND 1),
    ADD COLUMN IF NOT EXISTS priority_score        NUMERIC(12, 6) NOT NULL DEFAULT 0;

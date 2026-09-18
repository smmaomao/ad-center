-- 000050: 收口广告任务表单字段
--
--   1) advertisers.freq_windows 已废弃：广告主级频控窗口从未参与频控计算
--      （任务级频控在 campaign 维度，见 engine.CampaignFreqWindows），且前端表单
--      也无对应编辑项。直接删除该列。
--   2) campaigns.consume_speed 由「消耗节奏」枚举（even/accelerated/asap）改为整数
--      「曝光系数」（1-10，默认 5），影响下发节奏。历史枚举值按相近档位回落到整数。
ALTER TABLE ads_center.advertisers DROP COLUMN IF EXISTS freq_windows;

ALTER TABLE ads_center.campaigns
    DROP CONSTRAINT IF EXISTS campaigns_consume_speed_check;

ALTER TABLE ads_center.campaigns
    ALTER COLUMN consume_speed DROP DEFAULT;

UPDATE ads_center.campaigns
    SET consume_speed = CASE consume_speed::text
        WHEN 'asap' THEN '10'
        WHEN 'accelerated' THEN '8'
        ELSE '5'
    END;

ALTER TABLE ads_center.campaigns
    ALTER COLUMN consume_speed TYPE integer
    USING (consume_speed::integer);

ALTER TABLE ads_center.campaigns
    ALTER COLUMN consume_speed SET DEFAULT 5,
    ADD CONSTRAINT campaigns_consume_speed_check CHECK (consume_speed >= 1 AND consume_speed <= 10);

-- 旧的计费方式枚举只有 cpm/cpc/cpa；"cpa" 此前是「按任一转化事件扣费」，
-- 现拆为具体 CPA 子类型，统一把历史 'cpa' 回落到 CPA-激活，保证旧数据仍可编辑。
-- billing_mode 检查约束需先放开以接纳新的 'cpa-activate' 取值。
ALTER TABLE ads_center.campaigns
    DROP CONSTRAINT IF EXISTS campaigns_billing_mode_check,
    ADD CONSTRAINT campaigns_billing_mode_check CHECK (
        billing_mode = ANY (ARRAY['cpm'::text, 'cpc'::text, 'cpa'::text, 'cpa-activate'::text])
    );

UPDATE ads_center.campaigns SET billing_mode = 'cpa-activate' WHERE billing_mode = 'cpa';

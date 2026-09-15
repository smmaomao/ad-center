-- V1.4：应用管理增强 + 素材承载商业与投放策略
--
-- 产品决策：
--  1. 应用（App）需要配置独立的业务后端 S2S 回调地址与签名密钥。
--  2. 单价 / 扣费方式 / 优先级系数属于「投放策略与计费结算核心」，应跟着**素材**走——
--     不同的广告给的钱不一样（单价、扣费方式），竞争展现机会的权重也不一样（优先级系数），
--     因此不再只挂在广告主上。
--  3. 逐步去掉 slot（广告位）概念：素材直接声明「支持哪些样式」和「投放到哪些 App」。

-- ========== 1. 应用：业务回调 + S2S 密钥 ==========
ALTER TABLE ads_center.apps
    ADD COLUMN IF NOT EXISTS callback_url TEXT;
ALTER TABLE ads_center.apps
    ADD COLUMN IF NOT EXISTS secret_key TEXT NOT NULL
        -- 用内置 gen_random_uuid()（PG13+ 自带）：pgcrypto 的 gen_random_bytes
        -- 在本实例的 search_path 下不可见，插入时会直接报错
        DEFAULT replace(gen_random_uuid()::text || gen_random_uuid()::text, '-', '');

COMMENT ON COLUMN ads_center.apps.callback_url IS
    '该 App 业务后端接收广告系统回调的地址（如 https://xiuxian.com），S2S 事件回传目标';
COMMENT ON COLUMN ads_center.apps.secret_key IS
    'S2S 回调签名密钥，系统自动生成，支持复制与重置';

-- ========== 2. 素材：展现样式 / 投放 App / 尺寸变体 / 上下架 ==========
ALTER TABLE ads_center.creatives
    ADD COLUMN IF NOT EXISTS styles      TEXT[] NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS target_apps UUID[] NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS variants    JSONB  NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS start_at    TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS end_at      TIMESTAMPTZ;

COMMENT ON COLUMN ads_center.creatives.styles IS
    '支持的展现样式（多选）：splash 开屏 / rewarded_video 激励视频 / interstitial 插屏 / feed 信息流 / banner';
COMMENT ON COLUMN ads_center.creatives.target_apps IS
    '投放目标 App（多选）；空数组 = 投放到全部 App';
COMMENT ON COLUMN ads_center.creatives.variants IS
    '同一素材的多个尺寸版本（横屏/竖屏）：[{orientation, storage_path, width, height, file_size_bytes}]';
COMMENT ON COLUMN ads_center.creatives.start_at IS '上架时间（为空 = 立即生效）';
COMMENT ON COLUMN ads_center.creatives.end_at IS '下架时间（为空 = 长期有效）';

-- ========== 3. 素材：商业与策略字段 ==========
ALTER TABLE ads_center.creatives
    ADD COLUMN IF NOT EXISTS billing_mode TEXT NOT NULL DEFAULT 'cpm'
        CHECK (billing_mode IN ('cpm', 'cpc', 'cpa')),
    ADD COLUMN IF NOT EXISTS price        NUMERIC(12, 4) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cpa_event    TEXT;

COMMENT ON COLUMN ads_center.creatives.billing_mode IS
    '扣费方式：cpm 千次曝光 / cpc 点击 / cpa 行为（按 cpa_event 指定事件）';
COMMENT ON COLUMN ads_center.creatives.price IS
    '广告单价（元）：cpm=千次曝光价，cpc=单次点击价，cpa=单个转化事件价';
COMMENT ON COLUMN ads_center.creatives.cpa_event IS
    'billing_mode=cpa 时的计费事件（单选）：install 安装 / activate 激活 / register 注册 / first_purchase 首充';

-- 优先级系数：复用既有的 weight（原先用于素材加权随机），收敛到 0.1~5.0
UPDATE ads_center.creatives SET weight = 1.0 WHERE weight < 0.1 OR weight > 5.0;
ALTER TABLE ads_center.creatives
    DROP CONSTRAINT IF EXISTS chk_creatives_weight_range;
ALTER TABLE ads_center.creatives
    ADD CONSTRAINT chk_creatives_weight_range CHECK (weight BETWEEN 0.1 AND 5.0);
COMMENT ON COLUMN ads_center.creatives.weight IS
    '优先级系数（0.1~5.0，默认 1.0）：人工干预竞价曝光权重，越大越容易胜出';

CREATE INDEX IF NOT EXISTS idx_creatives_styles ON ads_center.creatives USING GIN (styles);
CREATE INDEX IF NOT EXISTS idx_creatives_target_apps ON ads_center.creatives USING GIN (target_apps);

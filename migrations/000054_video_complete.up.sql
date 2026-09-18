-- 激励视频完播事件（video-complete）落库 + S2S 转发结果记录。
--
-- 1) ad_events 新增 'video_complete' 事件类型：之前 /v1/ad/video-complete 只做
--    S2S 回调、不落库，导致 ad-center 侧无法统计完播率、也无法对账"是否成功通知
--    业务后端发奖"。现在补一条与 impression 同款的事件明细。
-- 2) 新增 callback_ok 列：记录该完播事件是否成功转发到 App 的业务后端回调地址
--    （true=HTTP 2xx；false=网络错误/非 2xx/未配置 callback_url）。业务后端挂了或
--    callback_url 没配时，此列为 false，便于对账"我们发了回调 vs 业务后端发了奖"。

ALTER TABLE ads_center.ad_events ADD COLUMN IF NOT EXISTS callback_ok BOOLEAN;

ALTER TABLE ads_center.ad_events
    DROP CONSTRAINT IF EXISTS ad_events_event_type_check,
    ADD CONSTRAINT ad_events_event_type_check CHECK (event_type IN
        ('request', 'fill', 'impression', 'click', 'conversion',
         'install', 'activate', 'register', 'first_purchase', 'purchase',
         'video_complete'));

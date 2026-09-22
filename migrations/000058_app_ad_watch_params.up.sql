-- 应用管理新增字段 ad_watch_params：激励视频完播回传给 App 业务后端时的附加参数。
-- /v1/ad/video-complete 触发 reward 回调时，除后台注入的 app_id / user_id 外，
-- 额外把本字段（JSON 对象）里的键值合并进回调请求体，供业务后端按需取用
-- （如 count / ad_network / ad_placement）。为空则仅发送 app_id + user_id。
ALTER TABLE ads_center.apps
    ADD COLUMN IF NOT EXISTS ad_watch_params TEXT NOT NULL DEFAULT '';

COMMENT ON COLUMN ads_center.apps.ad_watch_params IS
    '激励视频完播回传 App 业务后端的附加参数（JSON 对象），与后台注入的 app_id / user_id 合并发送；为空则只发 app_id + user_id';

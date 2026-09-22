-- 应用管理新增字段 add_server_id：app 服务端侧的应用 id。
-- /v1/ad/video-complete 转发 reward 回调给 app 业务后端时，统一以 "app_id" 键
-- 发送本字段的值（未配置则回退到本系统 app_code）。与 ad_watch_params（扩展参数）解耦。
ALTER TABLE ads_center.apps
    ADD COLUMN IF NOT EXISTS add_server_id TEXT NOT NULL DEFAULT '';

COMMENT ON COLUMN ads_center.apps.add_server_id IS
    'app 服务端侧的应用 id；reward 回调转发时作为 "app_id" 发送（未配置则回退 app_code）';

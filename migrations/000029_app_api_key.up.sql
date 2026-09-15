-- 应用 API Key 明文列：用于后台常驻展示脱敏前缀（adc_xxxx***）+ 复制完整 Key，
-- 服务于「多人共管后台，密钥需随时可查」的场景。
-- 鉴权仍走 api_key_hash（sha256），本列仅作展示/交付用途，与 S2S secret_key 处理一致。
ALTER TABLE ads_center.apps ADD COLUMN IF NOT EXISTS api_key TEXT;

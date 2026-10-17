-- S2S 转化幂等：同一 (click_id, event_type) 只扣一次费，防止归因平台 HTTP 超时重试
-- 或任何人拿到 URL 里的 clickid 重放，导致同一笔转化被重复扣款。
--
-- 唯一约束选 (click_id, event_type) 而非 click_id 单列：一次点击可对应 install /
-- activate / first_purchase / purchase 等多个不同事件，它们必须都能各自记一次；但
-- 同一事件（如同一 install、同一 purchase）的重放必须被拦下。click_id 来自投放时
-- 生成的点击登记，重试回传的是同一个 clickid，所以它能精确标识"同一笔转化"。
--
-- 先清理历史可能的重复行（若有），再建约束；整段用 DO 块包裹，可重复执行（迁移
-- 幂等：已存在约束则跳过）。

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'uniq_click_event'
  ) THEN
    WITH dups AS (
      SELECT id,
             ROW_NUMBER() OVER (PARTITION BY click_id, event_type ORDER BY id) rn
      FROM ads_center.ad_conversions
    )
    DELETE FROM ads_center.ad_conversions
     WHERE id IN (SELECT id FROM dups WHERE rn > 1);

    ALTER TABLE ads_center.ad_conversions
      ADD CONSTRAINT uniq_click_event UNIQUE (click_id, event_type);
  END IF;
END $$;

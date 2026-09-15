ALTER TABLE ads_center.advertisers
  ADD COLUMN daily_budget numeric(12,2) NOT NULL DEFAULT 0,
  ADD COLUMN spent_today numeric(12,2) NOT NULL DEFAULT 0;

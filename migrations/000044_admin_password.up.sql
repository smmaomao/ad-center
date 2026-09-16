-- 后台用户改由后端自管密码（不再依赖 Supabase Auth）。
-- 原 current_admin_role / current_admin_menus RPC 仍保留在库里，但前台已不再调用
-- （改由 Go 按邮箱查询，见 internal/api/auth.go 与 internal/store/admin_auth.go）。
ALTER TABLE ads_center.admin_users ADD COLUMN password_hash text;

-- 给本地种子管理员预设默认口令 admin123456（仅当该邮箱存在且尚未设密码时）。
-- 生产环境通过「重新导入数据」带入真实 password_hash，此 UPDATE 不会覆盖既有值。
-- 哈希格式 sha256$<saltHex>$<hashHex>，由 internal/store 的 HashPassword 生成。
UPDATE ads_center.admin_users
SET password_hash = 'sha256$f5451ba186993fbf236324e87f060da3$cbbf944d04cfde6a02044d98d42531e24065bc157dbf30561429198521fff504'
WHERE lower(email) = 'dev@adcenter.local' AND password_hash IS NULL;

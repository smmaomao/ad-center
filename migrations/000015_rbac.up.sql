-- V1.3：后台 RBAC 落地库表（角色 / 菜单 / 授权）
--
-- 背景：此前 RBAC 完全硬编码在前端 web/lib/auth.ts 的 ROLE_ROUTES（角色 → 路由前缀），
-- 新增角色、调整菜单、控制谁能看哪个页面都要改代码发版。本迁移把「角色 / 菜单
-- / 授权」落到库里，为后台管理页面提供数据基础。
--
-- 设计要点：
--  1. roles.code / menus.code 用业务码做主键（与前端 Role 类型、NAV_ITEMS 对齐），避免
--     改名字导致授权关系断裂。
--  2. menus 自关联 parent_code 支持一级分组（「系统设置」下挂用户/角色/菜单）。
--  3. role_menus 是角色与菜单的多对多授权表；页面级鉴权最终仍由前端按角色取菜单校验。
--  4. is_system 标记内置角色，禁止在界面上删除（避免把超级管理员删掉导致后台不可用）。
--  5. 预置数据与现有硬编码逻辑完全等价，迁移后行为不变，可平滑切换。

CREATE TABLE ads_center.roles (
    code        TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    is_system   BOOLEAN NOT NULL DEFAULT false, -- 内置角色：不允许删除
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE ads_center.menus (
    code        TEXT PRIMARY KEY,
    label       TEXT NOT NULL,
    href        TEXT,                           -- 叶子菜单路由；分组（父级）为 NULL
    parent_code TEXT REFERENCES ads_center.menus (code) ON DELETE CASCADE,
    sort_order  INT NOT NULL DEFAULT 0,
    enabled     BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_menus_parent ON ads_center.menus (parent_code, sort_order);

CREATE TABLE ads_center.role_menus (
    role_code TEXT NOT NULL REFERENCES ads_center.roles (code) ON DELETE CASCADE,
    menu_code TEXT NOT NULL REFERENCES ads_center.menus (code) ON DELETE CASCADE,
    PRIMARY KEY (role_code, menu_code)
);

-- 用户状态：disabled 的用户不可登录后台
ALTER TABLE ads_center.admin_users
    ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'disabled'));

-- 预置角色（（与 web/lib/auth.ts 的 ROLE_LABELS 对齐）
INSERT INTO ads_center.roles (code, name, description, is_system)
VALUES ('super_admin', '超级管理员', '全部权限，含用户与角色管理', true),
       ('operator', '广告运营', '广告主 / 广告位 / 素材等日常运营', true),
       ('analyst', '数据分析', '只看数据与 AI Agent 控制台', true),
       ('strategy', '产品/策略', '看数据与策略配置', true)
ON CONFLICT (code) DO NOTHING;

-- 预置菜单：5 个顶级 + 3 个挂到 settings 下的子菜单（系统设置 → 用户/角色/菜单）
--（「系统管理」空壳分组已废，见 000018 迁移；子菜单的 parent 直接是 settings）
INSERT INTO ads_center.menus (code, label, href, sort_order)
VALUES ('dashboard', '实时监控', '/dashboard', 10),
       ('advertisers', '广告主管理', '/advertisers', 20),
       ('slots', '广告位管理', '/slots', 30),
       ('ai-agent', 'AI Agent 控制台', '/ai-agent', 40),
       ('settings', '系统设置', '/settings', 50)
ON CONFLICT (code) DO NOTHING;

INSERT INTO ads_center.menus (code, label, href, parent_code, sort_order)
VALUES ('system.users', '用户管理', '/settings/users', 'settings', 10),
       ('system.roles', '角色管理', '/settings/roles', 'settings', 20),
       ('system.menus', '菜单管理', '/settings/menus', 'settings', 30)
ON CONFLICT (code) DO NOTHING;

-- 预置授权（与 ROLE_ROUTES 等价）：
-- 运营/超管看全部业务菜单；系统设置下的用户/角色/菜单仅超管可进（页面也是
-- requireSuperAdmin 拦截，两处保持一致，避免出现"看得见但点进去被踢"）
INSERT INTO ads_center.role_menus (role_code, menu_code)
SELECT r.code, m.code
FROM ads_center.roles r
         CROSS JOIN ads_center.menus m
WHERE r.code IN ('super_admin', 'operator')
  AND m.code NOT IN ('system.users', 'system.roles', 'system.menus')
ON CONFLICT DO NOTHING;

INSERT INTO ads_center.role_menus (role_code, menu_code)
SELECT 'super_admin', m.code
FROM ads_center.menus m
WHERE m.code IN ('system.users', 'system.roles', 'system.menus')
ON CONFLICT DO NOTHING;

CREATE TRIGGER trg_roles_touch
    BEFORE UPDATE ON ads_center.roles
    FOR EACH ROW EXECUTE FUNCTION ads_center.touch_updated_at();
CREATE TRIGGER trg_menus_touch
    BEFORE UPDATE ON ads_center.menus
    FOR EACH ROW EXECUTE FUNCTION ads_center.touch_updated_at();
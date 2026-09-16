-- 移除「全局频控配置」菜单及其角色授权：任务级频控已统一承接频控职责
--（config.CampaignFreqWindows，按真实观看 impression 计数），全局疲劳度层下线。
DELETE FROM ads_center.role_menus WHERE menu_code = 'fatigue';
DELETE FROM ads_center.menus WHERE code = 'fatigue';

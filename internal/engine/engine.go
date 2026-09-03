// Package engine 是广告决策引擎（PRD 5.1），纯内存计算、无 IO：
//
//	筛选活跃广告主 → KPI 分档 → 优先级得分 → 保底份额 → 排序 → 疲劳过滤 → 兜底链
//
// 依赖（ConfigCache / BudgetCtrl / FrequencyStore）通过接口注入，
// 保证引擎可单测、可基准测试（阶段 1.9：引擎耗时 <10ms）。
package engine

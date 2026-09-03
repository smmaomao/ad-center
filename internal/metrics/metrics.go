// Package metrics 提供实时指标聚合：内存计数器（slot×advertiser×分钟粒度），
// SSE 推送给监控看板（延迟 <3s），每分钟批量落库 metrics_minute。
package metrics

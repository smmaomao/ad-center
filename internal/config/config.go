// Package config 提供配置缓存：启动全量加载广告主/广告位/素材到内存，
// 通过 Postgres LISTEN/NOTIFY 秒级热更新，60s 定时对账防通知丢失。
package config

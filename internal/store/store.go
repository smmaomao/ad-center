// Package store 是 Postgres 访问层（sqlc 生成 + migration），
// 仅服务启动、配置加载、定时落库等非决策路径；决策路径全程内存。
package store

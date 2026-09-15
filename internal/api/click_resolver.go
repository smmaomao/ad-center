package api

import (
	"context"

	"adcenter/internal/store"
)

// StoreResolver 是 ClickResolver 的 DB 实现：点击登记落库（store.RegisterClick）
// 并在 S2S 转化回调时通过 store.ResolveClick 反查，转换为 api.ClickContext 用于归属。
type StoreResolver struct{ st *store.Store }

// NewClickResolver 用 store 构造点击反查器，注入 Server.Clicks。
func NewClickResolver(st *store.Store) ClickResolver { return &StoreResolver{st: st} }

// Resolve 实现 ClickResolver。接口无 ctx，内部以 background 承载（S2S 回调路径，
// 不参与请求链路透传）。
func (r *StoreResolver) Resolve(clickID string) (*ClickContext, bool) {
	rec, ok, err := r.st.ResolveClick(context.Background(), clickID)
	if err != nil {
		return nil, false
	}
	if !ok {
		return nil, false
	}
	return &ClickContext{
		AppID: rec.AppID, AdvertiserID: rec.AdvertiserID,
		Style: rec.Style, DeviceID: rec.DeviceID, CreativeID: rec.CreativeID,
	}, true
}

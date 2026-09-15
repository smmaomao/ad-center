package api

// ============================================================
// Swagger 文档模型（仅供 swag 生成 docs 使用；运行时响应仍以 handler 内的 map 返回）。
// 字段名/示例与 docs/客户端接口.md 保持一致。
// ============================================================

// ClientSimpleResponse 通用成功响应（仅 code / msg）。
type ClientSimpleResponse struct {
	Code int    `json:"code" example:"200"`
	Msg  string `json:"msg" example:"success"`
}

// AdListRequest 批量获取广告请求。
type AdListRequest struct {
	AdAppID    string `json:"ad_app_id" example:"app_10001"`
	Count      int    `json:"count" example:"5"`
	UserID     string `json:"user_id" example:"user_abc_12345"`
	AdjustAdid string `json:"adjust_adid" example:"adj_xyz_9988"`
	OS         string `json:"os" example:"ios"`
	IP         string `json:"ip" example:"192.168.1.100"`
}

// AdListItem 单条广告素材（ad_list 数组元素）。
type AdListItem struct {
	BidID            string   `json:"bid_id" example:"bid_20260908_0001"`
	CreativeID       string   `json:"creative_id" example:"cr_90002"`
	AdStyle          string   `json:"ad_style" example:"REWARDED_VIDEO"`
	MaterialType     string   `json:"material_type" example:"VIDEO"`
	MaterialURL      string   `json:"material_url" example:"https://cdn.example.com/creatives/abc123.mp4"`
	Width            int      `json:"width" example:"1080"`
	Height           int      `json:"height" example:"1920"`
	TargetScene      []string `json:"target_scene" example:"APP_LAUNCH"`
	RequiredDuration int      `json:"required_duration" example:"15"`
	ClickURL         string   `json:"click_url" example:"https://landing.com?clk={CLICK_ID}"`
}

// AdListData ad_list 包装。
type AdListData struct {
	// Loopable 整个广告列表是否允许在客户端缓存期内循环播放。
	// 当且仅当列表内所有素材均可循环（视频）时为 true，任一图片素材出现则为 false。
	Loopable bool         `json:"loopable" example:"true"`
	AdList   []AdListItem `json:"ad_list"`
}

// AdListResponse 批量获取广告响应。
type AdListResponse struct {
	Code int        `json:"code" example:"200"`
	Msg  string     `json:"msg" example:"success"`
	Data AdListData `json:"data"`
}

// AdImpressionRequest 曝光埋点请求。
type AdImpressionRequest struct {
	BidID      string `json:"bid_id" example:"bid_20260908_0001"`
	AdAppID    string `json:"ad_app_id" example:"app_10001"`
	CreativeID string `json:"creative_id" example:"cr_90002"`
	Timestamp  int64  `json:"timestamp" example:"1774213460"`
}

// AdClickRequest 点击埋点请求。
type AdClickRequest struct {
	BidID      string `json:"bid_id" example:"bid_20260908_0001"`
	AdAppID    string `json:"ad_app_id" example:"app_10001"`
	CreativeID string `json:"creative_id" example:"cr_90002"`
	UserID     string `json:"user_id" example:"user_abc_12345"`
	AdjustAdid string `json:"adjust_adid" example:"adj_xyz_9988"`
	Timestamp  int64  `json:"timestamp" example:"1774213465"`
}

// AdClickData click_id / jump_url 包装。
type AdClickData struct {
	ClickID string `json:"click_id" example:"clk_7788992233"`
	JumpURL string `json:"jump_url" example:"https://landing.com?clk=clk_7788992233"`
}

// AdClickResponse 点击埋点响应。
type AdClickResponse struct {
	Code int         `json:"code" example:"200"`
	Msg  string      `json:"msg" example:"success"`
	Data AdClickData `json:"data"`
}

// AdVideoCompleteRequest 视频播放完毕请求。
type AdVideoCompleteRequest struct {
	BidID      string `json:"bid_id" example:"bid_20260908_0001"`
	AdAppID    string `json:"ad_app_id" example:"app_10001"`
	CreativeID string `json:"creative_id" example:"cr_90002"`
	UserID     string `json:"user_id" example:"user_abc_12345"`
	Timestamp  int64  `json:"timestamp" example:"1774213495"`
}

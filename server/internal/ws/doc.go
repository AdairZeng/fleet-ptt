// Package ws 提供 H5 信令 WebSocket。
//
// 客户端连接 GET /api/v1/device/ws，用查询参数 token 或 Authorization: Bearer 携带 session_token。
// 服务端向频道内在线连接推送 talk_start、talk_end、members、channel_created、channel_switched。
package ws

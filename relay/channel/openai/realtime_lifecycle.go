package openai

import (
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/gorilla/websocket"
)

// 与 Responses WebSocket 使用相同的请求及上游消息限制来源。
func configureRealtimeConnections(client, target *websocket.Conn) {
	maxMB := constant.MaxRequestBodyMB
	if maxMB <= 0 {
		maxMB = 128
	}
	client.SetReadLimit(int64(maxMB) << 20)
	limit := int64(helper.DefaultMaxScannerBufferSize)
	if constant.StreamScannerMaxBufferMB > 0 {
		limit = int64(constant.StreamScannerMaxBufferMB) << 20
	}
	target.SetReadLimit(limit)
}

func writeRealtimeMessage(conn *websocket.Conn, message []byte) error {
	if err := conn.SetWriteDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, message)
}

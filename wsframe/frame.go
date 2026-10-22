// Package wsframe 是抖音 webcast 弹幕长连接的**帧层**：只做 PushFrame 的编解码、
// payload 解压、心跳/ack 帧构造与握手参数解析，不持有连接、不做重连、不碰业务。
//
// 为什么把它单独拎出来：这些形状（payload 实际总是 gzip、心跳原样回 ack、
// seq 要 +1、客户端心跳固定 10000/2/x-webcast-douyin）是靠实测对出来的，
// 每个接弹幕的项目都会重新踩一遍。放在这里，调用方只需要管"什么时候发、发完怎么处理"。
package wsframe

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	pb "github.com/athlon18/douyin-protocol/webcast"
	"google.golang.org/protobuf/proto"
)

// PushFrame.payload_type 的三个取值（实测口径）。
const (
	// PayloadTypeHeartbeat 服务端下发的心跳帧：必须原样回 ack，否则连接会被断开。
	PayloadTypeHeartbeat = "hb"
	// PayloadTypeAck 确认帧（双向）。
	PayloadTypeAck = "ack"
	// PayloadTypeClientHeartbeat 客户端主动心跳的 payload_type，取现网在用值。
	PayloadTypeClientHeartbeat = "x-webcast-douyin"
)

// ClientHeartbeatSeqID 客户端主动心跳的固定 seq，取现网在用值。
const ClientHeartbeatSeqID uint64 = 10000

// Decode 把一条 WebSocket 二进制消息解成最外层 PushFrame。
func Decode(data []byte) (*pb.PushFrame, error) {
	frame := new(pb.PushFrame)
	if err := proto.Unmarshal(data, frame); err != nil {
		return nil, fmt.Errorf("解析 PushFrame 失败: %w", err)
	}
	return frame, nil
}

// Unpack 取出帧里真正承载 Response 的字节。
//
// 抖音 PushFrame 的 payload 实际始终为 gzip，但 payload_encoding 字段值并不稳定，
// 所以这里**无条件尝试 gunzip**，失败就按原始字节返回（而不是报错丢帧）。
func Unpack(frame *pb.PushFrame) []byte {
	payload := frame.GetPayload()
	if len(payload) == 0 {
		return nil
	}
	if out, err := gunzip(payload); err == nil {
		return out
	}
	return payload
}

// DecodeResponse 把 Unpack 出来的字节解成 Response。
func DecodeResponse(payload []byte) (*pb.Response, error) {
	resp := new(pb.Response)
	if err := proto.Unmarshal(payload, resp); err != nil {
		return nil, fmt.Errorf("解析 Response 失败: %w", err)
	}
	return resp, nil
}

// Heartbeat 构造一帧客户端主动心跳。
func Heartbeat() ([]byte, error) {
	return proto.Marshal(&pb.PushFrame{
		SeqId:       ClientHeartbeatSeqID,
		Service:     2,
		PayloadType: PayloadTypeClientHeartbeat,
	})
}

// Ack 构造一帧确认。payload 原样带回：服务端认的是那份 internal_ext，不是空串。
func Ack(seq uint64, payload []byte) ([]byte, error) {
	return proto.Marshal(&pb.PushFrame{
		SeqId:       seq,
		PayloadType: PayloadTypeAck,
		Payload:     payload,
	})
}

// HeartbeatReply 给"收到 hb 帧"的调用方算回包三要素：seq 必须 +1，payload 原样带回。
// 拆成纯函数而不是直接写连接：写连接要锁、要判代次，那是调用方的状态，不属于帧层。
func HeartbeatReply(frame *pb.PushFrame) (seq uint64, payload []byte) {
	return frame.GetSeqId() + 1, frame.GetPayload()
}

// ParsePingInterval 从握手响应头 Handshake-Options 里取 ping-interval 秒数。
// 形如 "regx=0; ping-interval=15;"；拿不到（或值非法）就返回 false，
// 由调用方退回自己的默认周期。
func ParsePingInterval(header string) (time.Duration, bool) {
	for _, part := range strings.Split(header, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 || !strings.EqualFold(strings.TrimSpace(kv[0]), "ping-interval") {
			continue
		}
		sec, err := strconv.Atoi(strings.TrimSpace(kv[1]))
		if err != nil || sec <= 0 {
			return 0, false
		}
		return time.Duration(sec) * time.Second, true
	}
	return 0, false
}

func gunzip(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var out bytes.Buffer
	if _, err := io.Copy(&out, r); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

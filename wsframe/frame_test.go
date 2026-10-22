package wsframe

import (
	"bytes"
	"compress/gzip"
	"testing"
	"time"

	pb "github.com/athlon18/douyin-protocol/webcast"
	"google.golang.org/protobuf/proto"
)

// TestUnpackGzipAndFallback 钉住两条实测口径：payload 是 gzip 时要解开；
// 不是 gzip 时必须原样返回而不是丢帧（encoding 字段不可靠）。
func TestUnpackGzipAndFallback(t *testing.T) {
	raw := []byte(`{"messages":[{"method":"WebcastChatMessage"}]}`)

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		t.Fatalf("构造 gzip 失败：%v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("关闭 gzip 失败：%v", err)
	}

	got := Unpack(&pb.PushFrame{Payload: buf.Bytes()})
	if !bytes.Equal(got, raw) {
		t.Errorf("gzip payload 没被解开：%q", got)
	}

	plain := []byte("not-gzip-at-all")
	if got := Unpack(&pb.PushFrame{Payload: plain}); !bytes.Equal(got, plain) {
		t.Errorf("非 gzip payload 应原样返回，实际 %q", got)
	}

	if got := Unpack(&pb.PushFrame{}); got != nil {
		t.Errorf("空 payload 应返回 nil，实际 %q", got)
	}
}

// TestDecodeResponseRoundTrip 确保帧层与 pb 定义对得上：Unpack 出来的字节能解成 Response。
func TestDecodeResponseRoundTrip(t *testing.T) {
	want := &pb.Response{NeedAck: true, InternalExt: "ext-1", HeartbeatDuration: 15}
	body, err := proto.Marshal(want)
	if err != nil {
		t.Fatalf("marshal Response 失败：%v", err)
	}
	got, err := DecodeResponse(body)
	if err != nil {
		t.Fatalf("DecodeResponse 失败：%v", err)
	}
	if !got.GetNeedAck() || got.GetInternalExt() != "ext-1" || got.GetHeartbeatDuration() != 15 {
		t.Errorf("Response 往返后字段不一致：%+v", got)
	}
}

// TestHeartbeatAndAckRoundTrip 钉住客户端心跳的固定形状与 ack 的 seq/payload 透传。
func TestHeartbeatAndAckRoundTrip(t *testing.T) {
	b, err := Heartbeat()
	if err != nil {
		t.Fatalf("构造心跳失败：%v", err)
	}
	frame, err := Decode(b)
	if err != nil {
		t.Fatalf("解心跳帧失败：%v", err)
	}
	if frame.GetSeqId() != ClientHeartbeatSeqID {
		t.Errorf("心跳 seq = %d，应为 %d", frame.GetSeqId(), ClientHeartbeatSeqID)
	}
	if frame.GetService() != 2 || frame.GetPayloadType() != PayloadTypeClientHeartbeat {
		t.Errorf("心跳帧形状变了：service=%d type=%q", frame.GetService(), frame.GetPayloadType())
	}

	ackBytes, err := Ack(7, []byte("ext"))
	if err != nil {
		t.Fatalf("构造 ack 失败：%v", err)
	}
	ack, err := Decode(ackBytes)
	if err != nil {
		t.Fatalf("解 ack 失败：%v", err)
	}
	if ack.GetSeqId() != 7 || ack.GetPayloadType() != PayloadTypeAck || string(ack.GetPayload()) != "ext" {
		t.Errorf("ack 没有原样透传：seq=%d type=%q payload=%q",
			ack.GetSeqId(), ack.GetPayloadType(), ack.GetPayload())
	}
}

// TestHeartbeatReplySeqPlusOne 服务端心跳要回 seq+1，payload 原样带回。
func TestHeartbeatReplySeqPlusOne(t *testing.T) {
	seq, payload := HeartbeatReply(&pb.PushFrame{SeqId: 41, Payload: []byte("hb-body")})
	if seq != 42 {
		t.Errorf("回包 seq = %d，应为 42（原 seq + 1）", seq)
	}
	if string(payload) != "hb-body" {
		t.Errorf("回包 payload = %q，应原样带回", payload)
	}
}

func TestDecodeRejectsGarbage(t *testing.T) {
	if _, err := Decode([]byte{0xff, 0xff, 0xff, 0xff}); err == nil {
		t.Error("坏字节解出了 PushFrame，错误被吞了")
	}
	if _, err := DecodeResponse([]byte{0xff, 0xff, 0xff, 0xff}); err == nil {
		t.Error("坏字节解出了 Response，错误被吞了")
	}
}

func TestParsePingInterval(t *testing.T) {
	cases := []struct {
		header string
		want   time.Duration
		ok     bool
	}{
		{"regx=0; ping-interval=15;", 15 * time.Second, true},
		{"Ping-Interval=20", 20 * time.Second, true},
		{"  ping-interval = 12 ; ", 12 * time.Second, true},
		{"regx=0; ping-interval=0;", 0, false},
		{"regx=0; ping-interval=-5;", 0, false},
		{"regx=0; ping-interval=abc;", 0, false},
		{"ping-interval", 0, false},
		{"regx=0;", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		got, ok := ParsePingInterval(c.header)
		if ok != c.ok || got != c.want {
			t.Errorf("ParsePingInterval(%q) = (%v,%v)，应为 (%v,%v)", c.header, got, ok, c.want, c.ok)
		}
	}
}

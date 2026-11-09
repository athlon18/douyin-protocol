package im

import (
	"bytes"
	"testing"
)

// ============================================================
// 协议底座的纯函数门禁（不联网、不连库）。
//
// 它是四条私信链路（收件箱 / 历史 / 发送 / 已读）共用的唯一 wire 实现：
// 编码错一位四处一起错，解码 panic 一次带崩协程。所以这里只钉两条性质：
//   ① 编解码自洽（写进去的读得回来）；
//   ② 外部报文任意截断/畸形都绝不 panic（抖音回什么我们都得接着）。
// ============================================================

func TestEncodeDecodeRoundTrip(t *testing.T) {
	var b []byte
	b = append(b, Varint(1, 300)...)
	b = append(b, Str(2, "会话")...)
	b = append(b, Bytes(3, nil)...)
	b = append(b, Varint(15, 3)...)

	fs := Decode(b)
	if len(fs) != 4 {
		t.Fatalf("字段数 = %d，期望 4", len(fs))
	}
	if f, ok := First(fs, 1); !ok || f.Wt != 0 || f.V != 300 {
		t.Errorf("field1 = %+v，期望 varint 300", f)
	}
	if f, ok := First(fs, 2); !ok || f.Wt != 2 || string(f.B) != "会话" {
		t.Errorf("field2 = %+v，期望 bytes 会话", f)
	}
	if f, ok := First(fs, 3); !ok || f.Wt != 2 || len(f.B) != 0 {
		t.Errorf("field3 = %+v，期望空 bytes", f)
	}
	if all := All(fs, 15); len(all) != 1 || all[0].V != 3 {
		t.Errorf("field15 = %+v，期望唯一 3", all)
	}
	if _, ok := First(fs, 99); ok {
		t.Error("不存在的字段不该被命中")
	}
}

// 截断是最常见的外部形态（网络半包、响应被裁）：任何长度都必须安静返回已解出的部分。
func TestDecodeNeverPanicsOnTruncatedInput(t *testing.T) {
	full := append(Varint(1, 123456789), Str(2, "hello")...)
	full = append(full, Bytes(3, []byte("payload"))...)
	for n := 0; n <= len(full); n++ {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("截到 %d 字节时 panic: %v", n, r)
				}
			}()
			_ = Decode(full[:n])
		}()
	}
}

// 未知 wire type（group 的 3/4 等）按协议约定停在那里，不能猜长度硬切片。
func TestDecodeStopsOnUnknownWireType(t *testing.T) {
	b := append(Varint(1, 7), 0x0b) // field1, wt=3(group start)
	b = append(b, Varint(2, 9)...)
	fs := Decode(b)
	if len(fs) != 1 || fs[0].Num != 1 {
		t.Fatalf("应在未知 wire type 处停下，实际 %+v", fs)
	}
}

// 信封是四条链路的公共壳：cmd1/cmd2/field6/参数体任何一格漂了，抖音会静默回空。
func TestBuildEnvelopeCarriesEndpointAndParam(t *testing.T) {
	param := Bytes(301, Str(1, "0:1:1:2"))
	raw := BuildEnvelope(301, 10436, 0, param)

	fs := Decode(raw)
	if f, ok := First(fs, 1); !ok || f.V != 301 {
		t.Errorf("cmd1 = %+v，期望 301", f)
	}
	if f, ok := First(fs, 2); !ok || f.V != 10436 {
		t.Errorf("cmd2 = %+v，期望 10436", f)
	}
	if f, ok := First(fs, 6); !ok || f.V != 0 {
		t.Errorf("field6 = %+v，期望 0（发送档）", f)
	}
	if f, ok := First(fs, 8); !ok || !bytes.Equal(f.B, param) {
		t.Error("field8 参数体必须原样进信封")
	}
	// 公共上下文是重复 KV，少一组抖音就可能判成残缺客户端
	if kv := All(fs, 15); len(kv) != len(CommonKV) {
		t.Errorf("公共 KV = %d 组，期望 %d 组", len(kv), len(CommonKV))
	}
}

// TestDecodeSurvivesAbsurdLength 畸形长度前缀绝不能 panic。
// 报文来自 WS/HTTP 响应，是外部可控输入：读协程里一个未 recover 的 panic 会带走整个进程，
// 现象就是「私信进程被一个坏包打崩」，而这类崩溃在日志里只会留下一条 stack trace。
func TestDecodeSurvivesAbsurdLength(t *testing.T) {
	cases := map[string][]byte{
		// 字段 1、wt=2，长度前缀写成 uint64 最大值：转成 int 会溢出成负数
		"长度前缀全 1": {0x0A, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x01},
		// 长度声明 500 字节，实际只剩 3 字节
		"长度超过剩余": {0x0A, 0xF4, 0x01, 'a', 'b'},
		"长度后无载荷": {0x0A, 0x05},
	}
	for name, b := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Decode 对畸形长度 panic 了：%v", r)
				}
			}()
			_ = Decode(b)
		})
	}
}

// TestDecodeKeepsFieldsBeforeABadTail 一个坏尾巴不该把前面已经解出来的字段一起吞掉：
// 上层靠 msgid 去重，丢了前面就等于整帧白丢。
func TestDecodeKeepsFieldsBeforeABadTail(t *testing.T) {
	b := append(Varint(1, 99), 0x0A, 0xF4, 0x01, 'a') // 字段1 正常 + 字段2 声明超长
	fs := Decode(b)
	if len(fs) == 0 || fs[0].Num != 1 || fs[0].V != 99 {
		t.Fatalf("前面的正常字段被坏尾巴连带丢了：%+v", fs)
	}
}

// TestDecodeEmptyBytesFieldIsStillAField 空字符串是常态（昵称缺失、文本为空），
// 它必须作为一个字段存在，而不是被当成"没有这个字段"。
func TestDecodeEmptyBytesFieldIsStillAField(t *testing.T) {
	fs := Decode(Bytes(7, nil))
	f, ok := First(fs, 7)
	if !ok {
		t.Fatal("空 bytes 字段被丢了，First 找不到字段 7")
	}
	if len(f.B) != 0 {
		t.Errorf("空字段载荷应为 0 长度，实际 %d", len(f.B))
	}
}

// TestDecodeSkipsFixedFieldsButKeepsGoing fixed32/fixed64 我们不解值（IM 报文不用），
// 但只许跳过、不许停 —— 停在这里会让该字段之后的所有字段（常见就是 msgid/时间戳）静默消失。
func TestDecodeSkipsFixedFieldsButKeepsGoing(t *testing.T) {
	b := []byte{0x09, 1, 2, 3, 4, 5, 6, 7, 8} // 字段 1、wt=1(fixed64) + 8 字节
	b = append(b, Varint(3, 42)...)           // 其后还有一个正常 varint 字段
	fs := Decode(b)
	if _, ok := First(fs, 3); !ok {
		t.Fatalf("fixed64 之后的字段被整帧丢弃了：%+v", fs)
	}
	if f, _ := First(fs, 3); f.V != 42 {
		t.Errorf("后续字段值应完整解出，实际 %+v", f)
	}
	// fixed 字段本身按设计不产出 Field
	for _, f := range fs {
		if f.Num == 1 {
			t.Errorf("fixed64 不该产出一个空值字段：%+v", f)
		}
	}
}

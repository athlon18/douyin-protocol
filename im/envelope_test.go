package im

import "testing"

func TestVarintAndStrBytes(t *testing.T) {
	// field1 varint 3 -> tag (1<<3|0)=0x08, value 3
	if got := Varint(1, 3); len(got) != 2 || got[0] != 0x08 || got[1] != 0x03 {
		t.Errorf("Varint(1,3)=% x, want [08 03]", got)
	}
	// field3 string "0.1.8" -> tag (3<<3|2)=0x1a, len 5, bytes
	got := Str(3, "0.1.8")
	if got[0] != 0x1a || got[1] != 5 || string(got[2:]) != "0.1.8" {
		t.Errorf("Str(3,\"0.1.8\")=% x", got)
	}
}

func TestBuildEnvelopeShape(t *testing.T) {
	b := BuildReq(2048, 10125, Str(1, "x"))
	fs := Decode(b)
	// cmd1=2048 应在 field1
	if f, ok := First(fs, 1); !ok || f.V != 2048 {
		t.Errorf("field1 cmd1 want 2048, got %+v ok=%v", f, ok)
	}
	// 公共上下文里应含 session_aid=6383（field15 包裹 field1/field2）
	if len(All(fs, 15)) < len(CommonKV) {
		t.Errorf("field15 KV count too small: %d", len(All(fs, 15)))
	}
}

func TestIsGroupConvID(t *testing.T) {
	if !IsGroupConvID("7455218347492327955") {
		t.Error("纯数字大 id 应判群聊")
	}
	if IsGroupConvID("private:123:456") {
		t.Error("带冒号应判私聊")
	}
}

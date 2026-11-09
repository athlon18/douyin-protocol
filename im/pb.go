// Package im 抖音私信 imapi.douyin.com 的 protobuf wire 协议底座。
//
// 只做两件事：① 手搓 protobuf wire 编解码（不依赖 protoc）；② 组请求信封 + 发 HTTP。
// 刻意不认识任何业务概念（不 import dao / model / service）：
//   - 会话历史的翻页语义在 imhistory；
//   - 消息归一化成 model.IMMessage 在 imdecode/。
//
// 协议层被四条链路共用：实时收件箱轮询、历史回扫、发送/已读回执、CDP 侦察工具。
// 字段号只在这里改一处，四处同时生效——这是拆包的意义，不是搬家。
package im

import "encoding/binary"

// Field 是 wire 树上的一个字段。标签已知（num/wt）时按需取 V（varint）或 B（bytes）。
//
// 字段故意导出：解码结果要跨包给 imhistory / service 读，若藏成方法会让热路径多一层接口；
// 但**只读**是纪律——解码产物原地被业务读，不允许改。
type Field struct {
	Num int    // 字段号
	Wt  int    // wire type：0=varint 2=bytes 1=fixed64 5=fixed32
	V   uint64 // wt=0 时的值
	B   []byte // wt=2 时的载荷（未拷贝，指向原始报文，别长期持有）
}

// Uvarint 编码一个 base-128 varint。
func Uvarint(v uint64) []byte {
	var b [10]byte
	n := binary.PutUvarint(b[:], v)
	return append([]byte{}, b[:n]...)
}

// Tag 编码字段标签（字段号 << 3 | wire type）。
func Tag(fn, wt int) []byte { return Uvarint(uint64(fn)<<3 | uint64(wt)) }

// Varint 编码一个 varint 字段。
func Varint(fn int, v uint64) []byte { return append(Tag(fn, 0), Uvarint(v)...) }

// Bytes 编码一个 length-delimited 字段。
func Bytes(fn int, b []byte) []byte {
	o := Tag(fn, 2)
	o = append(o, Uvarint(uint64(len(b)))...)
	return append(o, b...)
}

// Str 编码一个字符串字段。
func Str(fn int, s string) []byte { return Bytes(fn, []byte(s)) }

// Decode 把一段 wire 字节解成字段树（只认 varint / length-delimited / fixed32 / fixed64）。
// 遇到未知 wire type 或截断就把已解出的部分返回——报文是外部输入，绝不能 panic。
func Decode(b []byte) []Field {
	var out []Field
	i := 0
	for i < len(b) {
		key, n := binary.Uvarint(b[i:])
		if n <= 0 {
			break
		}
		i += n
		fn := int(key >> 3)
		switch wt := key & 7; wt {
		case 0:
			v, n := binary.Uvarint(b[i:])
			if n <= 0 {
				return out
			}
			i += n
			out = append(out, Field{fn, 0, v, nil})
		case 2:
			ln, n := binary.Uvarint(b[i:])
			if n <= 0 {
				return out
			}
			i += n
			// 剩余字节够不够，必须在 uint64 上比：先转成 int 再比，遇到 0xFF… 这种
			// 恶意/脏数据里的巨大长度前缀会溢出成负数，检查直接失效，下一行切片就 panic。
			if uint64(len(b)-i) < ln {
				return out
			}
			out = append(out, Field{fn, 2, 0, b[i : i+int(ln)]})
			i += int(ln)
		case 5:
			if i+4 > len(b) {
				return out
			}
			i += 4
		case 1:
			if i+8 > len(b) {
				return out
			}
			i += 8
		default:
			return out
		}
	}
	return out
}

// First 取指定字段号的第一个。
func First(fs []Field, num int) (Field, bool) {
	for _, f := range fs {
		if f.Num == num {
			return f, true
		}
	}
	return Field{}, false
}

// All 取指定字段号的全部出现（重复字段）。
func All(fs []Field, num int) []Field {
	var r []Field
	for _, f := range fs {
		if f.Num == num {
			r = append(r, f)
		}
	}
	return r
}

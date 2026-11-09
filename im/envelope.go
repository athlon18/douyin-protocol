package im

import "regexp"

// 抖音私信 imapi.douyin.com 的请求信封。
//
// 全部由 CDP 抓包 + protobuf wire 解码实测逆出（见记忆「抖音私信/粉丝群IM真实端点」）：
//   - 读接口 POST，Content-Type/Accept: application/x-protobuf，仅 Cookie 鉴权，无 a_bogus/msToken。
//   - 请求信封字段：1=cmd码,2=子码,3=sdkver,4=空,5=3,6=1,7=build,8=参数,9="0",11="douyin_pc",
//     14="360000",15=重复KV公共上下文,18=1,21="douyin_web",22="web_sdk"。
//   - get_user_message(cmd 2048/10125) 响应：field6→2048→1→{1=游标, 2=重复Message}；
//     Message：1=str会话id,3=服务端消息id,5=会话数字短号,7=uid,8=内容(JSON含text),10=时间ms。

// UA 私信链路固定身份：这是"我这个账号在操作"，不跟随房间画像漂移
// （见 .qoder/skills/douyin-live/SKILL.md「身份」一节的两个例外）。
const UA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/154.0.0.0 Safari/537.36"

// CommonKV 请求公共上下文（照抄浏览器实发的键；值自填，抖音不强校验具体值，只要结构对）。
var CommonKV = [][2]string{
	{"session_aid", "6383"}, {"session_did", "0"}, {"app_name", "douyin_pc"},
	{"priority_region", "cn"}, {"user_agent", UA}, {"cookie_enabled", "true"},
	{"browser_language", "zh-CN"}, {"browser_platform", "MacIntel"}, {"browser_name", "Mozilla"},
	{"browser_version", "5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/154.0.0.0 Safari/537.36"},
	{"browser_online", "true"}, {"screen_width", "1440"}, {"screen_height", "900"},
	{"referer", "https://www.douyin.com/"}, {"timezone_name", "Asia/Shanghai"}, {"deviceId", "0"}, {"is-retry", "0"},
}

// BuildReq 组装 imapi 读接口请求信封。cmd1/cmd2 是端点码；params 是塞进 field8 的参数体。
func BuildReq(cmd1, cmd2 int, params []byte) []byte {
	return BuildEnvelope(cmd1, cmd2, 1, params)
}

// BuildEnvelope 组 imapi 信封。field6 读接口实测是 1，发送(cmd 100)实测是 0 —— 别照抄。
func BuildEnvelope(cmd1, cmd2 int, f6 uint64, params []byte) []byte {
	var b []byte
	b = append(b, Varint(1, uint64(cmd1))...)
	b = append(b, Varint(2, uint64(cmd2))...)
	b = append(b, Str(3, "0.1.8")...)
	b = append(b, Bytes(4, nil)...)
	b = append(b, Varint(5, 3)...)
	b = append(b, Varint(6, f6)...)
	b = append(b, Str(7, "0d50935:feat/pc-im-group")...)
	b = append(b, Bytes(8, params)...)
	b = append(b, Str(9, "0")...)
	b = append(b, Str(11, "douyin_pc")...)
	b = append(b, Str(14, "360000")...)
	for _, kv := range CommonKV {
		b = append(b, Bytes(15, append(Str(1, kv[0]), Str(2, kv[1])...))...)
	}
	b = append(b, Varint(18, 1)...)
	b = append(b, Str(21, "douyin_web")...)
	b = append(b, Str(22, "web_sdk")...)
	return b
}

// groupConvRe 粉丝团群聊的会话 id 是纯数字大整数（如 7455218347492327955）。
var groupConvRe = regexp.MustCompile(`^\d{6,25}$`)

// IsGroupConvID 按 id 形态猜会话类型：真值优先用报文 field2，这个只在缺字段时兜底。
func IsGroupConvID(convID string) bool { return groupConvRe.MatchString(convID) }

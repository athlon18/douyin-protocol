package webmssdk

// WS 握手 URL 的参数快照测试。
//
// 为什么钉"顺序"而不是只钉"有哪些参数"：这条 URL 是抖音风控直接读的对象。
// 参数重排不会让编译失败、也不会让本地自检报错，但在现网上它的表现是
// "握手被拒 / 接口 200+空体"这类静默失效 —— 本周已经为此查了两轮。
// 要改顺序必须先拿真房间验一次，改完这条测试也会红着提醒你这件事。
//
// 全程不联网：只跑本地 goja + 仓库里的签名材料。
import (
	"net/url"
	"strings"
	"testing"
)

// wantParamOrder 发给抖音的 WS 参数顺序（从 wssUrl 的拼装处抄来并钉住）。
var wantParamOrder = []string{
	"room_id", "compress", "version_code", "webcast_sdk_version", "live_id",
	"did_rule", "user_unique_id", "identity", "signature", "aid",
	"device_platform", "browser_language", "browser_platform",
	"browser_name", "browser_version",
}

const probeUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/132.0.0.0 Safari/537.36"

func mustRun(t *testing.T, platform string) *Webmssdk {
	t.Helper()
	w := New(7000000000000000000)
	if platform != "" {
		w = w.SetBrowserPlatform(platform)
	}
	ws, err := w.SetUa(probeUA).Run("7696538532378184448", "7000000000000000000")
	if err != nil {
		t.Fatalf("生成 WS URL 失败：%v", err)
	}
	if ws.WssUrl == "" {
		t.Fatal("WssUrl 为空")
	}
	return ws
}

func TestWssParamOrderIsFrozen(t *testing.T) {
	ws := mustRun(t, "Win32")
	got := orderedKeys(t, ws.WssUrl)
	if strings.Join(got, ",") != strings.Join(wantParamOrder, ",") {
		t.Errorf("WS 参数顺序与快照不一致。\n  期望：%s\n  实际：%s\n"+
			"顺序参与握手 URL 的字面形状，重排前先拿真房间验一次再改这份快照",
			strings.Join(wantParamOrder, ","), strings.Join(got, ","))
	}
}

// TestWssParamOrderHasNoDuplicates 钉一件事：同一个键出现两次会让"签名串与实发串"分叉。
func TestWssParamOrderHasNoDuplicates(t *testing.T) {
	seen := map[string]int{}
	for _, k := range orderedKeys(t, mustRun(t, "Win32").WssUrl) {
		seen[k]++
	}
	for k, n := range seen {
		if n > 1 {
			t.Errorf("参数 %q 出现了 %d 次（重复键会让抖音读到后一个值，签名却是对整串算的）", k, n)
		}
	}
}

// TestBrowserPlatformFollowsSetter 钉住"platform 必须与 UA 同源"这条约束的实现入口：
// 以前它写死 Win32，碰到 Mac/Linux 的 UA 就直接自相矛盾，而矛盾是抖音不需要多聪明就能看到的。
func TestBrowserPlatformFollowsSetter(t *testing.T) {
	cases := []struct {
		set, want string
	}{
		{"", "Win32"}, // 未设定时的兜底：老调用方行为不变
		{"Win32", "Win32"},
		{"MacIntel", "MacIntel"},
	}
	for _, c := range cases {
		q := queryOf(t, mustRun(t, c.set).WssUrl)
		if got := q.Get("browser_platform"); got != c.want {
			t.Errorf("SetBrowserPlatform(%q) 之后 URL 里是 %q，应为 %q", c.set, got, c.want)
		}
	}
}

// TestBrowserVersionMatchesUA 钉住"UA 只有一份来源"：两个参数合起来必须就是那个 UA。
//
// 形状不是"browser_version=132.0.0.0"：抖音 web 把带引号的完整 UA **拆在两个参数里**——
//
//	browser_name    = "Mozilla        （开头引号在这里）
//	browser_version = 5.0 (…) Chrome/132.0.0.0 Safari/537.36"   （结尾引号在这里）
//
// 这是写测试时跟真实输出对账对出来的，不是从代码猜的：拿它去验成"纯版本号"就是
// 把一个真实形状当成 bug 改掉（改掉后握手会被拒，而且不报错）。
func TestBrowserVersionMatchesUA(t *testing.T) {
	q := queryOf(t, mustRun(t, "Win32").WssUrl)
	bn, bv := q.Get("browser_name"), q.Get("browser_version")

	if !strings.Contains(bv, "Chrome/132.0.0.0") {
		t.Errorf("browser_version 里没有 UA 的 Chrome 版本：%q", bv)
	}
	if !strings.HasSuffix(bv, `"`) {
		t.Errorf("browser_version 应以引号收尾（UA 尾段的收尾引号）：%q", bv)
	}
	if strings.HasPrefix(bv, `"`) {
		t.Errorf("browser_version 不该自带开头引号，那个引号属于 browser_name：%q", bv)
	}
	if !strings.HasPrefix(bn, `"`) {
		t.Errorf("browser_name 应带开头引号：%q", bn)
	}
	// 两个参数去掉包裹引号后合起来必须能重新拼出完整 UA："UA 只有一份来源"这说法才是真的。
	rejoined := strings.TrimPrefix(bn, `"`) + "/" + strings.TrimSuffix(bv, `"`)
	if rejoined != probeUA {
		t.Errorf("browser_name+browser_version 拼不回原 UA：\n  期望 %q\n  实际 %q", probeUA, rejoined)
	}
}

func TestSignatureLooksLikeFrontierSign(t *testing.T) {
	ws := mustRun(t, "Win32")
	if ws.Sign == "" {
		t.Fatal("签名为空——签名材料可能没加载（看 jsFrom）")
	}
	// 实测这套材料产出的是 16 字符短串（X-Bogus/frontier sign），不是 184 字符的 a_bogus。
	// 长度突然变化意味着材料换了算法，那是必须知道的事。
	if len(ws.Sign) < 8 || len(ws.Sign) > 64 {
		t.Errorf("签名长度 %d 不像现网那份 16 字符短串：%q", len(ws.Sign), ws.Sign)
	}
	if !strings.Contains(ws.WssUrl, "signature=") {
		t.Error("WS URL 里没带 signature 参数")
	}
}

// TestWssURLStableForSameInputs 同一份输入应得到同一份 URL（把签名值归一后比较）。
// 变了说明拼装里掺了时间/随机量 —— 那会让"同一个房间两次重连身份不一致"这种问题无法复现。
func TestWssURLStableForSameInputs(t *testing.T) {
	a := normalizeSign(mustRun(t, "Win32").WssUrl)
	b := normalizeSign(mustRun(t, "Win32").WssUrl)
	if a != b {
		t.Errorf("同输入两次结果不同：\n  %s\n  %s", a, b)
	}
}

func queryOf(t *testing.T, raw string) url.Values {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("WS URL 解析失败：%v（%s）", err, raw)
	}
	return u.Query()
}

// orderedKeys 从原始 query 字面量按出现顺序取键名（url.Values 是 map，读不出顺序）。
func orderedKeys(t *testing.T, raw string) []string {
	t.Helper()
	i := strings.Index(raw, "?")
	if i < 0 {
		t.Fatalf("URL 里没有 ?：%s", raw)
	}
	var keys []string
	for _, kv := range strings.Split(raw[i+1:], "&") {
		if kv == "" {
			continue
		}
		keys = append(keys, strings.SplitN(kv, "=", 2)[0])
	}
	return keys
}

func normalizeSign(raw string) string {
	i := strings.Index(raw, "signature=")
	if i < 0 {
		return raw
	}
	j := strings.Index(raw[i:], "&")
	if j < 0 {
		return raw[:i]
	}
	return raw[:i] + "signature" + raw[i+j:]
}

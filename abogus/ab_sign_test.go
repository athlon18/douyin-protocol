package abogus

// a_bogus 的可冻结基准：把时间钉住之后，同输入必须同输出，且每个输入都真的影响输出。
//
// 为什么这不是"与官方 JS 对账"（要说清，别让它冒充它没有的能力）：
//   仓里没有能产出 a_bogus 的官方脚本（webmssdk.js 只产 16 字符的 X-Bogus，
//   里面 "a_bogus" 出现 0 次），所以这份基准是**自冻结的回归锁**：
//   它能立刻抓住"有人改了 rc4/摘要/编码表"这类算法漂移，但**不能**证明与官方逐字节一致。
//   已知差距（2026-10-15 实测）：我们这份恒定 164 字符，而从浏览器请求里抓到的那份是 184 字符 ——
//   长度就不一致，因此更谈不上逐字节一致；抖音两种都认（真实接口拉过 1.4MB 观众榜）。
//   要补成真对账，前置条件是拿到产 a_bogus 的官方脚本并在 Node/goja 里钉死 Date.now/Math.random。
//
// 为什么"能钉住时间"本身就是进展：改之前 startTime 在函数体里取墙钟，
// 同输入每次输出都不同 —— 那样任何基准都冻结不了，算法被改坏也不会有测试变红。
import (
	"strings"
	"testing"
)

const goldenUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/132.0.0.0 Safari/537.36"

const goldenUAMac = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

const goldenEnvMac = "1440|900|1440|860|0|30|0|0|1392|92|1440|860|1377|92|1|24|MacIntel"

// goldenTime 是钉死的时间戳（毫秒，按实现里的 uint32 截断方式取）。
// 用它，签名才与"什么时候跑测试"无关。
var goldenTime = func() uint32 {
	var ms int64 = 1760000000000
	return uint32(ms)
}()

func TestAbSignIsReproducibleOnceTimePinned(t *testing.T) {
	params := "aid=6383&app_name=douyin_web&web_rid=966303183956&msToken=abc"
	a := abSignAt(params, goldenUA, DefaultWindowEnv, goldenTime)
	b := abSignAt(params, goldenUA, DefaultWindowEnv, goldenTime)
	if a != b {
		t.Fatalf("同输入同时间却给了不同签名，基准无法冻结：\n  %s\n  %s", a, b)
	}
	// 时间真的被编进去了：+1ms 就该变。若不变，说明注入的时间是个死参数（读了却没参与运算）。
	c := abSignAt(params, goldenUA, DefaultWindowEnv, goldenTime+1)
	if a == c {
		t.Error("时间戳进不了签名：注入的 startTime 没有参与运算，这份算法对时间不敏感")
	}
}

// TestAbSignGoldenVectors 冻结的签名基准。抄录于 2026-10-15，来源是本实现自身（见文件头说明）。
//
// 它拦住的是"改算法没人知道"：任何一处 rc4 密钥、摘要轮数、编码表、字段顺序的改动
// 都会在这里变红，而不是等到现网房间静默拿不到数据。
func TestAbSignGoldenVectors(t *testing.T) {
	cases := []struct {
		name   string
		params string
		ua     string
		env    string
		want   string
	}{
		{
			name:   "enter-win",
			params: "aid=6383&app_name=douyin_web&web_rid=966303183956&msToken=abc",
			ua:     goldenUA, env: DefaultWindowEnv,
			want: "E7mhBmg6mEVNgf6X56nLfY3q6lP3Y6il0HViMD2f2VV-0L39HMYD9exo1HUveKWjZsmfIFYjy4hbO3xprQAjM36UHWwEUdQ2mgWkKl5Q5I0j53iruyRDntmF4vj3SFlm5XNAEOk0y75rKb70Woqe-vIlO62-zo0/9UW=",
		},
		{
			name:   "enter-mac-env",
			params: "aid=6383&app_name=douyin_web&web_rid=966303183956&msToken=abc",
			ua:     goldenUA, env: goldenEnvMac,
			want: "E7mhBmg6mEVNgf6X56nLfY3q6lP3Y6il0HViMD2f2VV-0L39HMYD9exo1HUveKWjZsmfIFYjy4hbTpcprQCj01wf98vx/25kmDSkKl12so0j53inegbQE0wimkt-tec2RvBlrOX/ohpHzbi0AxAJ5k1Eehchu1LRitW=",
		},
		{
			name:   "audience",
			params: "aid=6383&app_name=douyin_web&rank_type=30&room_id=7696538532378184448&msToken=",
			ua:     goldenUA, env: DefaultWindowEnv,
			want: "E7mhBmg6mEVNgf6X56nLfY3q64a3Y6il0HViMD2fWxV-0L39HMYD9exo1HUveKWjZsmfIFYjy4hbO3xprQAjM36UHWwEUdQ2mgWkKl5Q5I0j53iruyRDntmF4vj3SFlm5XNAEOk0y75rKb70Woqe-vIlO62-zo0/9UE=",
		},
		{
			name: "empty-params", params: "",
			ua: goldenUA, env: DefaultWindowEnv,
			want: "E7mhBmg6mEVNgf6X56nLfY3q6fe3Y6il0HViMD2fvxV-0L39HMYD9exo1HUveKWjZsmfIFYjy4hbO3xprQAjM36UHWwEUdQ2mgWkKl5Q5I0j53iruyRDntmF4vj3SFlm5XNAEOk0y75rKb70Woqe-vIlO62-zo0/91y=",
		},
		{
			name:   "other-ua",
			params: "aid=6383&app_name=douyin_web&web_rid=966303183956&msToken=abc",
			ua:     goldenUAMac, env: DefaultWindowEnv,
			want: "E7mhBmg6mEVNgf6X56nLfY3q6lP3Yvhl0HViMD2f2VvD0L39HMYD9exo1HUveKWjZsmfIFYjy4hbO3xprQAjM36UHWwEUdQ2mgWkKl5Q5I0j53iruyRDntmF4vj3SFlm5XNAEOk0y75rKb70Woqe-vIlO62-zo0/9X6=",
		},
	}
	for _, c := range cases {
		got := abSignAt(c.params, c.ua, c.env, goldenTime)
		if got != c.want {
			t.Errorf("[%s] 签名与基准不一致 —— 算法被改了。若确实是有意改动，先看是不是"+
				"rc4 密钥/摘要轮数/编码表/字段顺序，再重生成基准并用真房间复验\n  期望 %s\n  实际 %s", c.name, c.want, got)
		}
	}
}

// TestEveryInputActuallyMatters 逐个变量验证"它真的参与了签名"。
// 假实现的形状就是"参数换了签名不变"——历史上我们把 UA 传进去又自己写死过一份，就是这个坑。
func TestEveryInputActuallyMatters(t *testing.T) {
	base := abSignAt("aid=6383&web_rid=1&msToken=x", goldenUA, DefaultWindowEnv, goldenTime)

	changed := map[string]string{
		"参数串":  abSignAt("aid=6383&web_rid=2&msToken=x", goldenUA, DefaultWindowEnv, goldenTime),
		"UA":   abSignAt("aid=6383&web_rid=1&msToken=x", goldenUAMac, DefaultWindowEnv, goldenTime),
		"窗口环境": abSignAt("aid=6383&web_rid=1&msToken=x", goldenUA, goldenEnvMac, goldenTime),
		"时间戳":  abSignAt("aid=6383&web_rid=1&msToken=x", goldenUA, DefaultWindowEnv, goldenTime+7),
	}
	for name, got := range changed {
		if got == base {
			t.Errorf("只改 %s 签名却没变：这个输入没被吃进算法，签名与实发内容已经脱钩", name)
		}
	}

	// 反向也要成立：什么都不改就必须一模一样（防止实现里还藏着别的随机源）。
	same := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		same = append(same, abSignAt("aid=6383&web_rid=1&msToken=x", goldenUA, DefaultWindowEnv, goldenTime))
	}
	if same[0] != same[1] || same[1] != same[2] {
		t.Errorf("完全相同的输入给出不同签名，说明还有未固定的随机源：%v", same)
	}
}

func TestAbSignShape(t *testing.T) {
	got := AbSign("aid=6383&web_rid=1&msToken=x", goldenUA)
	if !strings.HasSuffix(got, "=") {
		t.Error("签名结尾的 \"=\" 不见了：它是签名本体，被剪掉时抖音回 200+空体，不报错")
	}
	if len(got) != 164 {
		t.Errorf("签名长度 %d，与基准 164 不一致（这算法的输出长度与输入无关，变了就是结构改动）：%s", len(got), got)
	}
	if strings.ContainsAny(got[:len(got)-1], "&=") {
		t.Errorf("签名里出现裸的 & 或 =，拼进 query 会把一串参数切成多串：%s", got)
	}
}

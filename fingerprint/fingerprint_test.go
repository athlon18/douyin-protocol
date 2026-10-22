package fingerprint

// 浏览器画像池的自洽与稳定性单测。
// 这里守的是三条会静默劣化采集效果的性质：
//   ① 池子里每一条画像内部必须自洽（UA ↔ platform ↔ Chrome 版本 ↔ 屏幕）；
//   ② 同一房间号在任何进程、任何时刻算出的画像必须相同（不入库也要一致）；
//   ③ 房间之间要真的分散开（不能 8 条池子只用到 1~2 条）。
import (
	"strings"
	"testing"
)

func TestFingerprintPoolIsSelfConsistent(t *testing.T) {
	pool := AllFingerprints()
	if len(pool) < 6 {
		t.Fatalf("画像池只剩 %d 条，房间多了就不够分散", len(pool))
	}
	for i, f := range pool {
		if why := f.SelfCheck(); why != "" {
			t.Errorf("池子第 %d 条不自洽：%s ｜ UA=%s", i, why, f.UA)
		}
		// 自洽不等于可用：2026-10-15 实测发现两条完全自洽的画像（WOW64 / Linux 桌面）
		// 会被抖音静默拒成空体，影响面恰好是“分到这两条的那批房间”，不报错也不掉线。
		// 这条断言把那个结论钉住：不能再往池子里添已知被拒的形态。
		if why := f.RejectedReason(); why != "" {
			t.Errorf("池子第 %d 条是实测被拒的形态：%s ｜ UA=%s", i, why, f.UA)
		}
		if !strings.Contains(f.UA, "Mozilla/5.0") || !strings.Contains(f.UA, "Safari/") {
			t.Errorf("池子第 %d 条 UA 形状不像真实浏览器：%s", i, f.UA)
		}
	}
}

func TestFingerprintForIsDeterministic(t *testing.T) {
	ids := []string{"966303183956", "921255738359", "58429704324", "77994390690", "25456700441"}
	for _, id := range ids {
		a := FingerprintFor(id)
		b := FingerprintFor(id)
		if a != b {
			t.Fatalf("同一房间 %s 两次算出不同画像（%q vs %q）—— 那等于每次请求换身份", id, a.UA, b.UA)
		}
		// 带空格/前后空白的房间号不能算成另一个身份
		if FingerprintFor(" "+id+" ") != a {
			t.Errorf("房间号带空格时画像变了：%s", id)
		}
	}
	// 空房间号必须走固定回退，绝不能随机挑一条
	if FingerprintFor("") != FingerprintFor("") {
		t.Error("空房间号每次都算出不同画像")
	}
}

func TestFingerprintForSpreadsAcrossRooms(t *testing.T) {
	seen := map[string]int{}
	for n := 0; n < 400; n++ {
		id := "96630318" + string(rune('A'+n%26)) + string(rune('a'+n/26%26))
		seen[FingerprintFor(id).UA]++
	}
	// 分布不该偏科：每条画像的期望频次是 400/8=50，这里要求**每条都落在期望值的 0.5~2 倍内**。
	// 只判"用到几条 + 最大不超四成"太松：8 条池子抽 400 个房间必然全用到，那种断言永远不会红。
	// （本轮在 spread.go 里就是被这类松断言骗过：FNV 高位不散，40 个 key 实际只散开 10 微秒。）
	pool := len(AllFingerprints())
	want := 400 / pool
	for ua, c := range seen {
		if c < want/2 || c > want*2 {
			t.Errorf("画像 %q 覆盖了 %d/400 个房间，超出期望 %d 的 0.5~2 倍范围，分散名不副实", ua, c, want)
		}
	}
	t.Logf("400 个房间号落在 %d 条画像上，期望每条 %d 个", len(seen), want)
}

func TestFingerprintQueryParamsAreEncoded(t *testing.T) {
	// 带空格的值在 query 里必须是 + 编码，且不能出现裸空格：
	// 签名是对实发串做的，串里多个空格就是"签的不是发的那串"。
	for _, f := range AllFingerprints() {
		q := f.QueryParams()
		if strings.Contains(q, " ") {
			t.Errorf("QueryParams 里有裸空格（应编码为 +）：%s", q)
		}
		if !strings.Contains(q, "browser_platform=") || !strings.Contains(q, "screen_width=") {
			t.Errorf("QueryParams 缺字段：%s", q)
		}
		if !strings.Contains(q, "browser_version="+f.ChromeVer) {
			t.Errorf("QueryParams 的 browser_version 与画像 ChromeVer 不一致：%s", q)
		}
	}
}

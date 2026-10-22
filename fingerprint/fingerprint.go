// Package fingerprint 提供抖音网页版接口使用的"客户端身份"：UA、navigator.platform、
// 屏幕尺寸与 os/browser 参数。
//
// 它是签名算法的一部分而不是装饰：a_bogus 与 WS signature 都把 UA 吃进算法，
// 实发参数又与画像同源。画像池里任何一条即使内部自洽，只要平台形态被抖音静默
// 拒绝（200 + 空体），分到它的房间就会长期拿不到数据 —— 见 RejectedReason。
package fingerprint

// 浏览器画像（fingerprint）：一个房间用哪套"客户端身份"。
//
// 为什么要有这个包：早先各处各自为政地写 UA，且互相矛盾 ——
// 抓页面用随机 OS+随机 Chrome 版本、WS 握手头写死 Chrome/104、HTTP 签名写死 Chrome/120，
// 而 webmssdk 把 browser_platform 写死 "Win32"：于是发给抖音的握手 URL 里出现
// "browser_platform=Win32 且 UA 是 (X11; Linux x86_64)" 这种 Windows 平台 + Linux 浏览器的
// 自相矛盾。签名（a_bogus / WS signature）都是把 UA 喂进算法的，参数与 UA 对不上
// 就是我们自己造的破绽，不需要抖音多聪明就能看出来。
//
// 选型：**由房间号确定性派生**，而不是随机、也不是入库：
//   - 房间与房间之间用不同画像 → 达到"分散风控"的目的（一个号被标记不牵连其他房间）；
//   - 同一房间内永远同一套 → 像真实浏览器（人重启电脑 UA 也不变），且重连/换进程不漂移；
//   - 不入库 → watcher 与 api 两个进程各自算出的值天然相同，零迁移、零同步、不会有"谁是权威"的问题。
//
// 想做"全体换一批身份"时不用改代码：把 fingerprintSalt 换个值即可（所有房间一起重排）。
import (
	"hash/fnv"
	"net/url"
	"strconv"
	"strings"
)

// fingerprintSalt 画像池的加盐量。改了它 = 所有房间一次性换身份。
const fingerprintSalt = "douyinlive-fp-1"

// Fingerprint 一份**内部自洽**的浏览器身份：UA 里的系统/平台/版本与其余字段严格对应。
type Fingerprint struct {
	UA           string // 完整 User-Agent
	Platform     string // navigator.platform：Win32 / MacIntel / Linux x86_64
	OSName       string // 接口参数 os_name
	OSVersion    string // 接口参数 os_version
	ChromeVer    string // 接口参数 browser_version，如 132.0.0.0
	ScreenWidth  int    // 接口参数 screen_width
	ScreenHeight int    // 接口参数 screen_height
}

// fingerprintPool 手工配好的自洽画像。**每条都要自查**：UA 里的系统串要和 Platform/OSName 对得上，
// UA 里的 Chrome 版本要和 ChromeVer 对得上，屏幕尺寸要是该平台上真实会出现的分辨率。
//
// ⚠ 但“内部自洽”只是必要条件：池子里每一条都必须**拿真实接口实测过**。
// 2026-10-15 实测：原本这两条内部完全自洽，却被抖音静默拒掉（room/web/enter 回 200+空体）：
//   - UA 带 `WOW64`（64 位 Windows 上跑 32 位浏览器的形态）—— 同参数换成 `Win64; x64` 就通，可归因；
//   - UA 带 `X11; Linux x86_64`（整个 Linux 桌面形态）—— 改 os_version、升 Chrome 版本都没用。
//
// 失败不报错误码、也不报签名失败，而是**静默空体**，且永远只影响分到这两条画像的那批房间
// —— 没有实测就会有一四分之一的房间长期拿不到数据而无人发现。
// 加新条目的正确顺序：先写进池子 → 用 RoomEnterWith 对真实房间跑一次 → 通了才留下。
var fingerprintPool = []Fingerprint{
	{
		UA:       "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/132.0.0.0 Safari/537.36",
		Platform: "Win32", OSName: "Windows", OSVersion: "10", ChromeVer: "132.0.0.0",
		ScreenWidth: 1920, ScreenHeight: 1080,
	},
	{
		UA:       "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36",
		Platform: "Win32", OSName: "Windows", OSVersion: "10", ChromeVer: "128.0.0.0",
		ScreenWidth: 1536, ScreenHeight: 864, // 1080p 屏 + 125% 缩放后的逻辑分辨率，Windows 上极常见
	},
	{
		// 以前这条用 WOW64，实测被拒；换成主流 64 位形态后同一分辨率 1366x768 是通的。
		UA:       "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36",
		Platform: "Win32", OSName: "Windows", OSVersion: "10", ChromeVer: "126.0.0.0",
		ScreenWidth: 1366, ScreenHeight: 768,
	},
	{
		UA:       "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/132.0.0.0 Safari/537.36",
		Platform: "MacIntel", OSName: "Mac OS", OSVersion: "10.15.7", ChromeVer: "132.0.0.0",
		ScreenWidth: 1440, ScreenHeight: 900,
	},
	{
		UA:       "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36",
		Platform: "MacIntel", OSName: "Mac OS", OSVersion: "10.15.7", ChromeVer: "126.0.0.0",
		ScreenWidth: 1680, ScreenHeight: 1050,
	},
	{
		UA:       "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36",
		Platform: "MacIntel", OSName: "Mac OS", OSVersion: "10.15.7", ChromeVer: "130.0.0.0",
		ScreenWidth: 2560, ScreenHeight: 1440,
	},
	{
		// 以前这条是 Linux 桌面（X11; Linux x86_64），实测在这条接口上整族被拒，换掉。
		UA:       "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36",
		Platform: "Win32", OSName: "Windows", OSVersion: "10", ChromeVer: "138.0.0.0",
		ScreenWidth: 1920, ScreenHeight: 1080,
	},
	{
		UA:       "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		Platform: "Win32", OSName: "Windows", OSVersion: "10", ChromeVer: "120.0.0.0",
		ScreenWidth: 1280, ScreenHeight: 720,
	},
}

// FingerprintFor 返回该房间固定使用的那份画像。
// 入参用房间短号（直播间 URL 上那个号）：它在各进程里都拿得到且长期不变，
// 因此"同一房间 = 同一身份"这条性质不依赖数据库、也不依赖进程是否重启过。
func FingerprintFor(roomID string) Fingerprint {
	id := strings.TrimSpace(roomID)
	if id == "" {
		// 没有房间号时不能随机挑：随机意味着同一次运行里前后两次调用会拿到不同身份，
		// 那正是本次要消灭的问题。这里固定回退到池首。
		return fingerprintPool[0]
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(fingerprintSalt + "|" + id))
	return fingerprintPool[int(h.Sum32())%len(fingerprintPool)]
}

// QueryParams 给出该画像对应的接口参数片段（HTTP 签名串与 WS URL 都用它拼），
// 保证"参与签名的值"和"发出去的头"来自同一个源，不会再各自写一份对不上。
//
// 字段顺序不是随便排的：a_bogus 是对整串参数做的，而实测通过的那条浏览器请求
// 就是 screen → browser → os 这个顺序。要改顺序必须重新拿真房间验一次，
// 不能为了“好看”在这里重排。
//
// 必须用编码态值：浏览器发的是 os_name=Mac+OS、browser_platform=Linux+x86_64（空格在 query 里是 +），
// 而 a_bogus 是对“我们实发的那一串”做的 —— 签名串与 wire 上不一致，比不自洽更难查。
func (f Fingerprint) QueryParams() string {
	return "screen_width=" + strconv.Itoa(f.ScreenWidth) +
		"&screen_height=" + strconv.Itoa(f.ScreenHeight) +
		"&browser_language=zh-CN" +
		"&browser_platform=" + url.QueryEscape(f.Platform) +
		"&browser_name=Chrome" +
		"&browser_version=" + f.ChromeVer +
		"&os_name=" + url.QueryEscape(f.OSName) +
		"&os_version=" + url.QueryEscape(f.OSVersion)
}

// UAShort 只要 UA 里的 Chrome 版本段（个别接口按 "120.0.0.0" 这种形式传）。
func (f Fingerprint) UAShort() string { return f.ChromeVer }

// SelfCheck 校验一份画像内部是否自洽（UA ↔ Platform/ChromeVer）。
// 返回不一致的原因；空串表示这份画像没问题。池子里的每条都必须通过它，
// 由单测在 CI 阶段强制 —— 画像池一旦被人随手加错，测试会直接红，而不是等线上掉线。
func (f Fingerprint) SelfCheck() string {
	if f.Platform == "Win32" && !strings.Contains(f.UA, "Windows NT") {
		return "Platform=Win32 但 UA 里没有 Windows NT"
	}
	if f.Platform == "MacIntel" && !strings.Contains(f.UA, "Macintosh") {
		return "Platform=MacIntel 但 UA 里没有 Macintosh"
	}
	if f.Platform == "Linux x86_64" && !strings.Contains(f.UA, "Linux x86_64") {
		return "Platform=Linux x86_64 但 UA 里没有 Linux x86_64"
	}
	if !strings.Contains(f.UA, "Chrome/"+f.ChromeVer) {
		return "UA 里的 Chrome 版本与 ChromeVer 字段不一致：" + f.ChromeVer
	}
	if f.ScreenWidth <= 0 || f.ScreenHeight <= 0 {
		return "屏幕尺寸非法"
	}
	return ""
}

// GlobalFingerprint 给“没有房间上下文”的请求用的一份固定身份：全站礼物表 / 表情 / 特效素材、
// 以及“我自己账号页”这类与房间无关的接口。
//
// 这里刻意**不随机、也不按调用方派生**：这些是进程启动时拉一两次的公共资源，
// 身份漂了只会让抖音看到“同一份资源被不同客户端反复请求”；而按房间派生又无从派生（没有房间号）。
// 所以定一条：全局素材永远用池首那份，它不再是“仓里第 5 个身份”，而是池里第一条的正式别名。
func GlobalFingerprint() Fingerprint { return fingerprintPool[0] }

// AllFingerprints 返回画像池（只读用途：自检单测、以及"这池子里是否有任何一条不自洽"的检查）。
func AllFingerprints() []Fingerprint {
	out := make([]Fingerprint, len(fingerprintPool))
	copy(out, fingerprintPool)
	return out
}

// rejectedUAShapes 实测被抖音**静默拒绝**的 UA 形态（接口回 200 + 0 字节，不给错误码）。
//
// 2026-10-15 用交叉实验定位：“某些房间 RoomEnter 拿不到数据”完全跟着画像走、与房间无关
// （同一房间换 8 条画像，失败的永远是那两条；换回能过的画像立即就通，结果稳定可重现）。
var rejectedUAShapes = []struct{ frag, why string }{
	{"WOW64", "64 位 Windows 上跑 32 位浏览器的形态；同参数换成 Win64; x64 就通"},
	{"X11; Linux", "Linux 桌面形态整族被拒（改 os_version、升 Chrome 版本都无效）"},
}

// signPlatformAllowed 实测只有这两个平台形态能被抖音接受。用**白名单**而不是黑名单：
// 这一维上"没见过的形态"就是会被拒（iPhone / MacPPC / Fake / Linux x86_64 全部 200+0 字节）。
//
// 2026-10-15 单变量实测：同一房间、实发参数完全不动，只换签名里 windowEnv 的平台段 ——
// Win32/MacIntel 拿到完整 JSON，其余一律空体。也就是说 **平台在 UA 和签名里各查一次**，
// 光把 UA 改对不够。
var signPlatformAllowed = map[string]bool{"Win32": true, "MacIntel": true}

// RejectedReason 返回该画像命中"已知被拒形态"的原因；空串表示没命中。
// 它跟 SelfCheck 管的不是件事：SelfCheck 验"内部对不对得住"，这里验"抖音认不认"。
// 内部自洽但被拒的画像最阴病：代码怎么看都对，只有分到这个画像的房间在静默丢数据。
func (f Fingerprint) RejectedReason() string {
	for _, r := range rejectedUAShapes {
		if strings.Contains(f.UA, r.frag) {
			return "UA 含 " + r.frag + "：" + r.why
		}
	}
	if !signPlatformAllowed[f.Platform] {
		return "platform=" + f.Platform + "：它会被编进 a_bogus 的窗口环境里，实测只认 Win32 / MacIntel"
	}
	return ""
}

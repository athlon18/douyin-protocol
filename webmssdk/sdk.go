package webmssdk

import (
	"crypto/md5"
	"crypto/sha256"
	"embed"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"
)

//go:embed webmssdk.js
var embeddedFS embed.FS

// 签名材料分两层：内嵌的那份是出厂基线，磁盘上的那份是可覆盖副本。
// 为什么这么分：材料要跟着抖音改版换，换文件比重新编译二进制快得多；
// 但把"主项目里的相对路径"写死在包里，一换项目/一换工作目录就读不到，
// 接着 goja 跑空脚本、拿不到 get_sign，整个调用方被带崩。
//
// 因此：默认只用内嵌基线（零配置可用）；需要"不重编就换材料"时，
// 调用方显式 SetMaterialPath 指向自己的文件。

// jsMinBytes 小于这个数就当作“没读到有效内容”：真文件是几百 KB，
// 空文件/错误页只会很短，拿它去跑 goja 只会得到一个看不出原因的失败。
const jsMinBytes = 1000

// materialPath 是进程级配置：磁盘上那份可覆盖材料的路径；空 = 只用内嵌基线。
var (
	materialMu   sync.RWMutex
	materialPath string
)

// SetMaterialPath 指定磁盘上那份可覆盖签名材料的路径（传空串 = 恢复只用内嵌基线）。
// 只影响之后的签名调用；已创建的 *Webmssdk 仍用创建时读到的材料。
func SetMaterialPath(p string) {
	materialMu.Lock()
	materialPath = p
	materialMu.Unlock()
}

// MaterialPath 返回当前配置的磁盘材料路径（监控告警文案要告诉运维该换哪个文件）；
// 空串表示没有配置，只使用内嵌基线。
func MaterialPath() string {
	materialMu.RLock()
	defer materialMu.RUnlock()
	return materialPath
}

// reSDKVersion 抓 js 头部注释里的自声明版本（实测形如 `/** 1.0.0.53 */`）。
var reSDKVersion = regexp.MustCompile(`/\*\*\s*([0-9][0-9.\-a-z]+)\s*\*/`)

// jsSource 取当前实际生效的那份签名材料，并说清它从哪来。
func jsSource() (src string, from string) {
	if p := MaterialPath(); p != "" {
		b, err := os.ReadFile(p)
		switch {
		case err == nil && len(b) >= jsMinBytes:
			return string(b), "disk"
		case err == nil: // 文件在但内容不对：比“不存在”更值得喊一声
			log.Printf("[sign] %s 只有 %d 字节，不像有效的签名材料，改用内嵌基线", p, len(b))
		case !errors.Is(err, os.ErrNotExist):
			log.Printf("[sign] 读 %s 失败（改用内嵌基线）: %v", p, err)
		}
	}
	if e, err2 := embeddedFS.ReadFile("webmssdk.js"); err2 == nil && len(e) >= jsMinBytes {
		return string(e), "embed"
	}
	return "", "none" // 两处都拿不到：调用方会把它当错误报，而不是 panic
}

// SignMaterialInfo 给「官方 SDK 监控」用：当前生效材料的来源、字节数、指纹与自版本号。
// 指纹是“换没换”的唯一硬判据 —— 自版本号只有 js 里写了才有。
func SignMaterialInfo() (from string, bytes int, sha string, version string) {
	src, from := jsSource()
	if src == "" {
		return from, 0, "", ""
	}
	s := sha256.Sum256([]byte(src))
	v := ""
	if m := reSDKVersion.FindStringSubmatch(src); len(m) == 2 {
		v = m[1]
	}
	return from, len(src), fmt.Sprintf("%x", s)[:12], v
}

// MaterialBytes 返回当前生效那份材料的完整内容（监控要算指纹、存 known-good 要用）。
func MaterialBytes() ([]byte, string, error) {
	src, from := jsSource()
	if src == "" {
		return nil, from, errors.New("签名材料缺失：磁盘与内嵌基线都没取到")
	}
	return []byte(src), from, nil
}

// RestoreMaterial 把一份已知可用的材料写回当前配置的磁盘路径（known-good 回滚用）。
// 只回滚到“上次真签成功过”的那份，不猜。
// 未配置磁盘路径时返回错误：没有落点就无法回滚，猜测写哪里比不写更危险。
func RestoreMaterial(b []byte) error {
	if len(b) < jsMinBytes {
		return fmt.Errorf("待回滚的材料只有 %d 字节，不像有效内容", len(b))
	}
	p := MaterialPath()
	if p == "" {
		return errors.New("未配置磁盘材料路径：先调用 SetMaterialPath 指定落点")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}

// SelfTest 真跑一次签名。“材料能加载”不等于“签名还能用”，所以只能这么测。
// 房间号用一个固定值：这里只关心“能不能算出东西”，不关心算的是哪个房。
func SelfTest(ua string) (ms int64, signLen int, err error) {
	start := time.Now()
	w := New(7300000000000000001).SetUa(ua)
	if _, err = w.Run("7695798596729801482", "123456"); err != nil {
		return time.Since(start).Milliseconds(), 0, err
	}
	return time.Since(start).Milliseconds(), len(w.Sign), nil
}

// DefaultUA 与运行时实际用的 UA 保持一致：签名对 UA 敏感，换了 UA 就是另一回事。
const DefaultUA = "Mozilla/5.0 (Windows NT 10.0; WOW64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/104.0.5112.20 Safari/537.36"

type Webmssdk struct {
	js             string
	jsFrom         string // disk / embed / none：出错时要能从日志看出用的是哪份材料
	ua             string
	platform       string // navigator.platform：必须与 ua 同一份画像（以前写死 Win32，碰到 Mac/Linux 的 UA 就直接自相矛盾）
	userUniqueId   int64
	liveRoomRealId string
	WssUrl         string
	Sign           string
	err            error // 签名材料缺失等致命问题，逐层带到 Run() 的返回值
}

func New(userUniqueId int64) *Webmssdk {
	src, from := jsSource()
	rand.NewSource(time.Now().UnixMicro())
	if userUniqueId == 0 {
		userUniqueId = rand.Int63n(700000000000000000-1) + 7300000000000000000
	}
	w := &Webmssdk{js: src, jsFrom: from, userUniqueId: userUniqueId}
	if src == "" {
		w.err = errors.New("签名材料缺失：磁盘与内嵌基线都没取到")
	}
	return w
}

func (w *Webmssdk) RandUserID() *Webmssdk {
	rand.NewSource(time.Now().UnixMicro())
	w.userUniqueId = rand.Int63n(700000000000000000-1) + 7300000000000000000
	return w
}

func (w *Webmssdk) Run(liveRoomRealId string, pushID string) (*Webmssdk, error) {
	var err error
	w.liveRoomRealId = liveRoomRealId
	w.WssUrl, err = w.wssUrl(pushID)
	return w, err
}

func (w *Webmssdk) SetUa(ua string) *Webmssdk {
	w.ua = ua
	return w
}

// SetBrowserPlatform 设定发给抖音的 browser_platform（Win32 / MacIntel / Linux x86_64）。
// 它不进签名入参（get_sign 吃的是那固定的一串参数 md5），只影响 URL 里的声明，
// 所以它必须与 SetUa 的 UA 同源 —— 两者对不上就是我们自己送的矛盾。
func (w *Webmssdk) SetBrowserPlatform(platform string) *Webmssdk {
	w.platform = platform
	return w
}

// browserPlatform 未设定时兜底 Win32：只 SetUa 不 SetPlatform 的老调用方行为不变，
// 不会因为加了个字段就突然改口。
func (w *Webmssdk) browserPlatform() string {
	if w.platform != "" {
		return w.platform
	}
	return "Win32"
}

func (w *Webmssdk) wssUrl(pushID string) (urls string, err error) {
	// 材料缺失在这里就当错误返回，不再往下跑：
	// 以前拿空 js 去 goja 会命中 panic("Not a function")，在房间协程里直接带崩整个进程，
	// 而且日志里只会看到一个看不出原因的 panic。
	if w.err != nil {
		return "", w.err
	}
	if err = w.sign(pushID); err != nil {
		return
	}
	bro := strings.Split(w.ua, "/")

	params := []map[string]any{
		{"room_id": w.liveRoomRealId},
		{"compress": "gzip"},
		{"version_code": 180800},
		{"webcast_sdk_version": "1.0.14-beta.0"},
		{"live_id": "1"},
		{"did_rule": "3"},
		{"user_unique_id": w.userUniqueId},
		{"identity": "audience"},
		{"signature": url.QueryEscape(w.Sign)},
		{"aid": "6383"},
		{"device_platform": "web"},
		{"browser_language": "zh-CN"},
		{"browser_platform": w.browserPlatform()},
		{"browser_name": "%22" + bro[0]},
		{"browser_version": url.QueryEscape(strings.Trim(strings.Join(bro[1:], "/"), " ") + `"`)},
	}

	data := ""
	for _, item := range params {
		for k, v := range item {
			data += k + "=" + fmt.Sprint(v) + "&"
		}
	}
	return "wss://webcast5-ws-web-lf.douyin.com/webcast/im/push/v2/?" + strings.Trim(data, "&"), err
}

func (w *Webmssdk) sign(pushID string) (err error) {
	vm := goja.New()
	_, err = vm.RunString(`document={};window={};navigator={"userAgent":"` + w.ua + `"};` + w.js)
	if err != nil {
		return fmt.Errorf("执行签名 js 失败（材料来源 %s）: %w", w.jsFrom, err)
	}
	// 加载函数
	sign, ok := goja.AssertFunction(vm.Get("get_sign"))
	if !ok {
		// 不 panic：签名材抖被换成官方原版（只导 frontierSign、没有 get_sign 包装）
		// 或是版本改了导致入口改名时，这里会命中。报错了房间只是连不上 WS，
		// 不该把整个 watcher 带崩（那会连带所有其他房间掉线）。
		return fmt.Errorf("签名材料里没有 get_sign 入口（来源 %s）：若是直接换成官方 webmssdk.es5.js，\n"+
			"  官方原版只暴露 frontierSign、需要浏览器环境，必须重做脱环境 shim 与 get_sign 包装才能用", w.jsFrom)
	}

	// get_sign 期望入参为“规范参数串的 md5”(X-MS-STUB)，返回 X-Bogus 作为 signature
	result, err := sign(goja.Undefined(), vm.ToValue(w.params()))
	if err != nil {
		return
	}
	w.Sign = result.String()
	return
}

func (w *Webmssdk) params() string {

	data := []map[string]any{
		{"live_id": "1"},
		{"aid": "6383"},
		{"version_code": 180800},
		{"webcast_sdk_version": "1.0.14-beta.0"},
		{"room_id": w.liveRoomRealId},
		{"sub_room_id": ""},
		{"sub_channel_id": ""},
		{"did_rule": "3"},
		{"user_unique_id": w.userUniqueId},
		{"device_platform": "web"},
		{"device_type": ""},
		{"ac": ""},
		{"identity": "audience"},
	}
	param := ""
	for _, item := range data {
		for key, value := range item {
			param += key + "=" + fmt.Sprint(value) + ","
		}
	}
	//fmt.Println(param)
	return md(strings.Trim(param, ","))
}

func md(str string) string {
	h := md5.New()
	io.WriteString(h, str)

	return fmt.Sprintf("%x", h.Sum(nil))
}

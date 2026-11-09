# douyin-protocol/im 私信协议层蒸馏 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 douyinlive `modules/im/protocol`（包名 `imapi`）蒸馏为 `douyin-protocol` 的公开 `im` 包，供 Go 消费者直接 `require`；`decode` 等业务留本地。

**Architecture:** `im` = 纯 wire 原语（手搓 protobuf 编解码）+ imapi 信封构造 + 消息类型码 + 注入式 HTTP `Transport`；刻意不认识业务。douyinlive 的 `imdecode`/`act`/`runtime`/`session` 改成依赖 `douyin-protocol/im`，业务（dao/model/IsSelf/FanOwner 富化）不动。

**Tech Stack:** Go 1.26+；依赖仅 `douyin-protocol/{abogus,fingerprint}` + stdlib（`regexp`/`encoding/binary`/`net/http`/`bytes`/`io`）。protobuf 为手搓 wire，**无 protoc**。

**Spec:** `douyin-protocol/docs/im-design.md`（§4 合规红线已确认；§6 决策①②已定）。本计划据该 spec 实现，并**修正 spec §2**：经实测 `imdecode.Message()` 内混有 `dao.FanOwnerByUID`/`imsession.IsSelf`/`RecordSecUID`（业务），故**不抽 `DecodeMessages` 进 im**——decode 整层留 douyinlive，im 只收纯协议原语。

## Global Constraints

- 依赖铁律：`im` 包**禁止 import 任何 `douyinlive/*`**；只可用 stdlib + `douyin-protocol/abogus|fingerprint`。由 Task 6 门禁测试钉死。
- 版本号：并入 `douyin-protocol`，发布 **v0.3.0**（次版本，符合协议仓纪律）。
- 行为等价红线：信封/签名/请求体的**字节输出**迁移前后必须逐字节一致（Task 6 golden 测）。
- 合规（来自 spec §4）：`im` 只提供"构造/解析/单次受控收发"能力，**不含采集策略**——不排轮询、不批量、不持凭据；发送须调用方受闸门控制。
- 落地方式：默认不动生产 im 运行时语义（纯"搬家+改 import"），douyinlive 侧灰度编译 + 现网对拍后才删旧 `imapi`。
- 工具链：build 用 `C:/Users/athlo/go/go1.27.3/bin/go.exe` + `GOTOOLCHAIN=local`；离线依赖走 `GOPROXY=https://goproxy.cn,direct`。

---

### Task 1: 创建 `im` 包 —— 搬纯协议原语

**Files:**
- Create: `douyin-protocol/im/pb.go`（源自 `modules/im/protocol/pb.go`，package 改 `im`）
- Create: `douyin-protocol/im/envelope.go`（源自 `modules/im/protocol/envelope.go`，package `im`，UA/CommonKV/BuildReq/BuildEnvelope/IsGroupConvID）
- Create: `douyin-protocol/im/msgtype.go`（源自 `modules/im/protocol/msgtype.go`，package `im`）
- Test: `douyin-protocol/im/envelope_test.go`

**Interfaces:**
- Produces:
  - `func Varint(fn int, v uint64) []byte`、`func Str(fn int, s string) []byte`、`func Bytes(fn int, b []byte) []byte`、`func Tag(fn, wt int) []byte`、`func Decode(b []byte) []Field`、`func First(fs []Field, num int) (Field, bool)`、`func All(fs []Field, num int) []Field`、`type Field struct{...}`
  - `func BuildReq(cmd1, cmd2 int, params []byte) []byte`、`func BuildEnvelope(cmd1, cmd2 int, f6 uint64, params []byte) []byte`、`func IsGroupConvID(id string) bool`、`const UA string`、`var CommonKV [][2]string`
  - 常量 `MsgDEFAULT..MsgGROUP_MESSAGE_FOR_AVCALL`（int，与源一致）

- [ ] **Step 1: 写失败测试（信封字节稳定 + pb 原语已知值）**

Create `douyin-protocol/im/envelope_test.go`:
```go
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
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./im/ -v`（在 `douyin-protocol` 目录）
Expected: FAIL — 包/符号不存在。

- [ ] **Step 3: 搬入三个源文件（改 package 为 `im`）**

从 `douyinlive/modules/im/protocol/{pb,envelope,msgtype}.go` 复制到 `douyin-protocol/im/` 同名文件，仅把 `package imapi` 改为 `package im`，**逐字节保留其余内容与注释**（字段号、UA、CommonKV 不动）。`envelope.go` 的 `Varint/Str/Bytes/Decode/First/All` 来自 `pb.go`，同包内直接可用。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./im/ -v`
Expected: PASS（三个测试）。

- [ ] **Step 5: 提交**

```bash
git add im/
git commit -m "feat(im): 搬入纯协议原语(pb/envelope/msgtype)+信封与 wire 单测"
```

---

### Task 2: `im.Transport` 取代 `util.HTTPClient`（解耦唯一网络依赖）

**Files:**
- Create: `douyin-protocol/im/transport.go`（源自 `modules/im/protocol/http.go` 的 `Post`，去掉 `douyinlive/util`）
- Test: `douyin-protocol/im/transport_test.go`

**Interfaces:**
- Consumes: `const UA`（Task 1）
- Produces: `type Transport struct{ HTTPClient *http.Client }`、`func (t *Transport) Post(ctx context.Context, host, path string, body []byte, cookie string) ([]byte, error)`

- [ ] **Step 1: 写失败测试（httptest 证明头正确 + 注入生效）**

Create `douyin-protocol/im/transport_test.go`:
```go
package im

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTransportPostHeaders(t *testing.T) {
	var got http.Header
	var bodySeen []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		bodySeen, _ = io.ReadAll(r.Body)
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	// 注入指向测试服务
	host := "http://" + srv.Listener.Addr().String()
	out, err := (&Transport{}).Post(context.Background(), host, "/x", []byte("body"), "sid=1")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "ok" || string(bodySeen) != "body" {
		t.Errorf("body/out mismatch: %q", out)
	}
	if got.Get("Content-Type") != "application/x-protobuf" || got.Get("Cookie") != "sid=1" || got.Get("User-Agent") != UA {
		t.Errorf("headers wrong: ct=%q cookie=%q ua=%q", got.Get("Content-Type"), got.Get("Cookie"), got.Get("User-Agent"))
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./im/ -run TestTransportPostHeaders -v`
Expected: FAIL — `Transport` 未定义。

- [ ] **Step 3: 写 `transport.go`（Post 用注入 client，无 util）**

Create `douyin-protocol/im/transport.go`:
```go
package im

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Transport 打一次 imapi 读接口（x-protobuf，仅 Cookie 鉴权）。
// HTTPClient 可选：注入带 Chrome-TLS 指纹的客户端抗风控；nil=内置带 15s 超时客户端。
// 库不持有、不落盘 Cookie；由调用方逐请求传入。
type Transport struct{ HTTPClient *http.Client }

func (t *Transport) Post(ctx context.Context, host, path string, body []byte, cookie string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, host+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("Accept", "application/x-protobuf")
	req.Header.Set("User-Agent", UA)
	req.Header.Set("Referer", "https://www.douyin.com/")
	req.Header.Set("Origin", "https://www.douyin.com")
	req.Header.Set("Cookie", cookie)
	client := t.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("imapi %s 返回 %d", path, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./im/ -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add im/transport.go im/transport_test.go
git commit -m "feat(im): Transport 注入式 HTTPClient 取代 util.HTTPClient,库不持凭据"
```

---

### Task 3: 合规/架构门禁 —— `im` 禁止依赖业务

**Files:**
- Test: `douyin-protocol/im/gate_test.go`

**Interfaces:** Consumes 整个 `im` 包。Produces：一条会让"im 误 import douyinlive"变红的守卫。

- [ ] **Step 1: 写门禁测试**

Create `douyin-protocol/im/gate_test.go`:
```go
package im

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// im 包必须零业务依赖：不得 import douyinlive/*；不得出现批量抓取入口关键字。
func TestImHasNoBusinessImports(t *testing.T) {
	fset := token.NewFileSet()
	files, _ := filepath.Glob("*.go")
	for _, fn := range files {
		if strings.HasSuffix(fn, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, fn, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		src, _ := os.ReadFile(fn)
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if strings.HasPrefix(p, "douyinlive/") {
				t.Errorf("%s imports business pkg %q", fn, p)
			}
		}
		// 采集策略红线：im 只提供单次能力，不含轮询/批量抓取入口
		for _, bad := range []string{"for range contacts", "PollLoop", "BulkScrape"} {
			if strings.Contains(string(src), bad) {
				t.Errorf("%s 含采集策略标识符 %q(应留上层)", fn, bad)
			}
		}
	}
}
```

- [ ] **Step 2: 跑测试确认通过（此时已满足）**

Run: `go test ./im/ -run TestImHasNoBusinessImports -v`
Expected: PASS（纯协议）。若有人日后误加业务 import → 变红。

- [ ] **Step 3: 提交**

```bash
git add im/gate_test.go
git commit -m "test(im): 门禁——禁 import douyinlive、禁含采集策略"
```

---

### Task 4: douyinlive 消费者改依赖 `douyin-protocol/im`

**Files:**
- Modify: `douyinlive` go.mod（`require github.com/athlon18/douyin-protocol v0.3.0`）
- Modify: `modules/im/protocol/{pb,envelope,msgtype,http}.go`（改为对 `im` 的薄转发或直接删除、更新 importers）
- Modify: 各 importer：`modules/im/act/{bubble,fold,markread,members,mute,send}.go`、`modules/im/decode/decode.go`（`imapi.X`→`im.X`）

**Interfaces:** Consumes `im.*`（Task 1/2）。Produces：douyinlive 内部引用统一走 `im`；`imdecode` 仍在本地做 dao/session 富化。

- [ ] **Step 1: 全量替换引用符号**

把 douyinlive 内所有 `imapi.` / `modules/im/protocol` 引用改指 `github.com/athlon18/douyin-protocol/im`：
- import 行 `"douyinlive/modules/im/protocol"` → `im "github.com/athlon18/douyin-protocol/im"`。
- 调用点：`protocol.BuildEnvelope` → `im.BuildEnvelope`；`protocol.Post(...)` → `transport.Post(ctx, "https://imapi.douyin.com", ...)`（`transport := &im.Transport{HTTPClient: util.PageClient(...)}` 每处一次）。
- `decode` 里 `protocol.Decode/First/All` → `im.Decode/First/All`（**保留 decode 对 dao/imsession 的调用，不动**）。

Run: `grep -rl "modules/im/protocol\|imapi\." modules service` 逐一确认改净。

- [ ] **Step 2: 删除本地重复真源**

删 `modules/im/protocol/{pb,envelope,msgtype}.go`（已搬到 `im`）；`http.go` 的 Post 由 `im.Transport` 取代。留 `modules/im/protocol` 目录仅剩必要本地胶水或整体移除。

- [ ] **Step 3: 编译**

Run（worktree，GOWORK 指向 im）: `go build ./...`
Expected: 成功。

- [ ] **Step 4: 迁移期现网/单测回归 —— 行为逐字节不变**

```bash
# 复用现有 act 单测(已覆盖 send/markread/fold/mute)：
go test ./modules/im/... -timeout 90s
```
Expected: 现有 im 相关单测全绿（信封字节、请求体、已读回执逻辑不变）。若某测试原断言 `imapi` 包名，仅改 import 路径、**不改期望值**。

- [ ] **Step 5: 提交**

```bash
git add modules/im go.mod go.sum
git commit -m "refactor(im): douyinlive 私信协议层改用 douyin-protocol/im;业务(decode富化/act/runtime)不动,逐字节等价"
```

---

### Task 5: golden 对拍 —— 迁移前后信封字节一致

**Files:**
- Test: `douyinlive/modules/im/protocol_golden_test.go`（新增，锁死迁移不漂移）

**Interfaces:** Consumes `im.BuildEnvelope/BuildReq`。

- [ ] **Step 1: 写 golden 测试**

```go
package im_test

import (
	"encoding/hex"
	"testing"

	im "github.com/athlon18/douyin-protocol/im"
)

// 固定输入下信封字节必须稳定（迁移=搬家不改形）。取一份已知 cmd/params 的期望 hex。
func TestEnvelopeGolden(t *testing.T) {
	params := im.Str(1, "conv-7455218347492327955")
	got := im.BuildEnvelope(2048, 10125, 1, params)
	want := goldenEnvelopeHex // 见 Step2：先用迁移前 imapi 输出生成，钉死
	if hex.EncodeToString(got) != want {
		t.Errorf("信封漂移！got=%x\nwant=%s", got, want)
	}
}
```

- [ ] **Step 2: 生成基准**

在**删除旧 imapi 之前**，跑一次临时用例打印旧 `BuildEnvelope(2048,10125,1,Str(1,"conv-7455218347492327955"))` 的 hex，填入 `goldenEnvelopeHex` 常量。迁移后本测试即证明逐字节一致。

- [ ] **Step 3: 跑测试通过** → **Step 4: 提交** `git commit -m "test(im): 信封 golden 锁死迁移不漂移"`

---

### Task 6: 发布 v0.3.0 + 文档

- [ ] **Step 1:** protocol README 包结构表加 `im` 行（职责：私信 imapi 协议底座；依赖：无/protobuf-less）。
- [ ] **Step 2:** `im-design.md` 状态改"已实现"；记 §2 修正（decode 留本地）。
- [ ] **Step 3:** `git tag -a v0.3.0 -m "feat(im): 私信协议层(纯 wire+信封+类型码+注入式 Transport)"`；`git push origin main v0.3.0`（**需用户点头推远端**）。
- [ ] **Step 4:** douyinlive go.mod 依赖锁 `v0.3.0`；`go build ./... && go test ./modules/im/...` 全绿。

---

## Self-Review 记录

- **Spec 覆盖**：§2 边界=Task1/2；依赖铁律=Task3；§3 迁移=Task4/5；§4 合规=Transport 不持凭据(Task2注释)+gate 禁采集(Task3)+发送仅受控能力(im 无批量)；§6 落地/版本=Task4 灰度+Task6。**§2 的 `DecodeMessages` 与 spec 有偏差**：实测 decode 混业务，改为"decode 留本地"——已在 Spec 引用段显式修正，非遗漏。
- **占位符**：无 TBD；golden 的 hex 由 Step2 现场生成（有具体生成方法，非空话）。
- **类型一致**：`Transport.Post(ctx,host,path,body,cookie)` 与 Task4 调用点签名一致；`BuildEnvelope/Str/Decode/First/All` 全程同名。

## 未决 / 风险

- 旧 `imapi` 与新 `im` 若并存一段时间=两份真源，须 Task4 一次性切净；建议同 PR 完成搬+改引用+删旧，不留中间态。
- 合规执行只在"能力面"；采集策略红线仍靠上层 douyinlive 的 im runtime 守（本计划不新增自动发）。

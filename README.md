# douyin-protocol

抖音网页版（`live.douyin.com`）的**协议算法层**，从 `douyinlive` 主仓蒸馏出来的独立 Go module。
目标是：其他项目**只引用、不复制**；算法随抖音改版时，发一个新版本，引用方改一处版本号即可。

> **公开仓库**（public）。可被任意项目按版本号 `go get` 引用。

## 包结构

| 包 | 职责 | 外部依赖 |
|---|---|---|
| `abogus` | `a_bogus` 签名（SM3 + RC4 + 魔改 Base64）。`AbSign(params, ua)` / `AbSignWithEnv(params, ua, windowEnv)` | 无 |
| `webmssdk` | 弹幕长连接的 `signature`（X-Bogus）：goja 执行内嵌 `webmssdk.js`，产出 `wss://` 地址 | `goja` |
| `fingerprint` | 浏览器画像池：UA / platform / 屏幕尺寸 / os+browser 参数，签名与实发参数同源 | 无 |
| `webcast` | `dy.proto` 生成的 protobuf 消息定义（package 名为 `pb`） | `protobuf` |
| `wsframe` | 弹幕帧层：PushFrame 编解码、gzip 解包、心跳/ack 构造、`ping-interval` 解析 | `protobuf` |

## 最小用法

```go
import (
    "github.com/athlon18/douyin-protocol/abogus"
    "github.com/athlon18/douyin-protocol/fingerprint"
    "github.com/athlon18/douyin-protocol/webmssdk"
    "github.com/athlon18/douyin-protocol/wsframe"
)

// 1) HTTP 接口签名：params 必须与实发 query 完全一致（顺序、编码都一样）
fp := fingerprint.FingerprintFor(roomID)
a := abogus.AbSignWithEnv(params, fp.UA, abogus.WindowEnv(fp.Platform, fp.ScreenWidth, fp.ScreenHeight))

// 2) 弹幕连接：先拿 wss 地址，再用 wsframe 收发帧
w, err := webmssdk.New(0).SetUa(fp.UA).SetBrowserPlatform(fp.Platform).Run(roomRealID, pushID)
// w.WssUrl / w.Sign

frame, err := wsframe.Decode(binaryMessage)
if frame.GetPayloadType() == wsframe.PayloadTypeHeartbeat {
    seq, payload := wsframe.HeartbeatReply(frame)
    out, _ := wsframe.Ack(seq, payload) // 写回连接
}
resp, err := wsframe.DecodeResponse(wsframe.Unpack(frame))
```

### 签名材料覆盖（可选）

`webmssdk` 默认使用**内嵌**的 `webmssdk.js`，零配置可用。
如果要保留"换文件不重编"的运维能力，启动时指定磁盘路径：

```go
webmssdk.SetMaterialPath("/path/to/webmssdk.js") // 传空串 = 回到只用内嵌基线
```

`SignMaterialInfo` / `MaterialBytes` / `RestoreMaterial` 三个接口配合可用于
"抓官方新版 → 比对指纹 → 归档 → 回滚 known-good"的监控链路。

## 版本与升级

语义化版本，以 git tag 发布：

```bash
go get github.com/athlon18/douyin-protocol@v0.2.1
```

引用方升级 = 改 `go.mod` 里的一行版本号。**不要在引用方复制本包源码**，否则会立刻出现两份真源。

发布新版本：

```bash
git tag -a v0.2.2 -m "feat: …"
git push origin v0.2.2
```

## 远端

仓库：<https://github.com/athlon18/douyin-protocol>（**public**）。`main` 与 tag `v0.1.0 / v0.2.0 / v0.2.1` 已推送。

因为是公开模块，引用方直接：

```bash
go get github.com/athlon18/douyin-protocol@v0.2.1
```

**无需** `GOPRIVATE`、无需 SSH 凭据——走公开代理即可。中国大陆若默认 `proxy.golang.org` 连不通，配一次国内镜像即可拉到本模块及其 tag：

```bash
go env -w GOPROXY=https://goproxy.cn,direct
```

（旧的 `GOPRIVATE=github.com/athlon18/*` + SSH 拉取写法是给私有仓用的，本仓已公开，不再需要。）

如果引用方暂时连不上网（或想锁死构建），可在那边 `go mod vendor`，或用下面 `go.work` 指向本机 clone——两种都只影响那一个仓，不改 tag 语义。

## 在引用方本地开发（go.work，不入库）

引用方 `go.mod` 只写版本号；要在本机改协议代码，用 `go.work` 把本地 clone 接进构建：

```bash
go work init . ./_external/douyin-protocol
go work edit -replace=github.com/athlon18/douyin-protocol@v0.2.1=./_external/douyin-protocol
```

`use` 之外还要一条**带版本号**的 replace：`require` 里的版本仍要参与 MVS 选版，
go 会去读该版本的 `go.mod`——模块还没推到远端时这一步必然失败（报 `git ls-remote` 读不到）。
所以 `go.mod` 里不要写 `replace`（指到不入库目录会让新 clone/CI/Docker 直接失败），
也不要把 `go.work` 入库。升级版本时，`go.work` 里那条 replace 的版本号要跟着改。

变更纪律：

- 补丁版本（v0.1.x）：只修 bug、补注释、加测试，不改签名输出。
- 次版本（v0.x.0）：加可选字段/新包，保持既有调用方不用改。
- 主版本（vX.0.0）：改函数签名、改 `dy.proto` 的字段号或类型。

`abogus` 与 `webmssdk` 各有一组**冻结基准测试**。任何一处算法漂移都会让测试变红；
要更新基准，必须先用真实房间复验，再改期望值。

## 合规边界

- 本包只提供"构造/解析"能力，**不含采集策略**：不持有 Cookie、不发请求、不做重连、不落库。
- 使用方必须遵守主项目 `docs/compliance.md` 的 R1：**不得主动构造带签名的请求去拉用户没触发到的数据**；
  采集应是被动抄浏览器已下发的帧。
- 不得用于绕过平台技术措施、批量抓取非公开数据或未授权商业化。
- 仓库中不含任何 Cookie、sessionid、token 或真实账号凭据。

## 来源

从 `github.com/athlon18/douyinlive` 蒸馏，行为与原实现保持一致；
`wsframe` 的帧形状（gzip payload、心跳 seq+1、客户端心跳 seq/service/type）均来自主仓实测结论。

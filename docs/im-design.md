# douyin-protocol/im 边界设计 & 合规红线

> 状态：**§4 合规红线已确认**；**§6 决策①有第二消费者=已确认、②落点=确认并入 `douyin-protocol/im`(Go 包，消费者直接 require，无需 HTTP/IPC shim)**。
> 仅差 §6 决策③(授权出实施计划) → 实现仍未开始。目标仓 = `douyin-protocol` 新增 `im` 包，版本拟 v0.3.0。
> 目的：把 douyinlive `modules/im` 的"私信协议层"蒸馏成可复用件；业务与采集策略留本地。
> 与 `live`（弹幕监听）不同，IM 触及**非公开私信**，合规要求更严，见文末红线。

## 0. 现状依据（实测，非推断）

`modules/im` 实际是一棵"协议 + 业务"混编树。按内部依赖把文件分三档：

| 档 | 文件 | 依赖 | 结论 |
|---|---|---|---|
| A 纯协议，可直接搬 | `protocol/envelope.go`（`BuildReq/BuildEnvelope/CommonKV/IsGroupConvID/UA`）、`protocol/msgtype.go`（类型码常量）、`protocol/pb.go`（`Field/Tag/Uvarint/Varint/Bytes/Str/Decode/First/All`） | 无 douyinlive 依赖 | 原样并入 `im` |
| B 先解耦再搬 | `protocol/http.go`（`imapi.Post`，仅耦合 `util.HTTPClient`）；`decode/decode.go`（`UserMessages/Message`，返回 `model.IMMessage` 且 import dao+session） | 见左 | http 改注入 HTTPClient；decode 拆"纯 wire 解码(入 im) / dao+session 富化(留本地)" |
| C 留本地（业务） | `runtime/`(poller/monitor/service 账号轮换·在线判定)、`session/`(档案)、`act/`(send/upload/markread/outbox/审批编排)、`history/`、`dao/model` 持久化、`notify`、`asr`(语音) | dao/model/util/api | **不搬**：这些是采集/存储/编排策略，正是"协议层不含业务"的边界外 |

## 1. 目标 / 非目标

**目标**
- `douyin-protocol/im` 提供：IM 请求信封构造、protobuf wire 编解码、imapi HTTP 传输、消息类型码、以及**纯解码** `字节 → im.Message`（不碰数据库、不碰会话富化）。
- 其他项目引用 `im` 即可对**自己已登录的账号**做私信收发的协议级操作。

**非目标（明确不做）**
- 不内含采集策略：不排轮询、不做账号池、不落库、不建联系人档案、不决定何时发/发什么。
- 不提供"批量抓别人私信"的能力（合规硬约束，见 §4）。

## 2. `im` 包边界（API 草案）

```go
package im // github.com/athlon18/douyin-protocol/im

// —— 传输（注入式，仿 live client）——
type Transport struct {
    HTTPClient *http.Client // 可选：注入 Chrome-TLS 指纹客户端抗风控；nil=内置带超时
    // Cookie 由调用方逐请求传入（库不持有、不落盘）
}
func (t *Transport) Post(ctx context.Context, path string, body []byte, cookie string) ([]byte, error)

// —— 信封 / protobuf 原语（A 档搬入，签名不变）——
func BuildReq(cmd1, cmd2 int, params []byte) []byte
func BuildEnvelope(cmd1, cmd2 int, f6 uint64, params []byte) []byte
func IsGroupConvID(convID string) bool
const UA = "..."
// pb 原语：Tag/Varint/Bytes/Str/Field/Decode/First/All

// —— 纯解码（B 档拆出：只解析，不查库）——
type Message struct { ConversationID string; SenderID uint64; TypeCode int; Content ...; Ts uint64 }
func DecodeMessages(raw []byte) ([]Message, cursor uint64, err error)

// —— 类型码常量（C 档外的纯常量搬入）——
```

**依赖铁律**：`im` 只允许依赖 `douyin-protocol/{abogus,fingerprint}` + `google protobuf`/stdlib。**不得 import 任何 douyinlive 包**（`douyinlive/dao|model|util|session`）。由一条门禁测试钉死（见 §5）。

## 3. 迁移与落地（默认关，不动生产）

1. **先拆 decode**：把 `decode/decode.go` 里"纯 wire→结构"的部分与"dao/session 富化"分离。前者进 `im.DecodeMessages`，后者留 douyinlive（把 `im.Message` 映射成 `model.IMMessage`）。
2. **搬 A 档**：envelope/msgtype/pb 原样迁入 `im`，douyinlive `modules/im/protocol` 改成薄转发（或直接引用），避免两份真源。
3. **http 改注入**：`im.Transport` 取代 `util.HTTPClient` 依赖。
4. **版本**：新增包属"次版本"，打 **v0.3.0**（顺带纠正：emoji/fansclub 上次误用补丁号 v0.2.1，见协议仓纪律）。
5. **douyinlive 侧**：`modules/im` 逐步改 `require douyin-protocol/im`；生产 im 路径**灰度**——先新引用并行、A/B 对拍无误再删旧码（同 live M2 的 flag-gated 做法）。

## 4. 合规红线（IM 专用，比 live 更严；实现与调用方都必须守）

> 继承主项目 `docs/compliance.md`，对私信域**加严**：

- **R1-IM 只读已下发/已授权**：只解析**已登录自己账号**会话里被推送的帧。**不得主动构造带签名请求，去拉用户没触发到的私信/别人会话**。（"导出私信"类批量抓取 = 禁用。）
- **授权边界**：任何 `im` 调用都针对调用者自身登录的 Cookie 账号；跨账号、第三方、陌生人爬取一律不在能力范围内（也不提供任何便利）。
- **发送红线（与现有一致且更严）**：发私信属 outbound 写——只在受控交互里**单条、限频、经批准**发送；守护/批量/无人值守/群发 **禁止**；默认**只落草稿**。库只提供 `send` 构造，是否发由上层受闸门控制，库不含自动发。
- **默认不出门**：采集/解析/规则判断本地闭环；不得为"看起来聪明"把私信内容外发大模型；确需 LLM 的（如代拟回复）须同时满足 可缺席 / 有上限 / 可审计（谁、为何、带什么出去、拿回什么）。
- **凭据与数据**：库**不持有、不落盘** Cookie/sessionid/token；调用方不得把凭据写进任何文件或提交。私信属非公开数据，存储须受限、可删。

## 5. 门禁（让红线"能失败"）

- **架构 lint**：`im` 包禁止 import `douyinlive/*`；禁止出现 `for range contacts` 式批量发送入口。用一条 `go test` 静态扫描钉死（仿 `TestNoOffBaseDataStoreAccess` / `TestNoHardcodedCompanyPolicy`）。
- **冻结基准**：`BuildEnvelope` / a_bogus(IM 场景) 各一组 golden 测试，算法漂移即红。
- **解码 golden**：真实抓样 wire 字节 → `DecodeMessages` 期望结构，字段级对拍。
- 更新基准前必须先用真实、已授权账号复验。

## 6. 待拍板（实现前须用户确认）

1. **要不要现在做**：除 douyinlive 外**有无第二个私信消费者**？没有 → 收益主要是"单一签名真源 + 可测"，可缓做（YAGNI）。
2. **落点**：确认并入 `douyin-protocol/im`（推荐，与 abogus/fingerprint 同仓同版本号），而非新独立仓。
3. **合规**：§4 红线逐条认可，尤其"只读已授权、禁批量、发送经批准"。
4. 若认可，下一步我出一份**实施计划**（拆 decode → 搬 A 档 → 注入 http → 门禁测试 → 灰度切换），仍默认关、不动生产 im 路径，验收通过才删旧码。

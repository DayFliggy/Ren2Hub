# 渠道接入类型与生命周期

渠道的整数编号是数据库与 API 合同，不因分类、名称或生命周期变化重新编号。定义源是 `constant.ChannelTypes`（`constant/channel.go`），名称投影由该注册表派生；`GetChannelTypeDefinition` 给出名称、分类与生命周期。分类用于管理入口与文档导航，不授予协议能力；实际端点仍由适配器、模型、插件绑定、路由配置和权限共同决定。

| 分类 | 渠道及现有编号 | 协议边界 |
| --- | --- | --- |
| OpenAI-compatible | OpenAI 1、Azure 3、OpenAI Max 6、OhMyGPT 7、Custom 8、AILS 9、AIProxy 10、API2GPT 12、AIGC2D 13、360 19、OpenRouter 20、AIProxyLibrary 21、FastGPT 22、LingYiWanWu 31 | OpenAI 兼容请求和各自已登记的端点；不能由分类推导为支持所有 Chat、Responses 或媒体请求 |
| Native providers | PaLM 11、Anthropic 14、Baidu 15、Zhipu 16、Ali 17、Xunfei 18、Tencent 23、Gemini 24、Moonshot 25、ZhipuV4 26、Perplexity 27、AWS 33、Cohere 34、MiniMax 35、Jina 38、Cloudflare 39、SiliconFlow 40、VertexAI 41、Mistral 42、DeepSeek 43、MokaAI 44、VolcEngine 45、BaiduV2 46、xAI 48、Submodel 53 | 供应商原生协议及已实现的转换；safe/strict 损失拒绝、模型映射和认证边界不变 |
| Gateway adapters | Sub2API 59、NewAPI 60 | 使用已有网关认证及多协议适配，不引入订阅账号池或第二套路由器 |
| Local/self-hosted | Ollama 4、Xinference 47、Advanced Custom 58、vLLM 62、SGLang 63 | Ollama 投影、已有兼容协议或明确的 Advanced Custom 路由；自托管类型不豁免 URL、权限或报价校验 |
| Media/task providers | Midjourney 2、MidjourneyPlus 5、SunoAPI 36、Dify 37、Coze 49、Kling 50、Jimeng 51、Vidu 52、Doubao 54、Sora 55、Replicate 56、Task Plugin 61 | 保留同步/异步宿主管线、不可变插件和冻结报价；Dify/Coze 的文本入口继续按各自适配器工作 |
| Retired/reserved | ChatGPT Subscription (Codex) 57 | 已退役，无可执行 API 类型和可路由端点；只识别历史记录并允许管理员显式删除 |

编号 0 为未知类型，28、29、30、32 等未分配值不代表已实现渠道；计数哨兵也不能作为渠道使用。生命周期 active 仅表示代码仍登记该类型，不代表供应商服务、账号或全部模型已获验证。详细端点与验证边界见[协议支持矩阵](../routing/GATEWAY_SUPPORT_MATRIX.md)。

## Codex 订阅渠道退役

类型 57 没有 ChatGPT 后端转发、OAuth 登录与凭据刷新、订阅模型发现、Wham 用量/额度重置及后台采集，对应管理路由不注册。历史行不会被自动删除、转换为 type 1，或以其中的 OAuth JSON 作为官方 OpenAI API Key。

管理 API 和统一选路在执行前拒绝退役类型；不能通过编辑其他字段、复制、批量操作、测试、模型抓取或同步绕过。旧行仍可读取，列表明确显示退役，管理员可以通过原删除操作清理。退役行不贡献 manifest、`/v1/models` 或客户端配置的可用端点，不能发送上游请求。

迁移由管理员显式完成：记录需要保留的模型映射与配置，删除旧渠道，使用官方 API Key 新建 OpenAI 类型 1，并重新核实模型、价格与权限。不要导出或复用旧 OAuth 内容。升级不修改既有历史行，也不删除钱包、付款或业务订阅数据。

## 标准 OpenAI Codex 与客户端导出

`gpt-5-codex`、`gpt-5.1-codex` 等名称是模型标识，不是 type 57 的别名。标准 OpenAI 渠道继续使用 `APITypeOpenAI`，保留自身 Chat、Responses、Compact 和 WebSocket 路径；具体模型是否接受某个请求由现有模型能力与供应商响应决定。

`GET /api/token/:id/client-config?format=codex|pi` 继续从统一 manifest 生成权限过滤配置。Codex 使用 OpenAI Responses TOML，Pi 使用 Chat Completions JSON；均引用 `REN2HUB_API_KEY`，不导出真实凭据，也不依赖订阅渠道、OAuth 或供应商窗口。参数保留、状态归属、路由、重试和账务继续由标准 OpenAI 路径负责。

## Sub2API 余额刷新

类型 59 的管理员余额刷新使用渠道 `base_url` 下的 `GET /v1/usage`，以渠道 Key 发送 Bearer 认证，并沿用渠道代理与 TLS 设置。优先读取 `remaining`（明确的零不回退），字段缺失时读取 `balance`；缺失、非数值、非有限值、`isValid=false` 或没有任何日/周/月限额的无限订阅均报错，保留已存余额与更新时间。钱包负值是合法的余额，不把它自动解释成无限额度。

余额只有成功写入数据库后才向管理员返回成功；所有渠道共用 `Channel.UpdateBalance` 的错误合同，批量刷新收集失败并返回错误，后台刷新记录失败。这是管理员主动查询的余额信息，不引入供应商额度窗口采集，也不影响本地钱包计费来源。多 Key 渠道继续拒绝余额查询。

## 将来接入供应商窗口的边界

代码中没有供应商用量窗口或 provider-usage 接口。新增供应商窗口必须单独立项并复用已有合法认证，明确稳定凭据与端点身份、unknown/stale/error 语义、取消与退避、权限交集和脱敏投影。观察结果不能成为资金预留、余额、路由健康或额度自动重置的第二份状态来源。

NewAPI 保留订阅渠道。本地退役是产品边界决定，属于刻意不吸收，复评条件见[同步账本](../upstream/SYNC_LEDGER.md#结构差异)。

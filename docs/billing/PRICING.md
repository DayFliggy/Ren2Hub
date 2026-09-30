# 明确单价与冻结报价

当前账务安装格式为 4，价格合同为 3，计算器为 `billing-v3`。只接受空库初始化或匹配 `billing_schemas` 标记的数据库，不迁移或清空旧账务记录。价格、倍率、金额和非 token 数量使用 Decimal 字符串，quota 使用 int64 十进制整数字符串；固定 `500000 quota = 1 USD`。

价格身份为 `(scope_kind, channel_id, model)`，每个身份独立维护活动版本。`channel_model` 要求正整数渠道 ID，报价倍率固定为 `"1"`；`global_model` 的渠道 ID 为 0，可应用冻结的渠道售价倍率。查价先取精确渠道价，再使用明确发布的全局价。渠道倍率与手动路由 `max_ratio` 均为 Decimal 字符串，范围 `(0,1000]`、最多 18 位小数。缺价、未知单位或请求所需维度无价格，在预留前失败。

模式为 `unit_price`、`expression` 或显式 `free`。可组合一次请求的 `flat_fee`、固定单价和结构化阶梯：`volume` 按完整数量选档，`graduated` 分档累计，有限末档超量报错。发布立即切换活动指针，拒绝未来 `effective_from` 和已到期 `effective_to`；当前没有排期切换器。创建报价检查窗口，历史结算按冻结时间重放。

| 数量 | 单位 | 实际计量边界 |
| --- | --- | --- |
| `p,c,reasoning` | `per_million_tokens` | 普通输入/输出、输出中的推理 token |
| `cr,cc,cc1h` | `per_million_tokens` | 缓存读取、默认/5 分钟写入、1 小时写入 |
| `img,img_o,ai,ao` | `per_million_tokens` | 图片、音频输入与输出 token |
| `img_cr,ai_cr` | `per_million_tokens` | 上游明确报告的媒体缓存 token，不估算占比 |
| `calls` | `per_call,per_thousand_calls` | 明确调用次数 |
| `images` | `per_image` | 已确认交付图片数，预留用最终请求数量 |
| `seconds` | `per_second,per_minute,per_hour` | 声明合同的媒体时长，换算保持时间维度 |
| `characters` | `per_character,per_million_characters` | 明确 TTS 字符合同，不折算 prompt token |
| `credits` | `per_credit` | 声明并确认的 credits，还须满足预留条件 |
| `tool:<类别>` | `per_call,per_thousand_calls` | 适配器确认的完成调用数，声明工具不等于调用 |

计算器识别单位不代表所有协议或来源已支持该产品。任务 schema 另受冻结字段的单位和来源约束。原始用量按各上游缓存语义归一化，完整输入上下文单独作为 `len`；独立收费的子类从基础输入/输出剔除，避免重复计费。媒体缓存不得超过对应媒体量及通用缓存总量，图片与音频缓存之和也不得超过通用缓存。缺失和明确零分别保存。

单价单位和 schema 数值字段单位只接受 `pkg/billingunits` 注册表中的 token、call、image、second/minute/hour、character、credit、count，以及千次调用、百万 token、百万字符；未注册单位在报价构造和发布前拒绝。数量与单价单位必须属于同一维度，时长只在秒、分钟、小时之间换算，不能跨维度；schema 价格单位必须与字段单位完全一致。

图片与音频是互斥的模态：两者的输入 token 之和不得超过输入总量（上游输入不含缓存时加上缓存读写），图片、音频输出与 reasoning 之和不得超过输出总量。超出时计算直接失败，不把剩余文本量夹到零后继续收费；缓存与模态可以重叠，合法缓存集合不受此限制。

Realtime 用量在累加和计价前经 `NormalizeRealtimeUsage` 校验（`service/billing_realtime.go`）：计数须非负且不超过 MaxInt32，给出的 `total_tokens` 须等于输入与输出之和，缓存、图片与 reasoning 细分不得超过所在方向。方向总量为正时，文本、音频、图片细分之和超过总量即失败；总量为零时，完整且非估算的上游观察按明确零处理，细分非零即失败，本地估算或不完整观察才由细分补出总量。缺失的文本细分取方向总量减去音频与图片细分的余数。

`flat_fee` 只覆盖其对应的常规产品计量。无其他单价的纯固定费可以忽略已确认的 calls、images、seconds、characters、credits 事实，带固定费时 calls 无须另行定价；未定价的 `tool:*` 或其他未知数量仍以缺价失败，表达式加固定费同样不能吸收未定价工具调用。

全部金额和表达式使用 Decimal，已移除 Expr VM/float64 结算入口。`v1:` 结果为微美元，`p * 2.5 + c * 10` 表示每百万输入 2.5 USD、输出 10 USD；`v2:` 使用冻结任务 schema，结果直接为 USD。数值属性使用 `attr_num`，时区日历及 UTC 偏移在报价时冻结。语法、资源限制和除法精度见[表达式契约](../../pkg/billingexpr/expr.md)。价格预览 `PreviewBillingPrice` 用 `common.DecodeJsonNumbers` 读取请求体，数字保留为 `json.Number` 原文并拒绝多个 JSON 值，解码后仍执行必填字段校验。

`ChargeResult.base_usd` 是分项合计；`adjustments` 记录渠道倍率、调整前金额、增减金额与调整后金额，`total_usd` 是调整后总额。最后乘 500000 并只舍入一次，正金额最低 1 quota，明确零为 0。无效或超过单请求 MaxInt32 的结果进入对账，不截断扣款。

发送前保存 Quote、作用域、版本及哈希、来源证据、最终发送安全属性、固定事实、冻结时间及报价哈希。每次尝试有独立 ID；首次使用渠道时锁价，换渠道建立另一份报价，返回已尝试渠道复用原价。恢复快照管理累计预留目标，结算不重新取价。未知接受或实际用量不明保持预留。文本未给输出上限时的 8192 token 仅是标记为估算的预留依据，不是费用上界。

## 自动来源与人工售价

只有 Sub2API 主源与 AxonHub 补缺源。精确身份优先，同一候选按来源顺序核对完整价格、所需维度和档位，不拼接字段。人工价优先，自动更新仅管理全局作用域。多个供应商同名时，即使同价也拒绝裸名，保留限定 `provider/model`。资源包装、日期及 reasoning 别名按既有单向规则匹配，不猜模型家族或兄弟日期版本。

| 来源 | 默认地址 | 来源单位 |
| --- | --- | --- |
| Sub2API | `https://raw.githubusercontent.com/Wei-Shaw/model-price-repo/main/model_prices_and_context_window.json` | token 字段为 USD/token，精确乘 1000000 |
| AxonHub | `https://raw.githubusercontent.com/ThinkInAIXYZ/PublicProviderConf/refs/heads/dev/dist/all.json` | cost token 字段为 USD/百万 token |

PublicProviderConf 是参考目录。媒体能力标签或 `cost.input/output=0` 不能证明按图、按秒、TTS 或视频免费。只接收已定义字段、单位与用量语义，缺失不当作零；拒绝原因见[解析器说明](../../pkg/autopricing/README.md)。

自动 token 价格生成冻结表达式，默认只支持 `default`。源数据对全部已收费维度给出完整显式档位价时才加入 `priority/flex`；不生成 ×2/×0.5，不把 priority 当 fast/ultrafast，不用普通价补缺。上下文条件来自原字段，冲突阈值拒绝。`support.dimensions/service_tiers` 冻结能力，未知档位不得落入默认分支。请求档位用于锁价，响应实际档位单独审计，不追溯换价。

自动版本冻结 `provenance.kind=source`、不可变 `snapshot_id`、`parser_version` 和 `fields[{target,source_path,raw_value,source_unit,conversion}]`，同时冻结 `origin/source_model/source_hash`。原文按来源与 SHA-256 归档于 `BillingPricingSnapshot`，`BillingPricingSource` 保存活动指针；刷新不覆盖历史原文，取回原文先核验 SHA。自动发布的证据对象由服务端从归档快照构建：重新核验正文 SHA 并重新解析原文，待发布版本（含字段证据）的哈希必须与该快照的解析结果一致，被快照拒绝的身份不能发布；来源不符、正文损坏或计划不一致都拒绝，不能只凭哈希字符串发布。

人工发布必须提供 `provenance.kind=evidence` 和完整字段证据，或 `kind=retail` 与非空 `retail_reason`。retail 不带源快照及字段证据，不能将运营售价伪装为来源事实。参考目录导入只形成草稿，保存须明确人工依据；恢复全局自动价携带当前 `expected_version`。

启动仅在缺少来源记录时安装[双源离线快照](../../service/pricingdata/README.md)，不联网、不覆盖已有快照。默认每 30 分钟检查，Root 可设 5–1440 分钟或关闭计划，仍可手动检查。Sub2API 校验独立 SHA，AxonHub 使用条件请求与正文 SHA；正文上限 16 MiB，每源超时 40 秒，哈希请求 10 秒。

单源下载或校验失败保留旧有效快照，并继续其他源。状态为 `bundled/current/cached/unavailable`，报告快照 ID、解析器版本、归档数及错误。所有源无有效快照时返回目录错误，不批量清空既有价格、不启用默认价。有效目录无法提供某模型完整价格时写入 `version_id=0,origin=unavailable`，阻断新请求借别名取得旧价；历史 Quote 仍可重放。来源视图和活动价格均参与并发校验，哈希未变也核对未完成发布。自动发布与活动版本哈希相同时复用原版本，只更新活动指针的来源归属，不新增版本；人工发布始终新建版本（`model/billing_pricing.go`）。当前没有旧种子、旧倍率或历史来源迁移路径。

## 音频、工具与任务边界

`metering` 是显式冻结的物理计量合同，仅用于无任务 schema 的 `unit_price`，不能混合 token 收费：

| 请求 | 数量来源和方法 | 适配边界 |
| --- | --- | --- |
| `speech/characters` | `request_fixed/unicode_codepoints` | 最终发送文本 Unicode 码点数，预留和结算使用同一值 |
| `speech/characters` | `completion_fact/provider_usage` | 当前 MiniMax 字符回执，须声明正数 `reservation_quantity` |
| `transcription/seconds` | `completion_fact/provider_duration` | 当前 OpenAI 音频兼容适配的 STT/翻译时长回执，须声明正数 `reservation_quantity` |

`reservation_quantity` 仅是经审阅的预留量，不补实际缺失，也不是费用上限。TTS token 计费要求能报告实际用量的 OpenAI SSE；二进制音频本身不提供 token 事实。STT token/时长计费要求可携带用量的 JSON 响应格式，缺失事实保持对账。Embedding 的 `usage_scope=embedding` 限制请求类型及输入用量。

以下“可收费”均以该请求存在已发布且完整的价格或计量合同为前提，不代表目录中的模型默认可收费；最终消费仍需完整实际事实或明确的 `request_fixed` 合同。

| 产品 | 可收费路径 | 边界 |
| --- | --- | --- |
| 文本与 Embedding | token、缓存读写、reasoning 及明确覆盖的媒体细分；固定费、单价、结构化阶梯或 Decimal 表达式 | Embedding 使用 `usage_scope=embedding`；缺失、估算或空 usage 不当作实际零值 |
| 工具 | web search（含 search-preview 模型）、file search、Google search、图片生成及已配置函数维度，按适配器确认的完成调用计数 | 声明工具即要求对应 `tool:*` 价格，缺价在预留前拒绝，不退回基础 token 价；`ObserveBillableToolCall` 按 ID、call ID 与输出索引别名去重，item.done 与终态快照只计一次；身份全缺或别名的类型、名称、ID 冲突时整次请求的工具用量无效，计价失败并保留预留进入对账 |
| Realtime | 已实现的累计 token/媒体用量；纯固定请求价覆盖常规 token 维度 | 只允许客户端执行的 function 声明；其他工具类型、已配置 `tool:<name>` 价格的函数、需要 `tool:*` 维度的请求，以及顶层 `input_audio_transcription` 或嵌套 `audio.input.transcription` 均以 `missing_usage_contract` 拒绝；会话中不能改变模型 |
| 图片 | 图像 token/缓存细分，或已确认交付的 `images` 数量 | 同一版本不能同时按 `img_o` 与 `images`/`tool:image_generation` 收费，发布前拒绝 |
| 视频、音乐与其他任务 | 通过插件合同的固定 calls 或 schema 数量价；冻结插件版本、计量字段与价格，按成功终态事实结算 | schema 中 `estimate_only` 字段只用于预留，不能定义最终费用 |

以下产品当前拒绝或无法发布，不能用固定费、示例零值或参考目录价格绕过：

- Code interpreter、computer use、Google Maps 工具：请求声明即在预留前以 `missing_usage_contract` 拒绝。自动来源中的地图查询价只作为未定价能力的字段证据保留。
- Vidu credits 按量价：credits 只在完成时提供，没有可信预留估算，含 credits 的工厂价格被拒绝，应改用不带 usage_schema 的 calls/per_call 价格。
- 存储量乘时间：注册表没有对应单位，无法发布。
- Realtime 单独收费的工具与输入转写附加产品（见上表）。

没有可信实际 usage 的视频插件不按请求时长结算。任务产物的存储与计费是不同边界：`service/task_artifact_store.go` 只有 `disabledArtifactStore`，`GetTaskArtifactStore()` 始终返回禁用实现，`Persist`/`Serve` 返回禁用错误；配置结构、代理下载或签名访问都不能说明已实现对象存储持久化。

## 公开展示与审计

`GET /api/pricing` 返回 `model_name` 与 `offers[]`。每份 offer 独立包含计价模型、公开版本、适用条件、渠道调整、服务端 Decimal 投影价格和可用状态。请求别名可与计价模型不同；相同版本不同倍率保留不同 offer。`offer_id` 是不透明标识，不公开渠道 ID 或证据正文。页面区分版本单价与调整后售价。

Root 对账的 `audit` 重放冻结证据，`upstream_cost` 保存不可变尝试成本。客户实收来自账本事件，区别于舍入前 `total_usd`；全部尝试成本明确且实收确认后才报告利润，否则为 `null`。登记成本不改钱包，重试保留事件 ID。日志展示作用域、版本及报价哈希、基础费用与调整、请求/实际服务档。

指标包括版本、尝试与来源统计，`operational` 是当前进程累计指标，不能当持久化资金统计。普通金额展示最多 3 位小数，编辑、提交、原文下载和内部计算保留完整精度。字段、预览与冲突规则见[计价接口](../../pkg/billing/API.md)。

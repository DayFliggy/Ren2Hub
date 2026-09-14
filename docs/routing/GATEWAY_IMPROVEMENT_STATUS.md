# 网关改进实施记录

基线：Ren2Hub `d1a69b1d5fe30dddbac2dca4a0d42cc8e05ba8b7`。参考：sub2api `4726bdd08b6201d426a80529b79be123a4008d20`、AxonHub `46687ed6337d42c9cb4f3c9ccad14732301ba451`、new-api `74629e29f83f506bb523c5542aa851947bd423eb`。

更新：2026-09-14。本地实现和回归已整理为后端提交 `02b0a180a73b9318ebf9f4eac38e78d81735df21`、控制台提交 `638cfcc46e2890e8fedc9332a95edb257a9114f0`；尚未发布或部署，未调用付费供应商。下表区分已运行回归与证明边界，不能以代码存在或容器健康代替完成条件。测试路径相对于 Ren2Hub，基线不是本次实现提交。

原六阶段改动跨越同一请求、适配器和账务契约，实施期间未保留可独立验收的阶段提交。本次按后端与协议测试、控制台、文档三个依赖批次集成；不将事后文件分组称作原六阶段的独立交付。

| 项目 | 当前处置 | 已取得证据与剩余范围 |
|---|---|---|
| F01/F07 探测与旧结果 | 规划只读，尝试认领/续期/释放，epoch 与 probe 隔离旧结果 | `service/route_health_attempt_test.go`、`controller/relay_route_test.go` 的取消、超时、并发和旧成功回归通过 |
| F02 能力与计划 | 唯一宿主登记；预览和执行共用计划；快照幂等发布，并发首次创建受 CAS 保护；合同 v11 登记专有音频路径/流式边界及 Vertex Gemini 的 Claude 入站；修复 NewAPI 枚举遗漏和 Gemini 模型 URL 映射 | 400 个文本请求场景、226 条标准响应路径、26 条向量路径；云基础 SDK 80 条、自定义路由 64 项、其余兼容适配器 448 条、旧适配器 108 项及 Vertex 开源模型 64 项通过 |
| F03 工具身份 | 保留显式 ID，缺省按历史关联，歧义拒绝；Gemini 参数与 ID 累积；移除自动签名绕过标记；Vertex 四入口执行相同无 ID 投影策略 | 云 40 个工具往返、40 个并行双工具往返及 12 个歧义历史发送前拒绝通过；strict 拒绝可识别 ID 损失，所有策略拒绝歧义；SDK 证明范围见支持矩阵 |
| F04/F12 流与结算 | 统一终止与交付；未写出转换事件不能提交前导或启用心跳；Realtime 按 response/item 隔离估算和重复事件；JSON usage 与下游写失败统一处理；任务终态和账务意图同事务 | 云 40 条中断、JSON 57 个用量/交付场景及 10 个控制器退款/健康回归通过；文本、媒体、音频、Realtime 官方 SDK 已重验；三库来源恢复 36 场景通过 |
| F05/F06 候选、预算、归因 | 不同候选扫描与 3 次发送分离；保留终态上游错误；本地失败和下游写入失败不罚供应商；关闭 AWS SDK 内部重试；Coze/Replicate 已接受的创建禁止重放 | 原候选、AWS 单次发送、Coze 创建/查询故障和轮询取消通过；Replicate 5 个 HTTP 场景验证上传晚于最终校验/预扣、最终认证头、损坏上传响应和生成结果未知不重放；任务和 Midjourney 使用统一接受状态决策，真实供应商幂等查询确认未验证 |
| F08/F13 损失与零值 | 默认 safe，Token allow/strict；旧投影、RawMessage/any、Schema 损失诊断；公共输出上限保留显式 0；Ollama 工具约束纳入共享登记，工具历史拒绝歧义 | Dify 策略、四协议 20 个输出上限、seed=0、Ollama num_predict=0、工具控制/大整数参数/同名多轮和预览策略回归通过；推理/搜索计划 11 场景、30 个 HTTP 请求及媒体计划 48 个 HTTP 请求通过；厂商扩展按登记和诊断处理，不能据此推导所有模型特性可转换 |
| F09/F10 准入与分配 | 用户/渠道租约、有界队列、成功预留、共享 RPM/TPM、Priority 内负载档；最终发送体决定文本预算、图像数量/品质及 TTS 输入预扣，限流通过后才预扣 | 四协议预算及图像覆盖回归通过；新增 MiniMax/火山 metadata 文本覆盖、额度不足 4 个实际 HTTP 回归；图像数量和音频输入估算不宣称支持 TPM |
| F11 状态归属 | 稳定凭据、端点版本、传输摘要及元数据绑定；Suno 续作与视频下载锁定原任务身份；轮询返回 ID 必须匹配；引用、签名及 TTL 边界保持共用检查 | 不透明状态及三库迁移/保留已通过；Suno 续作 5 组、视频下载/轮询 22 个 HTTP 场景，Google 下载复审 39 场景；Google Files URI、gs:// 与 Chat 嵌套 file_id 共用归属校验 |
| 控制台 | 准入、运行状态、继承、转换策略、预览 requirements/plan/diagnostics 及投影标识 | 全套 584 条 Vitest；19 条预览定向、四组 API fixture、四组真实准入及四组真实样本预览通过，已检查截图；普通用户权限和诊断不泄露通过 |
| 运维 | Root `/api/gateway/metrics`，分离进程计数与数据库积压 | 排队取消、失败 latch、积压条件及数据库失败回归通过；指标不含高基数身份标签 |

默认规则：转换 safe；用户并发 10，累计等待 5 秒，等待队列 20；实际发送最多 3 次，单轮最多扫描 64 个不同候选；状态绑定元数据保留 30 天，不存储会话内容。管理员覆盖不在重启时重置。

F01–F13 的源码与正向回归均在后端提交 `02b0a180a73b9318ebf9f4eac38e78d81735df21` 中；下列是定位入口，不是完整测试清单。各项本地回归通过，跨数据库、官方 SDK 和多实例的外部夹具证据见[验证说明](GATEWAY_VALIDATION.md)。

| 缺陷 | 正向回归入口 | 已验证行为 |
|---|---|---|
| F01 | `service/route_health_attempt_test.go`、`controller/relay_route_test.go` | 探测到期重取、取消释放、规划不认领未发送渠道 |
| F02 | `relay/capability_matrix_test.go`、`relay/capability_response_test.go` | 400 个请求场景和 226 条响应路径，登记与实际转换/拒绝一致 |
| F03 | `relaykit/relayconvert/conversion_plan_test.go`、`router/gateway_sdk_tools_test.go` | 显式 ID、无 ID 历史、同名和并行工具往返，歧义拒绝 |
| F04 | `relay/helper/stream_boundary_test.go`、`relay/channel/openai/stream_outcome_test.go` | 畸形、EOF、传输中断不得制造正常完成；已交付部分结算 |
| F05 | `router/gateway_candidate_scan_test.go`、`controller/relay_scan_test.go` | 四入口均可越过前三个满载候选；只发送一次，账务一次，扫描有界 |
| F06 | `service/route_health_attempt_test.go`、`controller/relay_attempt_test.go` | 本地/客户端失败不罚供应商，已交付上游故障仍更新健康 |
| F07 | `TestRouteAttemptHealthLateSuccessAndShortFailureKeepNewerCooldown` | 旧成功与较短失败不能覆盖更新冷却 |
| F08 | `relaykit/relayconvert/conversion_plan_test.go`、`router/gateway_semantic_sdk_test.go` | 搜索/custom tools 按原生、保真、显式损失或拒绝处理 |
| F09 | `service/gateway_admission_test.go`、`service/gateway_multiprocess_test.go` | 成功名额预留、失败释放、跨进程限制及 Redis 故障边界 |
| F10 | `service/route_selector_distribution_test.go` | Priority 内负载档与稳定权重，手动 Sticky 不越合法层 |
| F11 | `router/gateway_state_test.go`、`router/gateway_opaque_state_test.go`、`router/gateway_semantic_sdk_test.go` | 同 Token/凭据/端点续轮，未知和跨 Token 拒绝，签名与嵌套文件不绕过 |
| F12 | `TestResponsesStreamLifecycleAndOutcome`、`router/gateway_sdk_test.go` | item/part 生命周期、最终值和官方 SDK 流累积 |
| F13 | `TestPortableSearchAndExplicitZeroAcrossProtocols`、`relaykit/relayconvert/output_limit_test.go` | top_p、seed、输出上限的显式 0 与缺省值保持区别 |

自动路由按 Priority、10% 负载档和 RequestID 稳定权重排序。诊断已删除任意相加的 Total，保留原始负载、权重和性能观测，区分未知与真实 0。手动顺序及既有权重语义保留。

已取得的集成证据：

- 官方 Python/TypeScript SDK 每语言 32 条文本 JSON/SSE、16 轮工具往返、16 条中断流，以及 Responses 状态和前导失败切换；新增 Ollama 模拟上游。范围见 [支持矩阵](GATEWAY_SUPPORT_MATRIX.md)，不是实际供应商认证。
- 官方 OpenAI Python/TypeScript SDK 每语言 7 条媒体正常路径与 1 条图片中断路径，覆盖 Embedding float/base64、图像 JSON/SSE、TTS 二进制及 STT JSON/multipart SSE。音频 DTO 保留 `stream` 的缺省、false 和 true。
- Azure、AWS Claude Bearer/SigV4、Vertex Claude/Gemini，各语言四协议 JSON/SSE 共 80 条基础路径通过；AWS 验证真实 SDK 签名和二进制事件流，Vertex 使用 API Key 模式。
- AdvancedCustom 各语言 22 条成功路径和 10 条发送前拒绝通过，验证四协议、模型映射、路径、查询参数和四种认证配置。官方 Realtime SDK 各语言 4 场景验证交错响应、工具、断流和关闭码，使用 SDK 的迭代器/事件接口。
- 专有音频官方 OpenAI SDK 各语言 5 条成功、2 条发送前拒绝、1 条二进制截断通过；正常内容校验字节，截断要求客户端抛出传输异常。消费来源与恢复三数据库 36 场景重新通过。
- SQLite、MySQL 5.7.19、PostgreSQL 16 账务共 36 场景，覆盖重复终结、资金/Token/日志/统计失败、取消、Playground、免费消费、并发消费和退款恢复。PostgreSQL 使用生产 simple protocol。
- `TestGatewayStartupMigrations` 在三库运行完整迁移两次、关闭重连，验证 safe 一次性升级和管理员覆盖保留；`ChannelInfo` JSON 编码兼容已修复。
- `TestGatewayMultiProcessAdmission` 覆盖用户名额、共享渠道、两类取消、成功预留、热更新、排队后配置变化、Redis 故障与进程退出；`TestGatewayApplicationInstances` 用两个真实二进制验证 TokenAuth、跨协议容量和数据库计费。
- 三库并发首次快照发布通过；同内容重复发布不增加活动版本，避免第二实例启动使第一实例失效。

各批次主模块、独立 RelayKit、SDK、静态检查、构建与真实双应用结果见 [验证操作说明](GATEWAY_VALIDATION.md)。本地试用后端仍为 v9；本轮文件或构建产物不能视为该进程已更新。

2026-09-13 新增 `relay/helper/outbound_budget_test.go`、`router/gateway_budget_test.go` 和 `router/gateway_opaque_state_test.go`。预算检查接入 HTTP 与 AWS SDK 的最终请求体，不读取或消耗真正发送体的游标。`route_decision` 增加预算数值、估算来源和拒绝来源，使用 AttemptOutcome 区分本地准入与供应商失败。

同日继续新增 `router/gateway_transport_test.go`、`router/gateway_oauth_state_test.go`，并覆盖压缩输出用于后续 Responses。实际认证不能通过覆盖换成另一 Key，身份不变的覆盖仍可用；新 `transport_id` 不记录明文，旧记录缺少摘要时要求新会话。移除了自动 Gemini 签名标记及无效控制台开关，SDK 工具回归已在该变化后重新通过。三库迁移不会伪造旧绑定的传输身份。

v8 补充：已知 SDK 遥测头不再影响传输归属，Host/租户变化仍拒绝。Dify 转换不再执行文件上传；上传使用最终覆盖后的 user、可取消的共享 HTTP 传输和已验证的认证，准入失败时无上游连接。上传或生成接受状态不明时禁止重放，上传错误不再静默丢弃图片。控制台布尔设置补齐可见标签，桌面/移动与明暗主题截图已检查。

本地收尾与发布边界：

1. Nova、Coze、讯飞官方 SDK 共 54 项、Codex 32 项及 Vertex llama/-maas 64 项已通过；Vertex 使用本地 JWT 换取验证服务账号路径，修复缓存身份，API Key 不支持的路径提前拒绝。提交已接受但数据库持续不可写时不能盲目重放，未提供幂等确认的供应商保留结果未知错误。
2. 媒体形态、推理控制、配置型搜索及私有文件 URI 已有计划与 HTTP 证据；官方 SDK 语义套件两语言共 116 场景通过，每语言 34 条成功、24 条拒绝。拒绝路径核对上游、预扣会话、钱包、Token、日志全部零变化；Google Python 图片编码问题已修复并由 12 条跨协议回归覆盖。
3. 主模块全测与 vet 已在最后的文件归属、图片编码修复及候选 HTTP 回归后通过；独立 RelayKit 全测/vet/build 通过。控制台产品代码未变化，复用已通过的 584 条 Vitest 和桌面/移动、明暗主题验收。提交后构建和双应用结果以[验证说明](GATEWAY_VALIDATION.md)的最终集成记录为准。
4. 本地提交及 F01–F13 映射已形成；发布时相对届时生产 SHA 复核[迁移与发布准备](GATEWAY_RELEASE.md)。本机未安装 Docker，最终 SHA 镜像冷构建未执行；Linux 交叉编译、三库升级夹具、重复启动和恢复结果不能替代镜像检查。

F01–F13 已有本地处置与正向回归，支持矩阵区分基础路径和专项特性。原计划要求的六阶段独立提交未按原顺序保留；最终镜像和生产验收尚未执行，因此不宣称原六阶段的全部交付条件已经满足。后续供应商认证应按明确支持矩阵执行，不把本地模拟结果扩张为所有模型特性的兼容承诺。

2026-09-13 任务提交补充：10 个任务适配器只返回响应数据，控制器在任务与消费快照同一事务提交后发送成功。201/202、持久化失败不泄露成功 ID、结算失败保留任务并恢复一次、明确拒绝可推进、接受不明不重放、RPM 先于预扣均有真实本地 HTTP 回归。Task 错误桥保留错误来源与禁止重试规则，路由日志复用逻辑请求的扫描/发送统计。

新任务在发送前记录稳定凭据和端点身份；Key 调序、可信 OAuth 刷新、替换、停用、跨渠道及三库 JSON 读写均有回归。Remix 使用原凭据和供应商任务 ID，跨 Token 拒绝。Suno 按凭据分组查询，相同上游 ID 在不同渠道/凭据下分开更新；本地查询不再绕过轮询持久化终态。提交账务恢复未结束时不启动任务终态计费，临时渠道/凭据不可用不直接把供应商任务判为失败。

2026-09-13 v9：`PersistTaskCompletion` 将 Suno/视频终态与 `task-terminal:<id>` 账务意图一起提交；任务 quota 与用量统计同事务调整，消费/退款日志复用幂等日志键。24 个三库场景覆盖钱包、订阅、零差额、全退、部分退和补扣，并注入 Token、统计、日志失败。既有终态且没有恢复意图的历史记录不自动补退。轮询错误及无明确任务状态的响应不再被当成任务失败，不同渠道保持并行。

Ollama 四协议均经过共享 `ollama_chat` 投影；强制/串行/严格工具约束以及不支持的角色提前拒绝，采样、图像 detail 等损失按策略诊断。工具参数不再经浮点解码，同名多轮工具 ID 不冲突。SDK 扩展测试发现并修复 JSON 正文丢失和异常流重复终止，原始 HTTP 断流回归要求恰好一个失败终止事件。

v9 主模块全测、主模块 vet/build、独立 RelayKit 全测/vet/build 和真实双应用检查均通过。双应用测试进程 22384/19192 已退出。本地控制台后端已更新为隔离的 v9 二进制；源码仍未提交、未发布。

2026-09-13 Midjourney：继续使用原 Midjourney 表；新提交在发送前绑定稳定凭据，成功 ID 在任务与账务意图提交后才暴露。明确未接受可推进候选，接受不明不重放，`Retry-After` 进入健康冷却；第 4 个候选、取消、跨 Token、Key 调序、提交/结算/日志失败均有本地 HTTP 回归。后台按渠道和凭据查询，临时不可用不伪造失败，终态与退款意图同事务。三库 33 场景及完整迁移重复启动通过。新增 `private_data` 字段不改写历史费用；旧任务缺少凭据绑定时不猜测 Key。图片链接 v2 绑定本地行，签名域与旧 provider ID 链接隔离。

2026-09-13 媒体：OpenAI 图像不再将空数据、损坏事件或负数 usage 当成成功；大于 256 KiB 的已验证首张图片不占前导事件缓冲。中断流按已交付图片数结算，只有部分图片时按一次，不按请求 n 收取未交付费用。Realtime 识别正常完成、进行中断开、客户端错误和供应商失败；usage 事件本身不算有效输出，输入而无输出时退款。固定价格、重复 response.done、估算/可信 usage、共享租约及结算恢复均有本地 WS 回归。媒体改动后的定向测试、主模块全测、vet 和新二进制构建已通过；真实供应商验收仍未执行。

2026-09-14：SSE 写出、prelude 提交和逻辑请求终结共享首个输出错误；兼容 void 包装也不能吞掉短写或再发送成功终止。客户端写入失败不因 scanner 已记录 handler_stop 而处罚供应商。图片 usage 拒绝负值、总量溢出、不一致总量和输入明细；转换后的 Ali、Imagen、MiniMax、Jimeng、Replicate、智谱图片使用相同交付边界。Replicate 转换阶段只准备上传，实际上传使用最终验证的头部并在数量预算和预扣后执行；已接受上传不会被后续生成错误改写为可重放。图像原生透传应用模型映射和参数覆盖，供应商 Extra 在内部复制后仍保留。

v11：Realtime 不再用单个连接级 pending 覆盖交错响应；旧完成事件不清除其他响应，末帧有效内容参与估算，缺 ID 的多响应事件拒绝，空后续响应不收费。Ali 轮询有界、可取消并沿用实际发送的身份，图片下载传播取消且失败不交付；5 个实际 HTTP 回归验证既有副作用规则，未另建任务记录。MiniMax/火山保留 metadata 的 0/false，最终实际朗读文本决定输入预扣；Cloudflare 仅开放非流式转写。账户组只作用于账户配置，已退役的 Ability 业务组不重新用于模型授权；预览与执行一致性回归已通过。

v11 后续集成：Chat 当前分片立即交付，异常不能补发成功终态；Claude 首帧并行工具、单次角色及 ThinkingToContent 的同帧正文、多 choice、工具不重复回归通过。Realtime 完整 item 在缺少 response.done 的中断中仍可估算，同一 item 的 delta/done 不重复计费。原始工具分片没有生成客户端事件时，保持可重试且受前导事件数量限制。JSON 下游短写、取消和写错误不再作为成功计费或供应商故障。

Veo 的 Gemini/Vertex 两个任务适配器共用经过验证的参数与图片，计费和发送都读取同一请求。默认 8 秒、seconds、duration、metadata 优先级以及分辨率一致；超界、冲突别名、无效 metadata、多个输出和图片来源冲突返回 400。URL 图片尚未实现，明确拒绝；非法 base64、空体、超过 20 MiB 和文件读取失败不能静默变成纯文本生成。61 个定向场景、34 个 HTTP 场景通过，包含真实本地 Vertex JWT 换取、multipart 图片、预扣金额和拒绝时零发送/零换取/零账务意图。

最终复审补充：Gemini/Vertex 下载在读取缓存前复核原凭据，失效返回 `503 task_state_unavailable`；补查和 OAuth 交换传播取消。补查仅接受成功 HTTP、匹配的原任务 ID 及成功终态，移除解析失败后的 URL 备用路径；39 个控制器场景通过。私有 Google Files URI 与 `gs://` 复用状态归属记录，未知或跨 Token 不发送；同 Token 续轮保留实际 URI。Claude/Gemini 推理控制及已配置搜索的转换损失在 `safe/strict` 下拒绝，显式 `allow` 保留诊断；原生请求保留控制字段。

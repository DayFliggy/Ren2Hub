# 模型价格与渠道倍率

模型价格只来自管理员保存的配置，代码不内置模型价格；音频计费和工具调用沿用上游 NewAPI 的内置值。用量规范化、舍入和表达式语义沿固定 NewAPI 核心；`setting/ratio_setting/` 保存模型及辅助倍率，`setting/billing_setting/`、`setting/task_pricing_setting/` 和 `pkg/billingexpr/` 提供表达式与原生任务定价。

本地删除了 NewAPI 的以下内置价格（与上游的差异见[同步账本](../upstream/SYNC_LEDGER.md)）：

- 模型倍率、按次价、补全、缓存、缓存写入、图像的默认表，以及 `POST /api/option/rest_model_ratio`。
- 内置计费表达式（DeepSeek 峰谷、gpt-6-astra、gpt-image-2 等）。
- OpenRouter Claude 由 `usage.Cost` 反推缓存写入 Token 的规则（它只对内置模型倍率生效）。
- 补全倍率的模型规则与锁定：管理员保存的 `CompletionRatio` 总是生效，旧 `CompletionRatioMeta` 选项不再返回。

未配置的规则如下：

| 项目                                           | 未配置时                                                                               |
| ---------------------------------------------- | -------------------------------------------------------------------------------------- |
| 模型（无 `ModelPrice`、`ModelRatio` 或表达式） | 拒绝请求（模型价格未配置）。用户开启“接受未定价模型”或站点处于自用模式时按倍率 37.5 计 |
| 输出、缓存读、缓存写、图像倍率                 | 1，即与输入同价；1h/12h 缓存写入仍按固定倍数放大缓存写入价                             |
| 音频倍率                                       | 选项表首次写入时采用上游内置表；之后以已保存的表为准，表中缺项为 1，删除不恢复内置值   |
| 工具调用                                       | 内置工具用上游内置单价，其他工具为 0                                                   |

工具内置单价保留上游原文（`tools.go` 与上游一致），见 `setting/operation_setting/tools.go` 的 `seedHardcodedToolPrices`，数值按美元计入 quota，智谱、阿里搜索档位的人民币标价也不换算。`tool_price_setting.prices` 保存的值覆盖内置价，0 表示免费；托管工具名只按协议项或厂商用量计数，同名客户端函数不计入。

音频与上游 NewAPI 一致。`AudioRatio`、`AudioCompletionRatio` 保留上游内置表（`gpt-4o-audio-preview`、`gpt-4o-realtime-preview`、`gpt-4o-mini-tts`、`tts-1` 等），首次保存定价时随其他键写入选项表，之后以选项表为准；在定价页取消某模型的音频单价并保存即删除该倍率，不恢复内置值；OpenAI 兼容接口只在模型存在音频倍率时走音频结算。原生 Gemini 接口及没有音频倍率的兼容请求走文本结算；文本结算中，`operation_setting.GetGeminiInputAudioPricePerMillionTokens` 给出的模型（Gemini 2.5 Flash、2.0 Flash 等）按上游固定美元单价单独计音频输入，其他模型的音频输入计入输入 Token；同一模型的兼容音频价与原生固定价不一致时，目录不展示价格，转换预览拒绝转换。音频倍率只是附加维度，模型仍须配置基础价格才能调用。

缺项与显式零不同。删除或清空某个模型并保存，等于提交 `pricing: {}`，该模型回到未定价，不恢复任何价格；`ModelPricingChange` 没有 `reset` 字段。

每个渠道的 `channel_ratio` 必须大于 0、最多 18 位小数、最大 1000，十进制指数在 -18 到 18 之间，默认 `"1"`。每 Key 的 `max_channel_ratio` 可为空；它仅限制候选，不创建第二份售价。业务分组、组间倍率及充值分组倍率已移除。

| 模式         | 接入位置                                                                         |
| ------------ | -------------------------------------------------------------------------------- |
| 按量         | 原生输入、输出、缓存、图像、音频、工具费用合成后，最终取整前应用一次渠道倍率     |
| 按次         | `ModelPrice` 与原生数量规则应用渠道倍率                                          |
| 表达式及任务 | 保留原生表达式和用量语义，独立绑定渠道倍率，不能放进会被适配器替换的 OtherRatios |

模型编辑仍使用 `GET/PATCH /api/option/model_pricing` 和 `expected_version`。目录 `/api/user/model-catalog` 对基础价与各渠道报价提供同一展示/排序投影，区分 configured、default、zero、unavailable；default 只来自自用模式的 37.5 回退。只投影能静态确定的表达式单价；动态条件、上下文倍率不能猜成 1。目录十进制字符串不构成结算精度升级。

价格同步在原生 `/api/ratio_sync/fetch` 增量支持固定来源纯解析：请求不带 `upstreams` 和 `channel_ids` 时返回固定来源草稿，每个来源的 `id`、`name` 由后端 `pkg/pricesource.Sources()` 给出，前端按返回值渲染，不另写来源清单；两个来源依次下载、各限 15 秒，前端为这次请求设 45 秒超时。本地来源只有 `sub2api` 和 `axonhub` 两种格式到表达式的转换。价格抓取弹窗里参与同步的上游只有原生预设（basellm、models.dev）和已配置 Key 的 OpenRouter 渠道，其他渠道即使出现在 `/api/ratio_sync/channels` 中也不参与价格同步。草稿给出下载内容的 SHA-256，与来源发布的 `.sha256` 比对结果作为来源级警告，不改变状态。sub2api 中已知但本地不计费的字段（如 `cache_storage_cost_per_token_per_hour`）跳过并在草稿中警告，其他未知计价字段仍然拒收。结果只进入草稿，保存后才生效；渠道上游抓取路径继续保留。抓取失败、部分不可解析和明确零价分别表达，不自动覆盖售价。

`HandleChannelRatio` 与上游一致，只返回渠道配置的倍率，调用计价不再打折。优惠只在充值下单时降低实付，到账额度足额，详见[充值折扣](TOPUP_DISCOUNT.md)。

渠道倍率无效时，`HandleChannelRatio` 返回错误，`ModelPriceHelper`、`ModelPriceHelperPerCall` 和任务表达式分支都拒绝请求（任务返回 400 `model_price_error`），不预扣、不发上游，也不按 0 计费。

所有金额 API 沿原生数值 quota，不补造旧 `available_quota`、`reserved_quota` 或钱包版本。配置生成、公开展示和排序不能反向决定路由。实际结算以 `relay/helper/price.go`、`service/text_quota.go`、`service/tiered_settle.go`、`service/task_billing.go` 为准。

渠道上游倍率（`GET /api/channel/upstream_ratios`、`/:id/upstream_ratio`）只展示上游声明的分组倍率，写入独立表 `channel_upstream_ratios`，不改写 `channel_ratio`，也不参与路由或计费。详见 `docs/channel/UPSTREAM_RATIO.md`。

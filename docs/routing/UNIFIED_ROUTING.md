# 统一路由

更新：2026-09-07。本文描述源码契约；生产是否已采用该版本应以实际镜像和发布记录为准。

## 路由和权限

`middleware.Distribute` 和协议阶段切换统一调用 `SelectLiveTokenRoute`。自动模式根据模型能力、渠道优先级和健康评分选渠；手动模式使用 Token 的 Profile、活动 Group、Entry 顺序与 Policy。没有 Profile 的已有 Token 使用自动模式；手动 Profile 没有启用的活动组时拒绝请求。

Token 的所有权、启停、到期、额度、IP 和模型限制继续生效。旧 `Group`、`AutoGroups`、`CrossGroupRetry` 字段、业务组权限、组间价格和旧随机选择器退出运行路径。账号分类仍可用于充值、订阅和账号限流，但不授权模型或决定 Relay 价格。

平台渠道默认可被发现；显式撤销或到期的 UserChannelEntitlement 会阻止使用。能力快照、当前 Channel/Ability 状态、模型限制、协议与 Advanced Custom 路径共同决定候选资格。目录未识别但渠道明确配置的模型归为 custom，不猜测供应商；映射冲突和非法映射会拒绝。

## 并发和计费

- `Channel.capacity_total` 是实际并发容量，同一渠道的所有模型共享一个 Redis 租约池。默认容量为 20。
- `Channel.channel_ratio` 是实际计费乘数，默认 1；UI 的这两个字段是唯一配置来源。
- 每次尝试在预扣和发送前获取租约，复检渠道、能力版本、健康版本、Token、Profile、Entry、使用资格与价格。
- 计价后的倍率或容量变化会阻止发送。Redis 不可用或容量耗尽时不回退、不发送上游、不扣费。
- 重试共享账单，切到更高价格时补充预扣；已交付响应不因之后的租约续期失败而退款。
- 普通、流式和媒体提交都续期并释放租约。媒体任务运行于供应商期间不持续占用提交请求的租约。

新计费对象与日志使用 `channel_ratio`。历史 TaskBillingContext 的 `group_ratio` JSON 字段保留作为已接受的价格快照，异步结算不会重新读取分组配置。Midjourney 继续使用已有钱包账务和退款记录，不新增订阅计费能力。

## 协议和重试

Chat、Responses、Claude、Gemini、图像、音频、Embedding、Rerank、Realtime、Compact、Midjourney、Suno 和视频均使用同一选渠与准入边界。查询类接口保留自己的任务所有权校验；后续任务操作绑定原任务渠道，并重新检查当前能力和用户资格。

Compact 保留精确模型和基础模型两个阶段。每阶段分别排除已用 Key，能力健康按该阶段实际匹配的能力模型记录，逻辑计费模型保持独立。总尝试次数受系统上限及手动重试策略约束。已输出响应后不得切换上游或拼接第二份响应。

## 接口和运行依赖

- `/api/status.frontend_capabilities` 开放 `api_tokens`、`token_private_routing`。
- `/api/token/*` 返回 `type: auto | manual`，不返回旧分组字段。
- `/api/routing/*` 管理 Profile、候选目录和静态预览；预览不发送请求、不预扣、不持有租约。
- `/api/channel/` 管理实际容量和渠道倍率；独立 `/route-policy` 接口已移除。
- `/api/pricing` 返回模型基础价格和可用渠道倍率区间，取消旧组过滤和组倍率响应。

不再使用 TOKEN_PRIVATE_ROUTING_ENABLED、ROUTE_LIVE_*、ROUTE_SHADOW_* 或 ROUTE_SCORE_* 灰度开关。能力刷新任务继续使用 ROUTE_CAPABILITY_REFRESH_*；ROUTE_DECISION_EVENT_QUEUE_SIZE 控制有界决策日志队列。

## 数据和验证

代码清理不自动删除历史数据库列、账单、任务或审计数据。旧组配置即使仍在数据库中也不会重新进入请求权限和定价。该版本改变权限与计费语义，发布前需备份并按部署技能评估回滚，不能仅凭 AutoMigrate 成功认定可只回滚镜像。

`router/unified_routing_integration_test.go` 使用隔离 SQLite、Redis 和模拟 HTTP 上游，覆盖 Chat、Compact 阶段切换、Suno、视频、Midjourney、倍率计费、租约释放以及准入失败不发送、不扣费。其他测试覆盖 Profile 版本、资格撤销、Key 健康、表达式计费和退款。主模块与独立 RelayKit 必须分别验证；本地模拟测试不代表真实供应商或生产集群已验收。

### 2026-09-06 本地验收

- 主模块 `go test -timeout 60s ./...` 通过；媒体路径回归覆盖 Audio、Rerank、Midjourney、Suno、视频及不支持协议的拒绝。
- RelayKit 在 `GOWORK=off` 下独立构建通过，Vue 产物嵌入检查与主应用构建通过。
- Vue 573 项单元测试通过，类型检查、lint、格式检查与生产构建通过。
- Playwright 188 项测试覆盖根路径、退休 `/next` 前缀、统一路由 UI、全部页面烟测、桌面/平板/手机、明暗主题、键盘、溢出和画布像素；浏览器使用隔离 API 夹具。
- `routing-simulator` 全部测试通过，包括自动 Profile 临时切换到手动后恢复原模式、身份与空配置。

真实供应商异步收费语义仍需以生产供应商响应和账务记录核验；源码契约不以本地模拟替代该证据。

# Ren2Hub 文档入口

本文索引当前源码的开发、接口和运维契约。主项目是 Go/Gin/GORM 网关、Vue 控制台与独立 RelayKit 模块；当前仅允许一个网关进程承接业务流量。钱包是唯一资金来源，Token 是附加消费限额。

## 现行文档

| 主题 | 入口 | 内容 |
| --- | --- | --- |
| 开发与结构 | [项目约定](../AGENTS.md)、[公共常量](../constant/README.md) | 目录边界、独立模块、代码与验证要求 |
| 安装与发布 | [Compose 安装](installation/DOCKER.md)、[宝塔环境](installation/BT.md)、[切换与回退](routing/GATEWAY_RELEASE.md) | 空库安装、单实例排空、持久化兼容性 |
| Web 应用 | [前端说明](../frontend/README.md)、[页面路由](../frontend/ROUTING.md)、[术语表](translation-glossary.md) | Vue、同源 API、功能门控与中英文文案 |
| 登录与会话 | [认证契约](authentication.md) | 浏览器登录、刷新会话、绑定、安全验证、Origin 与代理规则 |
| 模型路由 | [统一路由](routing/UNIFIED_ROUTING.md)、[网关 API](routing/GATEWAY_API.md) | 自动/手动路由、本地 FIFO、资格重检、队列与故障转移 |
| 协议和渠道 | [渠道接入类型](channel/TYPES.md)、[支持矩阵](routing/GATEWAY_SUPPORT_MATRIX.md)、[RelayKit](../relaykit/README.md)、[渠道设置](channel/other_setting.md) | 实际实现的协议转换、Responses WebSocket、供应商入口和边界 |
| 账务架构 | [实现边界](billing/IMPLEMENTATION.md)、[账务 API](billing/API.md)、[结算兜底与恢复](billing/RECOVERY.md) | 冻结报价、持久化预留、事件、原子结算与先计费兜底 |
| 定价 | [模型定价与价格获取](billing/PRICING.md)、[计价接口](../pkg/billing/API.md)、[表达式](../pkg/billingexpr/expr.md) | NewAPI 配置、五源草稿、三模式计算、空默认与加密重放 |
| 易支付充值 | [支付契约](billing/EPAY.md) | 报价、订单快照、商户版本、通知重试与管理员补单 |
| GMPay 加密货币充值 | [接入契约](billing/GMPAY.md) | 独立配置、多链资产、USD 报价、到账证据和未证明边界 |
| 支付系统设置 | [EPay / GMPay 配置清单](billing/PAYMENT_SETTINGS.md) | 控制台入口、默认值、金额单位、保存与启停边界 |
| 复验方法 | [网关验证](routing/GATEWAY_VALIDATION.md)、[账务复验](billing/VALIDATION.md)、[验收边界](routing/SINGLE_INSTANCE_ACCEPTANCE.md)、[SDK 套件](../tests/gateway-sdk/README.md)、[准入基准](../tests/admission-benchmark/README.md) | 隔离依赖、测试入口、测量范围与未证明的边界 |

`openapi/api.json` 与 `openapi/relay.json` 是有限覆盖的历史接口描述，尚未与全部现行路由同步。它们不能证明接口仍开放，也不能替代上表中的 API 契约、`router/` 注册和实际请求校验；不要仅据这些 JSON 生成生产客户端。

## 源码阅读顺序

启动看 `main.go`；普通 API 从 `router/` 进入 `controller/`，再跟进 `service/` 与 `model/`。这是职责分层，不要求每个控制器必须经过 Service。

AI 请求按 `router/relay-router.go` → 鉴权/限流 → `middleware/gateway_admission.go` → `middleware/distributor.go` → `service/request_route.go` → `controller/relay.go` 阅读。宿主在 `relay/` 发送请求和处理传输，`relaykit/` 负责协议语义，`tokenkit/` 负责文本与图片的 token 计数和估算（两者都是独立模块，不依赖主模块）；后台作业从 `controller/system_task_handlers.go` 进入系统任务框架。

Vue 从 `frontend/src/router/`、`frontend/src/stores/app.ts` 和 `frontend/src/api/` 核对页面、能力状态与真实 API。后端入口存在不代表当前 UI 开放。当前账务格式为 schema 5，仅接受空库初始化或版本、密钥指纹均匹配的 `billing_schemas` 数据库；价格合同为 4，计算器为 `newapi-c71eefbc-v1`。不能把旧计费数据库直接接给新代码。模型定价只从 `model_pricing` 保存，公开接口返回 NewAPI 模型行；五源价格获取只填入前端草稿。请求求值上下文加密保存供重放，日志和 API 不暴露明文或证据密文。

## 文档与部署状态

运行契约只在对应主题文档维护，复验方法见上表；本目录不保存执行结果，文档整理不算重新测试。主项目源码发布、服务器隔离验收与常驻应用部署是不同状态，部署版本、验收证据、备份和发布结果只由所属工作区[服务器档案](../../docs/server/SERVER_PROFILE.md)记录。

RenAI 工作区 `reference-projects/` 是本地研究材料，不是运行依赖或发布内容。参考仓库当前 HEAD 也不等于历史研究采用的提交。构建产物、测试数据库、未合并补丁和证据须分别识别，不能因为被 Git 忽略就统一删除。

## 历史追溯

阶段报告、审计记录、上游集成证据和旧验收结果已从工作树移除，从 Git 历史读取。需要核对某次修复时，在 Ren2Hub 目录使用只读命令定位，不把旧状态当成当前部署结论：

```powershell
git log --all -- docs/routing docs/billing docs/upstream
git show <commit>:<repository-relative-path>
```

历史代码需要执行时使用隔离环境；恢复旧报告或旧代码不授予发布、数据库恢复或重放支付的权限。

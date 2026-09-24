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
| 协议和渠道 | [支持矩阵](routing/GATEWAY_SUPPORT_MATRIX.md)、[RelayKit](../relaykit/README.md)、[渠道设置](channel/other_setting.md) | 实际实现的协议转换、供应商入口和边界 |
| 账务架构 | [实现边界](billing/IMPLEMENTATION.md)、[账务 API](billing/API.md)、[恢复与对账](billing/RECONCILIATION.md) | 冻结报价、持久化预留、事件、原子结算与人工决定 |
| 定价 | [单价与初始目录](billing/PRICING.md)、[计价接口](../pkg/billing/API.md)、[表达式](../pkg/billingexpr/expr.md) | 不可变价格版本、规范用量、精确金额与安全属性 |
| 易支付充值 | [支付契约](billing/EPAY.md) | 报价、订单快照、商户版本、通知重试与管理员补单 |
| GMPay 加密货币充值 | [接入契约与验收状态](billing/GMPAY.md) | 独立配置、多链资产、USD 报价、到账证据和验收边界 |
| 支付系统设置 | [EPay / GMPay 配置清单](billing/PAYMENT_SETTINGS.md) | 控制台入口、默认值、金额单位、保存与启停边界 |
| 复验方法 | [网关验证](routing/GATEWAY_VALIDATION.md)、[SDK 套件](../tests/gateway-sdk/README.md)、[准入基准](../tests/admission-benchmark/README.md) | 隔离依赖、测试入口与测量范围 |
| 既有验证证据 | [网关与协议](routing/SINGLE_INSTANCE_ACCEPTANCE.md)、[账务](billing/VALIDATION.md)、[易支付](billing/EPAY_VALIDATION.md) | 明确版本、环境、已执行结果及未证明的边界 |

`openapi/api.json` 与 `openapi/relay.json` 是有限覆盖的历史接口描述，尚未与全部现行路由同步。它们不能证明接口仍开放，也不能替代上表中的 API 契约、`router/` 注册和实际请求校验；不要仅据这些 JSON 生成生产客户端。

## 源码阅读顺序

启动看 `main.go`；普通 API 从 `router/` 进入 `controller/`，再跟进 `service/` 与 `model/`。这是职责分层，不要求每个控制器必须经过 Service。

AI 请求按 `router/relay-router.go` → 鉴权/限流 → `middleware/gateway_admission.go` → `middleware/distributor.go` → `service/request_route.go` → `controller/relay.go` 阅读。宿主在 `relay/` 发送请求和处理传输，`relaykit/` 负责协议语义；后台作业从 `controller/system_task_handlers.go` 进入系统任务框架。

Vue 从 `frontend/src/router/`、`frontend/src/stores/app.ts` 和 `frontend/src/api/` 核对页面、能力状态与真实 API。后端兼容入口存在不代表当前 UI 开放。当前数据库格式仅接受空库初始化或匹配 `billing_schemas` 标记的数据库；不能把旧计费数据库直接接给新代码。

## 文档与部署状态

运行契约只在对应主题文档维护，测试方法与执行结果分别记录。更新验证结果时注明源码版本、环境和真实执行范围；文档整理不算重新测试。主项目源码发布、服务器隔离验收与常驻应用部署是不同状态，实际运行版本、备份和发布结果统一查所属工作区 `docs/server/SERVER_PROFILE.md`。

RenAI 工作区 `reference-projects/` 是本地研究材料，不是运行依赖或发布内容。参考仓库当前 HEAD 也不等于历史研究采用的提交。构建产物、测试数据库、未合并补丁和证据须分别识别，不能因为被 Git 忽略就统一删除。

## 历史追溯

已被现行契约吸收的阶段报告从工作树移除，Git 保留历史。需要核对某次修复时，在 Ren2Hub 目录使用只读命令定位，不把旧状态当成当前部署结论：

```powershell
git log --all -- docs/routing docs/billing
git show <commit>:<repository-relative-path>
```

网关与协议复审的版本入口保留在[网关验证证据](routing/SINGLE_INSTANCE_ACCEPTANCE.md)，账务与易支付的版本入口分别保留在对应验证页。历史代码需要执行时使用隔离环境；恢复旧报告或旧代码不授予发布、数据库恢复或重放支付的权限。

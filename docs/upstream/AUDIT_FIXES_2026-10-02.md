# 2026-10-02 审计修复交付

本文冻结推送部署授权之前的本地验收状态：已完成 9 项审计问题修复与 3 项上游能力补齐，交付本地工作树；当时 `candidate_sha=null`，未提交、推送、部署或操作生产数据库。用户已有主题提交 `e504860da84531e995eae283e6c03257aadbcba8` 保留。后续提交、隔离候选及正式发布单独记录在工作区服务器档案和发布证据中，不将本报告的历史空 SHA 解释为最新发布状态。

后续批准的 type 57 退役移除了本报告 FCA-02/FCA-03 涉及的 Codex 订阅窗口及界面；以下实现与通过记录保留作历史追溯，不代表当前仍开放该功能。现行边界见[渠道接入类型](../channel/TYPES.md)和[同步账本](SYNC_LEDGER.md)。

实施开始时读取 NewAPI `main`，仍为 `1a4166d8e8ba9802d2ca56fe8ecf0ed5404e80d5`。官方发布参照 rc.41（`2035a82aeb5414253a728bd937d4b8f97aa99b9b`），发布后 7 个提交；没有新增主干落后窗口。Release API 403 保持采集不完整，官方发布页是独立补证。本地参考树有改动，比较使用固定 SHA 的 Git 对象，未 fetch 或改写参考仓库。

逐项来源、落点、风险和复评条件见[同步账本](SYNC_LEDGER.md#2026-10-02-审计修复与同步补齐)。上游来源：[rc.41 发布页](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.41)、[锁定主干](https://github.com/QuantumNous/new-api/commit/1a4166d8e8ba9802d2ca56fe8ecf0ed5404e80d5)。

## 修复结果

| 问题 | 完成的行为 |
| --- | --- |
| PB-1，P1 | immediate 创建、终态、用量、结算意图同事务；提交失败沿用冻结估算，提交成功后继续幂等交付，均不重发或错误退款 |
| SEC-02 | 用户 status/role 定向持锁写入，保留并发密码/资料/账务；false 不写密码，降级与权限清理原子回滚 |
| SEC-01 | OAuth/Telegram 二次验证支持原浏览器/PAT身份，拒绝撤销、过期、替换及 Proof 重放 |
| FCA-01 | 目录按实际端点及执行器判价；缺价允许设置不能放过资格失败或不兼容表达式；共享端点与请求共用首候选选择 |
| PB-3 | 协议级 retainResult 拒绝；原生 false 仅 immediate，异步保留并告警；Images immediate 一次交付，Responses 三模式保留；豆包 1.2.1 |
| PB-2 | 无名工具 JSON/SSE 明确错误，不伪装 completed，不切备用渠道重放；按实际交付证据结算 |
| FCA-02 | Codex 用量运行状态入口支持 read 与 read+operate，无需 write |
| FCA-03 | 开关由服务端确认值驱动，待确认时保留旧值，失败恢复、取消/换渠道/撤权拒绝迟到覆盖 |
| ROOT-ENV-01 | 候选检查剥离真实端点、凭据与 opt-in，保留明确本地 SDK 运行时 |
| UP-01 | 市场索引、源码、图标缓存重验证，保留哈希、取消、大小及凭据边界 |
| UP-02 | Vue 比价字段和表达式词段高亮，精度及缺失/零保真，纯文本呈现 |
| UP-03 | 状态原因从现有事实派生，服务端脱敏/限长，两套管理 DTO 一致，键盘及移动交互可用 |

收尾审阅额外验证：discarded 正文、URL、插件状态在 SQL INSERT 前即清理，避免先写入事务日志再删除。结果摘要、终态、用量和报价证据独立保存。NewAPI 多插件绑定只能使用实际选中的协议执行器价格，不能借后续候选价格开放端点；泛型原生任务路由仍可分别选择插件。

## 验证证据

本机证据根为 `%LOCALAPPDATA%/RenAI/AuditRuns/20261002-audit-fixes-1e738bd7`，已于 2026-10-06 删除，不再可访问。其中测试数据库环境文件是私密执行材料，报告不复制连接信息。

| 范围 | 结果与证据 |
| --- | --- |
| 主模块 | `node scripts/test-go.mjs -count=1` 通过；`main-go-full.log` 含所有非 controller 包及 22 个 controller 批次。收尾代码变化另有相关完整包与定向复验 |
| 最终受影响包 | `delivery-affected-tests.log`：service/middleware 全包通过；`delivery-vet.log`、`delivery-build.log`：静态检查及新二进制构建通过 |
| RelayKit / 插件 | 独立 `GOWORK=off` 全测与 build，ABI d.ts 与 20 个 schema 引用通过；`protocol-billing-validation.json` |
| 资金失败路径 | 四阶段真实进程退出、终态 UPDATE 故障、交付失败、唯一事件、冻结恢复、取消/下载失败；三库最终存储边界无 SKIP |
| 结果保留 / 工具 | 原生同步/异步、Images immediate/真实异步插件、Responses JSON/SSE/background、无名工具真实 HTTP 通过；首次响应、后续检索、发送次数与结算均断言 |
| 数据库 | 本轮新建 SQLite/MySQL 5.7.19/PostgreSQL 15.19 隔离目录与随机 loopback 端口；权限、并发、schema、终态及存储边界通过；`database-validation-summary.json`，0 SKIP |
| 官方 SDK | 基础、Media、Semantic Python、Semantic Node、CloudToolAndInterruption、CloudParallelTool 六套逐套 60 秒入口；本地模拟上游，全部通过、0 SKIP |
| Vue | 177 文件、950 用例；typecheck、lint、format check、生产 build 通过；`frontend-fixes-validation.json` |
| 浏览器 | 35 个独立场景，桌面/移动、明/暗主题；最终用量变更后重跑 21 项，复用未变化价格 10 项/插件 4 项。20 张原图及四组拼图检查通过 |
| 候选脚本 | `candidate-isolation-tests-final.log`：21 场景通过，合成地址/凭据不能进入子进程，本地运行时路径保留 |
| 空库冒烟 | `delivery-smoke.log`：使用新二进制、新 SQLite 和模拟上游，PAT、能力卡、目录、导出、窗口、诊断及 Chat/Claude tool/Responses custom tool 通过；3 次发送、3 次唯一结算、预留归零 |
| 独立审阅 | `final-independent-review.json`：限定范围已确认未关闭问题 0；复现原件与修复后日志哈希保留 |

后端单次命令统一带 `-timeout 60s`。普通全量测试中需独立 opt-in 的分支不算通过，所需三库/SDK/浏览器在上述独立环境实际运行。没有连接真实供应商或正式 vLLM；没有进行生产负载或远端候选环境验收。

首次失败材料未删除：并行编辑中临时缺括号、测试升级全局插件 factory 后未恢复的污染，以及发现/收紧交互合同前的浏览器结果，均与最终通过日志分开。前端构建存在既有大 chunk 提示，没有通过放宽检查掩盖错误。

## 工作树与发布状态

写入前核对原 266 个文件，基线指纹为 `9762168ba823ff093193c234b9eafb1ec734d51ee682dfef02e56713a4e43cbd`。最终指纹、逐文件归类、恢复副本、二进制和工具 SHA-256 记录在 `worktree-delivery.json`、`validation-summary.json` 与 `tracked-final.patch`；不将包含本报告的指纹写回本报告形成循环。

本轮私有 MySQL/PostgreSQL 实例已核对进程身份后优雅停止，浏览器/Vite 和空库冒烟进程已退出。未清空已有数据库或删除旧证据。

本地实施与验证完成；本报告冻结时发布候选为 NOT_READY，没有本轮实际提交及绑定该提交的候选环境证据。后续提交、推送、远端部署和数据库重建按用户确定的独立授权边界执行。账本保留拒绝或延期项，不以机械合并 moejs、React、账号池或另一套账务来追平提交数。

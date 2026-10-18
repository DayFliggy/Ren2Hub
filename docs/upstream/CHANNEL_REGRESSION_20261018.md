# 历史能力回归与渠道路由重构执行记录

2026-10-18 用户批准实施。开发基线 `39daa6ccee738f7a26fdba131c829be1adbbdd5b`，历史视觉依据 `9a63062227c31ba54a6346e07ee8b4fd8802f22f`。后端已形成可审查提交 `87ad554d9`，Vue 默认完整浏览器 506 项已通过；其后的四条 AWS 双语字符串增量经 8 项定向单测、原 provider 四组合及静态检查、生产构建通过。最终候选尚未选定，`candidate_sha=null`；源码发布、镜像候选及正式切换尚未完成。

NewAPI 是唯一上游，当前核心仍为 `09109ec315a5086366f94bcce00f177ec831dbe1`。本轮参考 NewAPI `6cf0d90b5ff27f901697dddd61e0c28aba8efefc`、Sub2API `b0a02a3304a0a78cdd003ddda1dd23ba02c03084`、AxonHub `a946fca495c52344ed073abb6c3562b9ff728b12`。后两者只是针对具体领域的参考项目。

工作树 `/home/work/ren2hub-channel-regression-20261018`；私有执行证据 `/home/work/ren2hub-channel-regression-evidence-20261018`。`baseline.json` 保存初始 Git 树、工作区状态和工具 SHA256；`source-state.json` 是 10:36 UTC complete 的标准采集，参考仓库只读、未 fetch。Go 1.26.9、Bun 1.4.0 已核实；race 在隔离的 Go 1.26.9 / GCC 12.2 容器内实际执行。编译枚举不是运行测试，旧证据不计本批通过。

## 历史处置

| 历史链 | 采用代码/不变量 | 排除依赖 | 本轮落点/验证 |
|---|---|---|---|
| `6fb512461 → 325c4ce40 → 9a63062227` | 原生事务归因、充值返利；后期冻结、上限、幂等校验 | schema 5、账务事件、新人任务 | affiliate、TopUp 成功收口、管理员加额度；三库事务及定向 race 已通过 |
| `e8ca9d24b → ba106b23f → 9a63062227` | 固定分类目录、映射、canonical/alias/mixed/unknown | 旧协议能力/灰度、分类驱动路由、隐藏模型扩张 | `pkg/modellab` 纯解析、原生 vendor 优先；定向测试已实际通过 |
| `3e91ce7e7 → e993ceb08 → 02b0a180a` | 逻辑请求/尝试分层、倍率独立接点、完整入口覆盖 | 旧路由组、GatewayConfig、双管线 | 现有 controller/relay、service selector、BillingSession；提价重试、WS 生命周期定向回归及 race 已通过 |
| `a987d6918 → 19cf0ac62 → 9ac739890` | 原池热更新、幂等释放、资格先于截断、扫描不计发送重试 | FIFO 等待、Redis lease、旧资金恢复 | 无等待并发、共用候选服务；并发、热更新、释放和取消定向 race 已通过 |
| `1bb6e849e → 537dc8cd7 → 256672cf4 → fd12fa101` | 健康真实排序、未知同正常档、保持原权重比例、零权重备用 | 可配置策略框架、价格排序 | 自动选择器固定排序、观察存储；分档、权重、过期及配置代际定向回归已通过 |
| `d7095730e → 01098b738 → 29a1d1c78 → 1c04f945e` | 原生 DTO、设置部分保存、OIDC、权限与 WS 修正 | 整包页面改版、旧分组合同 | Vue 合同适配，保持现有原生安全实现 |
| `43c18f952 → d76638465` | 客户端配置纯函数、撤权/取消清理、有符号展示 ID | 分组市场 N+1、页面重设计 | 统一目录与客户端配置，视觉保持 9a |
| `357b0f87a → 63609374c → 9a63062227` | 首页消费日志聚合与故障传播 | 将部分第 25 小时合并成第一桶 | 24 个明确滚动区间、共享短缓存；时区、边界、取消和故障定向测试已通过 |
| `2ef58e026 → 9a63062227` | 五个固定价格来源和纯草稿解析 | 自动写售价、第二价格库 | 原生价格同步增量；五来源解析定向测试已通过，真实候选页面写入仍待验证 |

## 当前合同及接口

业务分组删除，最小路由单位为管理员管理的平台渠道。每 Key 自动/手动；空手动列表失败，零并发为无限且继续计数。原生模型价格乘渠道倍率，最高倍率是资格限制。用户槽覆盖逻辑请求，渠道槽覆盖真实尝试。新请求强制估算预扣，重试提价必须先补足；最终结算沿原生钱包。

新接口在现有 `/api` 下：`/user/model-catalog`、`/home/metrics`、`/data/self/summary`、`/performance/routing`、`/channel/labs`、`/user/affiliate`、`/admin/affiliate`。旧 `/next` 不恢复。首页及 dashboard 消费日志、逻辑性能、上游尝试观察有不同口径；不拼成同一总请求指标。价格字符串只用于显示/排序，不宣称升级原生结算精度。

返利默认关闭、20%、冻结 0 小时、时效/累计上限不限；管理员正向加额度的返利单独默认关闭。注册绑定与用户创建同事务；成功 TopUp、钱包与返利同事务。兑换码不触发。固定邀请注册奖励退役；注册基本额度保留。全额转入携带幂等标识。直属邀请列表只返回脱敏展示名与绑定时间，不公开被邀请人 ID、邮箱或原始用户名。

历史处置清单见 [CHANNEL_HISTORY_DISPOSITION_20261018.json](CHANNEL_HISTORY_DISPOSITION_20261018.json)，逐项保存本计划指名的 24 个历史提交完整身份、变更路径、采用或排除理由及补丁哈希；原始补丁保存在私有证据目录。该清单不宣称覆盖仓库全部历史提交。冻结核心逐文件差异见 [CORE_SOURCE_DELTA.json](CORE_SOURCE_DELTA.json)，纯源码差异不代表行为验收通过。

## 测试迁移记录

- 模型 owner 的业务组过滤、性能指标的 AllowedGroups/分组子序列、旧组缓存刷新操作退役，不计作通过；其优先级/权重、统计加权/刷新/时间界限、权限版本隔离回归保留到新渠道合同。
- QuotaData 原“有组才可统计”筛选退役；自用/管理员/root 隐私维度及 Token/渠道聚合保留。
- 原生插件候选筛选测试迁移至统一候选枚举，不保留 SQL 与缓存各一套随机选择器。
- 新增旧组库启动拒绝、首页区间/取消/故障、目录信息裁剪、静态价格投影及时间区间测试已定向运行；全套与候选状态单独登记。

## 本次执行证据

下列均为本次实际运行，不复用此前候选。失败日志保留，修复后的定向结果不能将原失败整套改记为通过。

| 检查 | 当前结果 | 私有证据目录下的文件 |
|---|---|---|
| SQLite / MySQL / PostgreSQL 返利、限额、归因、幂等及直属邀请查询 | 通过 | `finance-final-limits-invitees-three-db.log` |
| 三库路由、排序、空库初始化、重复初始化和旧库拒绝 | 通过 | `routing-databases-verbose.log`、`priority-three-db.log`、`startup-three-db-final.log` |
| 原生审计数据库合同及管理员余额幂等夹具 | 三库通过 | `audit-native-schema-three-db.log`、`user-manage-fixture-final.log`、`user-manage-fixture-*-attempt-2.log` |
| 并发、热更新、倍率重试、WS 和返利 race | 定向通过 | `routing-race.log`、`routing-lifecycle-race.log`、`finance-skip-race.log` |
| 主模块完整分批测试 | 84 包覆盖完成；Controller 22 批 436 项、Router 2 批 40 项通过。原失败保留，按清单复用和替代批次 | `go-full-8-inventory.json`、`go-full-*.log` |
| RelayKit 独立 `GOWORK=off` 测试及构建 | 通过 | `relaykit-final.log`、`relaykit-build-final.log` |
| Vue 单测 | 最近完整 180 文件 / 1020 例通过；之后的字体、KPI 和校验增量分别通过 | `frontend/test-final-verified.log` 及 Vue 实现记录 |
| Vue 类型、lint、格式和构建 | 通过；最后四字符串增量的 8 项单测、原 provider 四组合及静态构建另记 | `frontend/{build,lint,format}-provider-copy-incremental.log`、`frontend/test-provider-copy-incremental.log`、`frontend/browser-provider-copy-incremental.log` |
| 默认完整浏览器 | 66 份 spec、506/506 通过；0 失败、0 跳过、0 flaky。前后 844 份文件指纹一致 | `browser-full-final.log`、`browser-full-final.json`、`browser-full-final-result.json` |
| 验收脚本与插件驱动辅助测试 | 既有 33 例、插件 18 例通过；最终组合补测 23 例通过 | `acceptance-helpers-final.log`、`plugin-helpers-channel-ratio-2.log`、`acceptance-helpers-prepublication.log` |
| 安装器与验证器 | 定向通过 | `deployment-tools-2.log` |
| 实际 PGO 镜像、嵌入 HTTP、五组候选、SDK 和真实浏览器 | 尚未执行 | 必须绑定已发布完整 SHA 与不可变镜像，不使用工作树身份 |

完整 Go 枚举包含 1991 个顶层 Test/Example/Fuzz，枚举用于证明覆盖范围；不将枚举本身计为执行通过。最终清单 SHA256 为 `00f49f00553fd6a26afcdc7d9813466cafea8925ef33b3c4d9b5005934a45155`，逐项记录 43 份日志哈希、失败及续跑关系。受影响 controller/router/middleware 的最终 `go vet` 通过。

Vue 视觉与字体证据另见私有 `vue-implementation.md`、`font-artifacts.json` 及 `browser-model-visual-fixed/`、`browser-chart-font-fixed/`。静态截图、mock 浏览器和真实候选分别报告，不能互相替代。

正式库及当前 Redis db1 不在本轮开发目标内。新数据库与 Redis、镜像、归档和回退材料必须在候选通过后具体化，正式空库切换仍需当次批准。

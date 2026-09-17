# 单实例改造验收记录

更新：2026-09-17。单实例改造及审计修复已发布到新服务器，应用提交为 `f99a1bd66594476a90fb470496380ec05a2ba232`；PostgreSQL 15.18 隔离测试栈的 11 场景验证通过。公网生产站点未上线，没有执行生产 Redis 清理。

本地功能改造、正式回归和旧执行路径替换已完成。完整旧应用的等价工作树基线没有留存，因此“相同完整 HTTP 工作负载下的新旧性能比较”仍未完成，不能推导生产吞吐提升百分比。服务器长时间固定到达率压力、长连接排空及真实供应商验证仍待执行；完整生产上线验收尚未完成。

下文 2026-09-16 的本地记录包含开始任务前已有的路由、转换及账务修改，没有用 HEAD 覆盖工作树。当时尚未提交或发布的说明只描述历史快照。服务器通过结果使用当时的 Docker CPU/内存配额；随后配额全部取消，已核验健康状态，但无配额配置尚未重跑完整压测。

## 交付实现

| 边界 | 当前实现 |
| --- | --- |
| 用户与渠道准入 | `service/local_admission.go` 的同一 manager，按资源种类和 ID 定位稳定池；FIFO、直接授槽、一次性释放、总队列登记、空闲回收 |
| 请求归属 | 同用户的 Token、协议、Remix、Suno 续作、Midjourney 和 Realtime 共用用户容量；用户槽覆盖逻辑请求，渠道槽覆盖尝试，退避前清理并释放 |
| 热更新 | 提交后同步标记策略失效，合并读取快照，revision 阻止迟到覆盖；扩容唤醒、缩容不驱逐、无限转有限保留活动计数 |
| 路由 | 保留权限、能力、转换、价格和手动 position 边界；立即扫描后只排一个渠道，同层比较队列压力；最多扫描 64 个渠道和发送 3 次 |
| 故障转移 | 冷却属于渠道或已有渠道内健康范围；切到独立渠道不继承 Retry-After；保留健康 epoch、探测和不可安全重放限制 |
| 重试保护 | 固定百分之一精度，首发补充、额外发送消耗、发送前可撤销票据，预留计入 burst；热更新不补满 |
| 管理契约 | 四个新配置字段、显式零/null/省略区分、乐观版本；本地运行态与 Provider unavailable 分开，指标 schema_version=2 |
| 生命周期 | drain 拒绝新用户并唤醒排队者；跟踪 HTTP/SSE/hijacked WebSocket；默认 120 秒后取消上游、关闭连接，再给所有者 5 秒清理 |

manager 当前使用覆盖资源 map 的短互斥锁，统一保护总队列与回收；锁内没有 SQL、Redis 或上游 I/O。不是每个资源各一把锁。核心比较未发现该锁在本次负载中的稳定退化，但这不是大规模负载结论。

方案 B 的明确边界仍成立：排入 A 后不会在 B 空闲时迁移。未新增 Token limiter、跨渠道供应商池或自适应选路。

## 完成后审计与修复（2026-09-16）

在既有工作树上继续审计，没有重新以 HEAD 覆盖文件。发现并修复以下问题，新增回归保留在正式 Go 测试中：

| 问题 | 修复与验证边界 |
| --- | --- |
| Midjourney、视频在发送前 credential 配额拒绝后直接失败，漏掉独立备用渠道；末候选拒绝还可能漏掉此前繁忙渠道的队列 | 文本及媒体共用发送前拒绝分类，继续原扫描与尚未使用的唯一排队机会。HTTP 回归断言只发送一次、只记录一份账务、未增加健康失败，并覆盖原渠道绑定限制 |
| 排队期间扩容已授槽，后续检查仍要求容量等于入队前值 | 容量由稳定 manager 负责；保留状态、价格、能力和健康检查。HTTP 回归在旧持槽者未释放时扩容，新请求仍完成 |
| 批量撤权或渠道变更逐项刷新，首个慢查询之前尚未冻结后续资源 | 通知传入整批 ID，锁内先标记全部相关池失效，再做查询和缓存 I/O；同步屏障回归验证第二个被禁用用户不能趁首个刷新阻塞取得槽 |
| 请求入口的旧 RPM 与队列快照继续影响后续扫描 | 路由按 revision 刷新可变限制，保持原用户政策与等待预算；验证 RPM 放宽、队列启停及取消 |
| 扫描期间繁忙池变空并回收，零容量快照使候选被跳过 | 将无活动、无等待的候选重新做立即准入，允许零排队配置下取得空槽；忙渠道间仍按原策略层比较队列压力 |
| 实际入队前重复读取配置，读取可能超出剩余等待期限 | 准入参数只声明是否允许等待，队列上限由准入层在剩余预算子 context 内读取；慢查询回归验证超时返回、原 context 有效、无遗留 waiter |
| 视频任务的非成功响应丢失 Retry-After，未写入所属渠道冷却 | 任务错误保留带 Retry-After 的原始 API 错误；HTTP 回归断言 A 的完整 120 秒冷却、B 无冷却且完成请求 |
| 异步路由日志与请求日志并发修改计数器 | 首次 HTTP race 实测发现数据竞争；计数和轮转调度改为原子操作，轮转调度所有者负责清除状态。修复后的同场景 race 通过 |

本次日志位于 `.local-tests/single-instance-audit/`。`queue-policy-final.log` 记录新增定向服务、控制器及 HTTP 回归通过；`race-final.log` 记录 FIFO、重试额度、批量撤权、热更新、慢策略读取、候选队列、发送前故障转移和生命周期竞争检测通过。最初暴露 logger 竞争的日志仍保留在 `.local-tests/single-instance/audit-routes-race.log`，不能把该失败记录当作通过证据。

审计修复后的 `go test -p 4 ./... -timeout 60s` 全模块通过（`backend-final.log`）；`go vet -p 4 ./...` 和 `go build -p 4 ./...` 均通过（`vet-final.log`、`build-final.log`）。`TestGatewayOfficialMediaSDK` 使用已安装的固定依赖与模拟上游，Python、TypeScript 两套通过（`media-sdk-final.log`，没有跳过）。视频冷却回归最初失败及修复通过分别保留于 `.local-tests/single-instance/audit-task-cooldown-{before,after}.log`。

此轮审计未修改数据库 schema、SQL 事务、前端或 RelayKit；此前三库、前端与独立模块验证按相同代码边界复用。完整新旧 HTTP 性能基线与代表性生产数据库容量验收当时尚未完成；后续 PostgreSQL 场景验证和仍存限制见末节。

## 本地正式验证（2026-09-16）

所有 Go 测试命令带 `-timeout 60s`，单个包或 SDK 套件分别受此期限约束。原始日志保留在 `.local-tests/single-instance/`；前端截图保留在 `frontend/output/`。完整可重复命令见 [验证步骤](GATEWAY_VALIDATION.md)。

| 检查 | 结果与证据 |
| --- | --- |
| FIFO、取消授槽竞争、总队列、容量变化、回收、drain | 定向测试与 race 通过，`admission-race-final.log`；包括并发重试预留及 burst 缩小守恒 |
| 提交后热更新 | 无新请求时配置扩容、渠道扩容唤醒，禁用用户拒绝 waiter，active 保留；正式 `gateway_admission_events_test.go` 通过 race |
| 入口、发送与冷却边界 | middleware/controller 定向及 race 通过；跨 Token/协议/续作、最后发送取消、最多三次、退避前释放、错误保留、同层排队 |
| 配置三库 | SQLite 3.50.4、MySQL 5.7.19、PostgreSQL 16.4 实测通过；缺失字段迁移、显式 0/false、幂等、CAS 和取消，`model-final.log` |
| 账务与频率 | 三库账务及真实独立进程成功频率预留通过，`accounting-three-databases.log`；保留此多进程账务边界，不恢复多网关并发模式 |
| 应用进程 | 单实例认证、跨协议容量、429、取消、账务及顺序重启通过，`application-final.log` |
| 后端整体 | `go test -p 4 ./... -timeout 60s` 全部通过，`backend-final.log`；选择性环境测试另有下述实测，未把 SKIP 当通过 |
| 静态检查与构建 | `go vet -p 4 ./...`、后端构建通过；RelayKit `GOWORK=off go test ./... -timeout 60s` 与独立构建通过，`relaykit.log` |
| 官方 SDK | 文本、媒体、音频、Realtime、三组云场景、Legacy、Compatible、Special、Codex、Vertex Open Source、Custom、Python/Node Semantic 全部通过；固定依赖与模拟上游，无付费请求 |
| 前端 | 13 个定向 Vitest、类型检查、lint、格式和构建通过；桌面/移动、明暗、Provider unavailable、draining 共 4 个 Playwright 场景通过并检查截图 |
| 旧键清理工具 | dry-run 不写、仅固定前缀删除、重复执行及其他命名空间保护通过，`cmd/gateway-lease-cleanup/main_test.go` |
| 历史兼容与错误 | 新日志仅写 admission_state，旧 lease_state 仍可读；省略新增配置/null/Provider 部分不可用及错误层级正式回归通过 |

## 本地 HTTP 受控负载（2026-09-16）

运行 `TestGatewaySingleInstanceControlledLoad`，使用独占回环 Redis 5.0.14.1 的初始空 DB、内存 SQLite、真实 HTTP/SSE 和模拟上游。五组场景共 61 个请求，均断言账务请求数、授槽释放守恒、活动/等待归零和旧并发 Redis 命令为零。另有 `TestGatewayProviderFullChannelRoutesToIndependentChannel` 验证 A 的 RPM 满后直接选 B，不增加 A 调用。

| 场景 | 请求/成功 | 等待超时/队满 | 上游/额外发送 | 总耗时 p95 | 首内容 p95 | SQL/Redis 命令 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 低负载 | 12/12 | 0/0 | 12/0 | 7.20ms | 5.04ms | 936/324 |
| 突发 | 18/18 | 0/0 | 18/0 | 69.60ms | 66.52ms | 1444/463 |
| 拥塞 | 7/1 | 4/2 | 1/0 | 509.51ms | 506.85ms | 94/49 |
| 部分故障 | 12/12 | 0/0 | 13/1 | 205.70ms | 203.05ms | 969/316 |
| 全池不可用 | 12/0 | 0/0 | 0/0 | 1.56ms | 无输出 | 74/38 |

突发有 16 个 waiter，平均排队 33.25ms；拥塞设定等待 500ms，4 个 waiter 正确超时，2 个队满拒绝。部分故障 A 返回 60 秒 Retry-After，仅调用 A 一次，B 完成全部 12 个逻辑请求；额外发送比例为 1/12。全池不可用不生成上游发送或账单。

| 场景 | 测试进程 CPU | 分配字节 | 结束堆字节 |
| --- | ---: | ---: | ---: |
| 低负载 | 31.25ms | 12564496 | 17158392 |
| 突发 | 156.25ms | 20210312 | 24189208 |
| 拥塞 | 15.625ms | 1701048 | 17717848 |
| 部分故障 | 140.625ms | 12496080 | 19980600 |
| 全池不可用 | 15.625ms | 1292584 | 25330848 |

这些 CPU/内存属于同时包含网关、HTTP 客户端、模拟上游的测试进程，结束堆不是峰值。SQL 不含初始化和最终断言，但可能包含异步账务/日志；Redis 数字是客户端命令数，不含 Lua 内部调用，保留 Provider、频率和账务读写是预期行为。准入只记录队列均值，首个有效输出读取非空 content 事件，不能用首个响应头替代。

第一次使用多连接内存 SQLite 时，突发触发 `database table is locked: database is deadlocked (6)`。失败日志保留为 `http-load-initial-sqlite-lock.log`。当前负载夹具明确使用单连接串行数据库事务，HTTP 与上游仍然并发。不能据此声称默认多连接 SQLite 突发或生产数据库吞吐已经通过；生产容量结论需要对应数据库与连接配置的代表性负载。

## 新旧准入核心比较

可重复脚本：[tests/admission-benchmark/compare.ps1](../../tests/admission-benchmark/compare.ps1)。旧核心来自固定提交 `a2d7371b2bf0a3ee50e7a69b895caa08730460b6`，只用于独立临时包；获取、Lua FIFO、20ms 轮询和释放保持原样。新核心调用当前 manager。每次完成自动移除临时 Go 源码，不把旧后端带回应用。

三轮相同输入，容量 8、队列 20、等待 100ms。低负载 80 次/1 worker/持槽 5ms；突发 80 次/16 workers/持槽 5ms；拥塞 160 次/64 workers/持槽 20ms。固定数量的 worker 完成后继续提交，属于封闭负载；不是相同外部到达率测试。

| 场景与核心 | 三轮成功数 | 成功准入 p95 范围 | Redis 命令/轮 | 分配字节/轮 |
| --- | --- | ---: | ---: | ---: |
| 低负载，旧 Redis | 80/80/80 | 0.726–0.781ms | 160 | 161344–162016 |
| 低负载，本地 FIFO | 80/80/80 | 低于本次计时分辨率 | 0 | 44272–44832 |
| 突发，旧 Redis | 42/33/44 | 83.199–83.483ms | 416–432 | 433600–611744 |
| 突发，本地 FIFO | 80/80/80 | 5.609–6.190ms | 0 | 65024–75952 |
| 拥塞，旧 Redis | 14/16/14 | 63.225–87.908ms | 390–391 | 472384–765464 |
| 拥塞，本地 FIFO | 28/28/28 | 61.096–61.471ms | 0 | 80944–97584 |

拥塞时两方案每轮均队满拒绝 132 次，旧核心另超时 12–14 次，本地没有额外超时。旧 FIFO 的轮询队首需要等下一次检查才能授槽，解释了突发下更长等待。此比较没有看到新核心持续性延迟或分配退化；确认的结论是并发 Redis 命令与轮询归零。

CPU 原始值由 Windows GetProcessTimes 与 Redis INFO CPU 读取，短批次受约 15.625ms 粒度影响。各轮新核心进程 CPU 为 0–15.625ms，旧核心为 0–46.875ms；零值不是零成本，不能作精确倍数结论。完整原始 JSON（包括墙钟、超时和堆值）保留在 `.local-tests/admission-comparison/4eb5b30851ff4185b270f58f2ff79517/results.json`。此范围没有 SQL、上游内容或重试，它们不是核心微基准的业务，不能伪报为这些指标已完成新旧对比。

## 本地改造清理记录（2026-09-16）

已移除 `service/route_lease.go`、`route_lease_queue.go`、`route_lease_coordinator.go` 和旧 Redis 槽测试 `route_lease_test.go`。共用资格复核、健康探测、Provider 维护及结算已迁出保留；原账务/频率多进程测试保留。应用不再调用旧获取、释放、续期或 Lua 排队，无双轨或自动回退后端。

新路由日志使用 admission_state。历史 lease_state 解码及 route_lease 故障码分类属于读取兼容，不执行 Redis 并发。剩余系统任务租约、Provider reservation、成功频率和账务机制不是待删残余。

旧改进状态、历次修复、验证、发布和统一路由文档在 `routing/archive/` 保留改造前工作树内容，现行入口改为当前契约及归档链接。研究报告、参考项目、原 overlay、数据库、未合并补丁、前端嵌入占位和证据均保留；没有整目录删除 `.local-tests`、`frontend/embed-dist` 或参考项目，没有访问 `docs/developer-notes`。仅删除本次重复 benchmark 临时 Go 源码，保留原始结果。

收尾时已正常关闭本任务创建的 MySQL、PostgreSQL、Redis，确认回环 13306、15432、16379 不再监听。保留 `.local-tests/single-instance/{mysql,postgres,redis}` 数据、日志及构建供复核。真实回环 Redis 的旧键清理工具 dry-run 返回 observed=0、unlinked=0；未执行生产清理。最终额外补齐 Provider 配置读取的请求 context，定向取消测试与受影响构建通过，证据为 `provider-context-final.log`；准入错误层级见 `admission-errors-final.log`。

本地实现与上述功能验证完成后，保留完整 HTTP 新旧性能对比及代表性数据库容量验收的缺项。顺序切换、限定旧键清理和回退步骤见 [发布说明](GATEWAY_RELEASE.md)；此阶段未执行服务器部署。

2026-09-16 的历史记录曾处于子模块未提交、未发布状态；该状态只描述当时的验收快照。2026-09-17 已在新服务器独立 bare 仓库发布应用提交，并在隔离 PostgreSQL 测试栈复验。根工作区的发布状态、服务器地址和后续测试证据以 `docs/server/REN2HUB_POSTGRES_TEST_20260917.md` 为准；根 gitlink 同步不替代子模块源码发布。

## 服务器审计与 PostgreSQL 复验（2026-09-17）

PostgreSQL 隔离压测在修正管理 API 限流夹具后，业务、账务、槽位守恒和故障转移均通过，但严格路由日志核验发现：真实发送结束后，后续候选筛选会清除当前 `selected_channel_id`，重试预算耗尽时还可能把未发送的候选写成最终渠道。修复后，最终决策日志从真实发送边界保存的最后渠道读取证据；没有真实发送的请求仍不伪造渠道。新增回归覆盖“发送 11 后候选 12 被拒绝”和“已选 12 但预算拒绝发送”两条路径，普通测试、race、vet 和构建均通过。

修复后的应用提交 `f99a1bd66594476a90fb470496380ec05a2ba232` 已发布到服务器独立 bare 仓库，并以不可变镜像在 PostgreSQL 15.18、Redis 7.4.10 隔离栈复验。报告目录为 `/srv/ren2hub-test/reports/20260916T171507.860099Z/`（UTC），最终 `success=true`、`cleanup_complete=true`、`route_evidence=true`。11 场景覆盖低负载和稳态 JSON/SSE、用户与渠道拥塞、A 忙 B 空、独立冷却、部分 503、全池不可用及共享重试预算；行为、账务、路由证据和槽位守恒全部通过。

原先管理 API 限流不足和最终渠道日志缺失的失败报告继续保留，不改写为通过。测试没有公网端口或真实供应商调用；PostgreSQL 备份经 `pg_restore --list` 验证可读，该检查不等于恢复演练。具体请求量、延迟、CPU/内存和数据库调用量由工作区 `docs/server/REN2HUB_POSTGRES_TEST_20260917.md` 统一记录，避免在多个文档复制易失同步的数据表。

该报告使用当时的容器 CPU/内存上限。随后工作区配置已移除应用、PostgreSQL、Redis、构建和压测辅助容器的全部 CPU/内存配额，健康核验通过；没有重跑完整 11 场景，原测量不能代表无配额环境。完整旧 HTTP 基线、长时间固定到达率压力、SSE/Realtime 长连接排空、真实供应商和公网 TLS 验收仍未完成。

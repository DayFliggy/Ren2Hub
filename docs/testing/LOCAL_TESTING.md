# 本地测试方案

Ren2Hub 的质量门槛在本地运行，CI 调用同一入口。Go、Vue 的业务测试留在源码附近；旧独立验收脚本、日期批次入口、失效 SDK 工具和退役路由模拟器停止维护。工作区入口为 `python3 -B scripts/test-local.py`，独立应用仓库入口为 `python3 -B scripts/testing/run.py`。

## 来源与取舍

测试入口最初参考以下只读快照；当前应用同步目标为 `b388140acfd80369ec6243b16869daff1b4051f4`，不增加 Git 上游。NewAPI 是唯一上游，另外两个项目只提供测试设计参考。

| 来源    | 固定提交                                   | 采用的方式                                                                                                  | 不照搬的方式                                                                                     |
| ------- | ------------------------------------------ | ----------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------ |
| NewAPI  | `e449cc700eb569cdfc2199cb73e97de46944f43c` | `.github/workflows/ci.yml`、`makefile` 的 root/relaykit/tokenkit 独立测试，原生 Go 测试与协议夹具           | 缺 MySQL/PostgreSQL DSN 后跳过，不能算完整数据库验收；不照搬 React 的 Bun 1.4.4 / `bun check -b` |
| Sub2API | `a34f373973bf547ccec301c7405eb45194e1f28a` | `backend/Makefile` 的分层入口，`backend/internal/repository/integration_harness_test.go` 的每轮真实新数据库 | 不增加 testcontainers 大依赖，不给上游共享测试批量加 build tag；本地缺 Docker 也失败             |
| AxonHub | `cbdf9a6d4a3dab1e4bfe5168ce5d80205c805913` | `.github/workflows/test.yml` 的独立模块验证、协议请求/响应/流夹具、迁移独立场景                             | 不按用户前缀清理已有库，不使用真实供应商 key，不以固定等待或自动重试掩盖失败                     |

源码链接：[NewAPI CI](https://github.com/QuantumNous/new-api/blob/e449cc700eb569cdfc2199cb73e97de46944f43c/.github/workflows/ci.yml)、[Sub2API 数据库设施](https://github.com/Wei-Shaw/sub2api/blob/a34f373973bf547ccec301c7405eb45194e1f28a/backend/internal/repository/integration_harness_test.go)、[AxonHub 测试入口](https://github.com/looplj/axonhub/blob/cbdf9a6d4a3dab1e4bfe5168ce5d80205c805913/Makefile)。NewAPI 的 React CI 已更新到 Bun 1.4.4 和 `bun check -b`；Ren2Hub 仍使用适配 Vue 的 Bun 1.4.0、vue-tsc、ESLint 和 Prettier。此处是源码设计参考，不代表上游测试已在本机运行。

## 运行入口

先安装匹配的 Go 1.26.10、Bun 1.4.0、Node 24、Python 3、Docker，执行 `cd frontend && bun install --frozen-lockfile`。Chromium 及其系统库需事先就绪。runner 不自动安装依赖、拉取镜像、连接真实供应商或申请凭据。Linux 网络 namespace 用于隔离真实浏览器实例。

在 Ren2Hub 目录执行：

```sh
python3 -B scripts/testing/run.py diagnostics
python3 -B scripts/testing/run.py known_upstream_restored
python3 -B scripts/testing/run.py unit
python3 -B scripts/testing/run.py databases
python3 -B scripts/testing/run.py static
python3 -B scripts/testing/run.py build
python3 -B scripts/testing/run.py race
python3 -B scripts/testing/run.py browser
python3 -B scripts/testing/run.py ui-contracts
python3 -B scripts/testing/run.py candidate --image <应用不可变镜像ID> --image-identity <应用identity.json> --simulator-image <模拟器不可变镜像ID> --simulator-identity <模拟器identity.json>
python3 -B scripts/testing/run.py candidate --image <应用不可变镜像ID> --image-identity <应用identity.json> --simulator-image <模拟器不可变镜像ID> --simulator-identity <模拟器identity.json> --previous-image ren2hub-test:<旧版完整SHA>-v1
python3 -B scripts/testing/run.py candidate --image <应用不可变镜像ID> --image-identity <应用identity.json> --simulator-image <模拟器不可变镜像ID> --simulator-identity <模拟器identity.json> --traffic-host-interface eth0
python3 -B scripts/testing/run.py all --image <应用不可变镜像ID> --image-identity <应用identity.json> --simulator-image <模拟器不可变镜像ID> --simulator-identity <模拟器identity.json>
```

| 层           | 实际验证                                                                           | 依赖及通过条件                                                                                                |
| ------------ | ---------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| diagnostics  | 显式白名单 Go 标签诊断、真实失败指纹和标签包 vet | 精确测试名；编译/超时/缺项/跳过/未知失败/意外通过均失败，本地失败不能归为上游 |
| unit         | Go 三模块、Vue 业务断言；httptest 模拟协议、计费、权限、路由、任务、工单           | 自动发现 Go 用例，每批最多 20 个，单次 `-timeout=60s`；检查 JSON 测试事件，零用例/失败/意外跳过均失败         |
| databases    | 空库初始化、旧账务拒绝、钱包/返利/充值、路由、管理操作、模型目录、任务、日志与迁移 | SQLite + 新 MySQL 8.4 + PostgreSQL 15.19；每个合同独立新库，检查每个方言的运行与通过事件                      |
| static       | Vue 类型、Lint、格式；三个 Go 模块 vet                                             | 全部退出 0；缺工具是失败                                                                                      |
| build        | Vue 构建、嵌入资源图、实际 Go 二进制与两个独立模块                                 | 不用 placeholder 代替生产资源验证，记录二进制 SHA256                                                          |
| race         | 轮询、并发资金、服务准入、WebSocket 结算、监听器连接计数、流量采样与读取并发       | 固定 Go 容器与 CGO；无 race、零跳过、实际用例非空                                                             |
| browser      | 全新实例真实登录/刷新/注销、权限、Token、邀请归属与设置交互                        | 桌面/手机 × 明/暗，每组独立数据库；核心流程直接调用真实后端，插件 UI 场景使用显式同源合同桩；不接已有服务器，不接受任意目标地址                         |
| ui-contracts | `frontend/tests/visual/` 的页面、组件交互、状态及响应式行为                        | 自动发现该目录全部 spec，再逐 ID 核对实际执行；零缺项、跳过、重试；模拟原生 API，不冒充真实后端，不做像素比较 |
| candidate    | 不可变镜像的账务、HTTP/SSE/WS/Compact、插件重启、管理/扩展与 SDK 合同；指定旧镜像时增加跨版本普通升级 | 全新隔离 PG、Redis、附件与密钥，internal 网络；精确余额/发送次数断言，分组报告全部通过 |

`unit` 只把数据库矩阵列为转交 `databases`，把嵌入产物测试转交 `build`；不会把它们计入 unit 通过数量。需要真实 VLLM 的可选用例明确不在本地方案内。`unit` 通过不等于 `all` 通过。数据库清单中测试被删除、改名或缺失不能静默减少覆盖。

工作区部署工具合同使用 `python3 -B scripts/test-local.py infra`，测试临时 Git 管线、安装器门槛、Nginx、生产核验器和密钥转换，不运行正式部署。平台工具缺失而跳过必须使整层不完整。Windows Plink 操作仍按连接技能执行，本地 Linux 结果不代表 Windows 运行验证。

## 隔离、证据与失败

路由审计使用独立 `channel-simulator` 项目的版本 1 服务镜像；模拟器不访问网关数据库，不实现路由。每个渠道一个端口与独立状态，网关通过原生渠道配置连接，控制接口仅用于故障和事件屏障。新 `routing` 阶段加入完整 candidate；旧独立路由模拟脚本仍退役。

```sh
python3 -B scripts/testing/build_routing_image.py
python3 -B scripts/testing/run.py routing --image sha256:<应用镜像ID> --simulator-image sha256:<模拟器镜像ID>
```

研究构建脚本从当前工作树复制不可变输入，记录构建前后指纹、base SHA、镜像 ID，以及实际 Go/Bun/Node/Docker 版本与二进制哈希。正式候选构建使用根工作区 `sh scripts/server/build-ren2hub-test.sh <已发布SHA> --output-dir <新的空目录>`，从内部 bare 创建干净检出；两个入口复用相同 schema 1 身份验证，均要求镜像内容指纹。未提交源码的 `candidate_sha=null`；不能用现有发布镜像代表修改后代码。`routing` 执行精确 Go 诊断和真实路由合同，不要求无关 SDK 运行时，始终 `deployment_eligible=false`。完整 candidate 同样强制诊断并执行 SDK、升级等原有门槛；`all` 不单独重复诊断，由 candidate 执行一次并合并结果。此前层失败仍按原规则中止，不能把未执行的诊断算成通过。本地失败优先退出 1，只有已知上游及其阻断才退出 2。2026-11-13 起完整 candidate 有一条精确发布豁免：未通过项必须与 `routing_result.py` 登记的上游缺陷及其阻断逐项一致，其余阶段全部通过，manifest 记录 `upstream_exemption`，发布校验器按同一函数复核。多一项、少一项、诊断变了或本地失败都不豁免，routing 单独运行始终 `deployment_eligible=false`。镜像带内容指纹时，无论 routing 或完整 candidate 都要求与当前工作树一致。

诊断执行器与发布复核共用 `routing_result.py` 中的完整探针清单；新增执行探针不会自动加入已批准的豁免集合。完整候选的 manifest 中，`success` 表示可发布，`candidate_success` 表示测试是否全部通过；存在精确豁免时后者仍为 false，不能把已知缺陷和阻断计入通过数。

路由报告使用 `pass`、`fail`、`known_upstream`、`blocked`、`not_run`。退出 0 表示所有必测项通过；退出 1 表示本地/未分类失败、缺项、源码变化或清理失败；退出 2 仅表示已确认上游缺陷及其阻断，不能计为通过或发布。供应商到达序号不是状态更新游标，等待挂起完成时保留原游标重复查询；不靠固定 sleep 猜测并发交错。HTTP 请求 ID、供应商实际命中、重试轨迹、状态、预期/实际扣费与并发释放共同构成证据。Go 行锁、精确时间测试单列，不冒充实际 HTTP。必测清单独立于执行列表；已知上游允许表绑定用例名和诊断。成功 WS create 从唯一原生结算日志关联请求 ID，取消及被拒绝的 WS 若不能取得 ID，明确记录缺失并保留供应商/余额证据，不伪造。每个可关联请求核对日志净扣费，零扣费附加短稳定窗口；完整边界随 routing 报告的 limits 保存。

产物默认进入 `.local-tests/run-*`，可用 `--output-parent` 放到仓库外；每轮新目录，禁止覆盖旧证据。记录源码 base SHA、干净时的 candidate SHA、未提交时的内容指纹、工具版本、每层报告、日志哈希与失败原因。私有目录 0700、凭据 0600；不把密钥和生产数据写进仓库。候选 SDK 可通过 `GATEWAY_SDK_PYTHON`、`GATEWAY_SDK_NODE`、`GATEWAY_SDK_NODE_MODULE` 指定现有依赖；未指定时使用仓库内 `.test-tools/` 默认路径。候选在启动容器前检查这三项，缺失时直接以 `explicit_sdk_*` 失败。服务器现行工作区不允许未跟踪目录，正式候选必须指向工作区外的运行时。`success` 只表示各阶段通过，正式更新另需 manifest 的 `deployment_eligible` 为 true（干净源码、镜像修订与 gitlink 一致）；直接运行 `candidate.py` 时 CLI 输出同时给出两者，经 `run.py` 运行时以 `report.json` 的 `stages.candidate` 和 `candidate-manifest.json` 为准。runner 校验运行时版本并记录包元数据哈希，但不保证依赖目录只读或位于仓库外；`.test-tools/` 被 Git 忽略，不属于源码身份。

候选默认不挂载宿主文件，应用使用主监听器计数。`run.py candidate|all` 或 `candidate.py` 传入 `--traffic-host-interface NAME`（`^[A-Za-z0-9_.-]{1,15}$`，非法时在建证据目录前失败）后，候选应用额外得到只读单文件挂载 `/proc/1/net/dev → /host/proc/net/dev` 和 `TRAFFIC_HOST_INTERFACE=NAME`。除本轮生成的 mock CA 证书外，这是候选唯一允许的宿主文件挂载；它是宿主网卡计数，不是数据路径。模式经私有 `target.json` 的 `traffic_host_interface`（监听器模式为空串）传给各合同组；隔离检查只在宿主模式放行这一项，源不对、可写、多出一项或要求时缺失都拒绝，CA 规则不变；`TRAFFIC_HOST_NET_DEV` 在宿主模式只能为空或 `/host/proc/net/dev`，监听器模式两变量都必须为空。

管理组的 `traffic_source_matches_configuration` 最多等约 45 秒，直到两次采样出现速率，再要求 `observability.stale` 为 false、`scope`/`interface` 与参数一致、`reason` 为空、两方向为有限非负数。监听器模式下本组轮询和健康检查都经过主监听器，继续等到两个方向都大于 0，主监听器漏接计数时报 `listener_traffic_not_counted`；宿主模式下候选在 internal 网络里，请求不经过宿主网卡，0 是有效读数。协议细节（SSE、WebSocket、partial I/O）由 Go 真实连接测试承担。管理报告记录实测的 `traffic`，隔离报告和 `candidate-manifest.json` 记录 `traffic: {scope, interface, mounts}`，挂载在清理前取自运行中容器的 inspect。这些 runner 合同不在 `unit` 层，单独执行 `python3 -B scripts/testing/test_runner.py`。

数据库和候选 runner 自己创建目标，不接收用户提供的 DSN，不继承生产数据库环境变量。容器均带本轮随机 label，清理前核验确切 ID 和 label，不用名称通配符、全局 prune、FLUSHDB 或清空旧库。失败退出非零，保留报告与日志；清理失败也不能算通过。`browser` profile 的真实浏览器实例在只有回环接口的独立 network/PID namespace 内运行，外网请求直接失败；`ui-contracts` 使用同源模拟 API 和独立 loopback Vite 端口，不把这层当作真实网络隔离。

`ui-contracts` 只证明浏览器行为与布局断言。`tests/visual/` 恢复自 9a63062227，没有经过复核的像素参考，因此这一层不声称像素一致；视觉回归需另行对照 9a63062227 截图人工复核，并单独记账。旧 `tests/contracts/` 的像素基线流程随该目录退役，其证据见 Ren2Hub `888498f1b19e08acbafae91fefe1b755048fa7eb:docs/testing/UI_REGRESSION_20261024.md`（`git show` 读取）。

`ui-contracts` 固定使用专用回环入口 `http://127.0.0.1:41739`，因为邀请链接会真实显示当前 origin。该端口被占用时直接失败，不回退随机端口；同一机器上的完整浏览器验收须串行。历史副本与真实后端 smoke 仍使用各自端口；每轮 `visual-origin.json` 记录实际值。

真实 OAuth、真实付费模型和真实支付不在本地验证范围。SDK 运行时版本必须匹配候选合同要求，缺依赖不能跳过。普通更新须使用 `--previous-image` 验证已有数据的升级；首次安装的全新库结果不能替代它。该合同不尝试让旧程序改写已经迁移的新库；生产回退仍须连同对应数据库、Redis 和附件整体切回。

本地报告、已发布源码、镜像候选及正式部署分别记录。应用部署仍需仓库对应发布流程；本地 runner 本身不部署、不改正式环境。正式入口只用工作区的 `verify-ren2hub-app.sh` 与 `verify-ren2hub-https.py` 做只读核验。

## 旧资产的迁移边界

| 旧资产                                                                    | 新位置与状态                                                                                                                |
| ------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| `scripts/server/ren2hub_*acceptance.py` 与各 `test_*` 薄入口              | 业务断言在 `scripts/testing/contracts/`，由 `candidate` 管理隔离环境；8 个基础阶段，普通升级另加 7 个阶段；旧入口删除 |
| Go `*_test.go` 与 Vue `src/**/*.spec.ts`                                  | 保留原生行为断言，统一发现并运行；共享SQLite fixture补齐用户硬删除的路由表                                                  |
| 日期审计 spec 的8组业务回归及必要fixtures                                 | 随 `frontend/tests/contracts/` 退役；`tests/visual/` 继续复用其中的 `native-core-fixture.ts` 与 `channel-core-fixtures.ts`  |
| 旧 `frontend/tests/live/`                                                 | 新 `frontend/tests/local/` 对实际产物执行登录、权限、Token、邀请与设置；旧页面穷举不再作为现行门槛                          |
| `frontend/tests/visual/` 与 `frontend/tests/contracts/`                   | 恢复 9a63062227 的 `tests/visual/` 作为 `ui-contracts` 套件；`tests/contracts/` 的 spec、清单与配置退役                     |
| 旧 SDK / admission benchmark / upstream-sync 独立工具及 routing-simulator | 不再存在的接口与实现引用退役；当前SDK合同在 candidate 层                                                                    |
| 工作区 scripts/tests                                                      | 发布/安装/核验/Nginx/密钥断言迁入 scripts/testing/infra；旧独立入口删除。Windows专用测试未在Linux运行，不声称连接层完整验证 |

数据库 runner 发现读取 DSN/dialect 的测试及其 helper 使用者；明确清单覆盖的项目必须执行，未分类新项目使门槛失败。共享 helper 的其他原生用例单列为 SQLite 覆盖，不能宣称全部三方。当前矩阵使用 MySQL 8.4 与 PostgreSQL 15.19，并记录实际镜像 ID、digest 和服务器版本；不覆盖历史 MySQL 5.7、PostgreSQL 9.6 或 ClickHouse，亦不证明现有生产 PostgreSQL 15.18 的所有版本差异。浏览器网络 namespace 隔离 IP 网络，不是完整文件系统沙箱；不得用不可信二进制或不可信浏览器测试。

普通升级传入不同修订的不可变旧镜像，按 `upgrade-baseline → upgrade-management → upgrade-before-seconds → upgrade-prepare-p → upgrade-resume-p → upgrade-after-seconds → upgrade-after-p` 执行，再运行全部 8 个基础阶段。前四阶段由旧镜像生成合成配置、余额、充值、工单附件和在途任务，随后切换新镜像；`preparing` 状态在确认任务已持久化后落盘，由新版本恢复并继续原任务，不重新提交。`seconds` 与 `p` 两种计量分别验证。

切换时仅替换本轮应用容器，保留同一 PostgreSQL、Redis、网络、密钥及附件卷。附件使用带本轮唯一 label 的专属 Docker volume；合同拒绝其他卷、路径、所有者或可写宿主绑定。应用、数据库和 Redis 均不接生产目录。切换前后比较配置、用户/Token、渠道、充值、路由与工单记录和附件的指纹，重复启动验证迁移幂等；在途任务单独断言恢复后只结算/退款一次、没有重复供应商提交，且旧版签发的产物链接仍可读取。所有阶段通过并清理成功后才可得到 `deployment_eligible=true`，同时还要求新镜像、干净应用提交与根仓库 gitlink 一致。

普通升级候选的模拟供应商会在阶段之间计划停服，因此旧、新容器均显式设置并核验 `TASK_POLL_MAX_FAILURES=0`，避免测试调度等待触发默认失败次数退款；该设置记录在候选报告，不修改生产默认值，原生任务超时仍有效。失败阈值由独立 Go 行为测试验证。`p` 分支指自定义 `u("p")` 数量字段，不将它冒称为 Omni 输入/输出 Token 计量覆盖；Omni 的最终 Token 合同由插件回归单独验证。

## 本轮构建证据与静态路由资格

包含 routing 的 candidate/all 和独立 routing 入口必须同时提供 `--image-identity` 与 `--simulator-identity`。两个独立构建入口成功后才原子写出 schema_version=1 的 identity.json：artifact、success/stable、不可变 image_id、source_before/after、revision/base_revision、binary 的路径/哈希、实际工具版本；模拟器额外声明协议版本。prepare-only 或失败进度不是成功构建证明。验收核对清单、镜像标签、实际二进制和工具版本并保存原始清单副本及哈希；应用还必须匹配当前工作树，未提交内容的 candidate_sha 为 null。模拟器不导入网关代码，网关不导入模拟器代码。

A4 已成为默认回归；诊断白名单只保留精确确认的上游问题，不按 TestUpstream 前缀匹配。只有 known_upstream/blocked 时退出 2，本地失败/缺项/清理错误退出 1 且优先于 2；发布例外只有上一节登记的那一条精确集合。预览使用 transport=http|websocket，WS 必须显式查询，不把 stream=true 当成 WS。普通 HTTP 断开用例检查在途继续、成功后结算和释放各一次，不要求共享实现立即取消退款。

渠道锁序扩展矩阵每个数据库独立执行，全部 23 个场景保留，每次 Go 测试仍为 60 秒超时。205 行夹具采用批量插入并核对 PostgreSQL 反向物理布局；不缩减受测渠道写操作或锁等待屏障。

## 2026-11-13 同步合同

隔离浏览器后端仅将登录关键操作的限流窗口缩短到 1 秒，保留原生启用状态和阈值，避免串套合成账号耗尽 20 分钟窗口；插件用例按服务端 `Retry-After` 有界重试 429，其他错误仍直接失败。核心真实后端流程不拦截业务 API；新增插件 UI 用例明确使用同源合同桩，真实权限拒绝与会话恢复仍调用隔离后端。

本批新增提供商账号登录/刷新/原账号任务轮询、请求与回复插件、内存/磁盘请求体独立游标、最终图片数量预扣、易支付 RSA、敏感词迁移和系统任务清理回归。请求处理范围只有全局、渠道和模型；上游业务分组用例改为无分组合同，退役范围不能计作通过。

旧候选升级检查只容许删除五个旧敏感词键并追加对应过滤实例，其他配置继续逐键比较；资金、Token、路由、工单和附件断言不放宽。重复启动比较完整快照。按 2026-11-14 后端回归合同，敏感词迁移失败恢复原生 SysError 后继续启动；失败期间旧键保留且过滤实例未追加，过滤不会生效。候选仍严格比较迁移结果并阻断失败，不能放宽五键例外。只切回旧镜像不会恢复旧配置，应在独立副本验证回退限制。历史 b20 的启动加固记录保留，现行行为以同步账本的新条目为准。

候选中的供应商、支付、登录、市场下载和安装上报均由本轮本地服务承接；未知失败、缺项、跳过或外联尝试均需诊断，不新增豁免。正式上线仍须单独执行部署流程。最终交叉审计使用 Claude Code 的明确模型 `claude/claude-opus-5-5`，审查与源码及候选身份一致后才标记就绪。

## 2026-11-14 增量同步验证

本批固定 b388140：moego alpha.3 改变公共 JSON 依赖，Go 三模块及三数据库结果重新运行；无 AVX 的 AES 路径使用隔离 QEMU 对比旧/新版，不能用 GODEBUG 代替指令环境。Seedream 5.5 覆盖预扣与结算数量、分档、自定义表达式、别名、multipart、异常载荷与退款；旧模型、管理员激活版本及任务提交版本身份保持回归。Vue 长配置验证覆盖桌面/手机及明暗主题，并回归插件登录回调。

原生 b388140 诊断与本地候选逐项比对，保留既有 MJ 本地子例限制，不扩张豁免。完整候选从实际正式旧镜像升级，另验证 f659 到本批的插件版本与在途任务兼容；所有证据与最终发布身份交 Claude Code Opus 5.5 独立审计。原始报告和最终索引在 `/home/work/ren2hub-upstream-b388-20261114`；未完成检查不能凭计划条目计为通过。

## 后端原生回归诊断

`known_upstream_restored` 与既有 `diagnostics` 独立运行。冻结清单为 `scripts/testing/known_upstream_restored.json`；每项绑定固定旧版 2382 与原生 b388 的完整身份、原始日志、正确断言、fixture 哈希及完整 Go 测试节点。执行器不能从本轮失败反推允许集合。超时、编译失败、skip、缺失或重复节点、意外通过、未知断言及源码变化均为失败。

最终审计补入三项视频限流范围诊断，总计 70 项；旧 67 项逐对象及顺序保持不变。新项另在独立 2382、bcaa 与 b388 树预跑，使用真实注册路由，不手拼替代生产链。历史首次 204 的手造终点夹具完整归档，迁移后首次到达无渠道的真实 503，第二次应 429 的历史期望仍保留为原生差异。正常视频回归另验证实际认领和未认领的限流范围，不将已接受差异计为正常通过。

这个通用行为诊断不使用 `-race`，不提供数据竞争检测覆盖；若日志出现 DATA RACE 则拒绝该证据，但不能据此声称已经运行 race。受影响并发路径由单独的普通 race 与已冻结定价竞争阶段验证。

候选与根发布校验器分别读取冻结清单和原始日志重算结果。新阶段的已确认原生差异仍记 `known_upstream_restored`，退出码为 2；`candidate_success=false` 与 `deployment_eligible` 分开表达。旧 12 项诊断及 30 项阻断名单不变。历史断言的逐项迁移见 `docs/upstream/NATIVE_B388_TEST_MIGRATIONS.json`，原文件归档不代表其断言已通过。

本批真实三库范围包括资金事务与持久化接点；只在 SQLite 执行的场景单列。原生恢复的风险必须披露，不能将恢复上游写成安全性提升或修复完成。CPU 依赖和工具链未变时可复用既有对照，其余已改变生产输入必须重新验证。

定价缓存的原生数据竞争另设 `known_upstream_race`，冻结清单为 `scripts/testing/known_upstream_race.json`。它只编译专用 build tag 夹具，按原生函数体、读写源码行文本、调用栈、完整 goroutine 创建链和对应关系验收；真实 race 必须复现，未知栈或未复现均阻断。该阶段 `success=false`、退出码 2 只表示精确原生差异可单独验收，不表示 race 通过。普通 race 与 `known_upstream_restored` 仍拒绝所有 DATA RACE。候选与发布校验器独立复核这一份报告，最终候选必须使用已发布提交重新执行。

完整行为退步和证据边界见 [本批披露](../upstream/NATIVE_B388_DISCLOSURES.md)。

### 已确认的原生日志计数竞争

用户另行批准保留与固定 b388 相同的 `logger/logger.go`。`known_upstream_logger_race` 单独冻结 16 个并行流式测试和 3 个双渠道任务测试、日志计数访问点、调用帧以及按 goroutine ID 绑定的完整创建链；正常 race 精确移交这些测试后，由本阶段保持相同参数和内部并发执行，不删除 `t.Parallel` 或降低并行度。没有竞争且所有业务断言通过时退出 0；只有精确已知计数竞争时退出 2、`success=false`；任何其他竞争、业务断言失败、缺少用例、跳过、编译错误或超时均阻断。此授权不修改定价竞争及旧 12+30 名单。最终候选与发布分别从已发布源码重算报告，脏工作树准备证据不能冒充最终候选。

普通 `ordinary_race` 由 `run.py race` 与候选调用同一执行器，固定 11 组、129 次顶层执行；`race_selection.json` 的 SHA 固定，19 个移交测试另由日志计数阶段保持原并发执行。候选记录普通检查的源码身份、命令、清单及原始日志，发布独立重算；任何数据竞争、缺项、跳过或断言失败阻断。资金 13 项定向 race 单独报告，不混入 129 的计数。

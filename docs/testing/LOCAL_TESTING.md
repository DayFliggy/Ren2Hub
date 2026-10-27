# 本地测试方案

Ren2Hub 的质量门槛在本地运行，CI 调用同一入口。Go、Vue 的业务测试留在源码附近；旧独立验收脚本、日期批次入口、失效 SDK 工具和退役路由模拟器停止维护。工作区入口为 `python3 -B scripts/test-local.py`，独立应用仓库入口为 `python3 -B scripts/testing/run.py`。

## 来源与取舍

本轮研究只读快照，不升级应用核心，也不增加 Git 上游。NewAPI 是唯一上游，另外两个项目只提供测试设计参考。

| 来源    | 固定提交                                   | 采用的方式                                                                                                  | 不照搬的方式                                                                         |
| ------- | ------------------------------------------ | ----------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------ |
| NewAPI  | `206e0fa5c600127a314a97f88592c067f784f4a6` | `.github/workflows/ci.yml`、`makefile` 的 root/relaykit/tokenkit 独立测试，原生 Go 测试与协议夹具           | 缺 MySQL/PostgreSQL DSN 后跳过，不能算完整数据库验收                                 |
| Sub2API | `a34f373973bf547ccec301c7405eb45194e1f28a` | `backend/Makefile` 的分层入口，`backend/internal/repository/integration_harness_test.go` 的每轮真实新数据库 | 不增加 testcontainers 大依赖，不给上游共享测试批量加 build tag；本地缺 Docker 也失败 |
| AxonHub | `cbdf9a6d4a3dab1e4bfe5168ce5d80205c805913` | `.github/workflows/test.yml` 的独立模块验证、协议请求/响应/流夹具、迁移独立场景                             | 不按用户前缀清理已有库，不使用真实供应商 key，不以固定等待或自动重试掩盖失败         |

源码链接：[NewAPI CI](https://github.com/QuantumNous/new-api/blob/206e0fa5c600127a314a97f88592c067f784f4a6/.github/workflows/ci.yml)、[Sub2API 数据库设施](https://github.com/Wei-Shaw/sub2api/blob/a34f373973bf547ccec301c7405eb45194e1f28a/backend/internal/repository/integration_harness_test.go)、[AxonHub 测试入口](https://github.com/looplj/axonhub/blob/cbdf9a6d4a3dab1e4bfe5168ce5d80205c805913/Makefile)。Opus 5.5 参与设计与脚本审查；研究是静态分析，不代表上游测试已在本机运行。

## 运行入口

先安装匹配的 Go 1.26.9、Bun 1.4.0、Node 24、Python 3、Docker，执行 `cd frontend && bun install --frozen-lockfile`。Chromium 及其系统库需事先就绪。runner 不自动安装依赖、拉取镜像、连接真实供应商或申请凭据。Linux 网络 namespace 用于隔离真实浏览器实例。

在 Ren2Hub 目录执行：

```sh
python3 -B scripts/testing/run.py unit
python3 -B scripts/testing/run.py databases
python3 -B scripts/testing/run.py static
python3 -B scripts/testing/run.py build
python3 -B scripts/testing/run.py race
python3 -B scripts/testing/run.py browser
python3 -B scripts/testing/run.py ui-contracts
python3 -B scripts/testing/run.py candidate --image ren2hub-test:<完整SHA>-v1
python3 -B scripts/testing/run.py all --image ren2hub-test:<完整SHA>-v1
```

| 层           | 实际验证                                                                           | 依赖及通过条件                                                                                                |
| ------------ | ---------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| unit         | Go 三模块、Vue 业务断言；httptest 模拟协议、计费、权限、路由、任务、工单           | 自动发现 Go 用例，每批最多 20 个，单次 `-timeout=60s`；检查 JSON 测试事件，零用例/失败/意外跳过均失败         |
| databases    | 空库初始化、旧账务拒绝、钱包/返利/充值、路由、管理操作、模型目录、任务、日志与迁移 | SQLite + 新 MySQL 8.4 + PostgreSQL 15.19；每个合同独立新库，检查每个方言的运行与通过事件                      |
| static       | Vue 类型、Lint、格式；三个 Go 模块 vet                                             | 全部退出 0；缺工具是失败                                                                                      |
| build        | Vue 构建、嵌入资源图、实际 Go 二进制与两个独立模块                                 | 不用 placeholder 代替生产资源验证，记录二进制 SHA256                                                          |
| race         | 轮询、并发资金、服务准入、WebSocket 结算                                           | 固定 Go 容器与 CGO；无 race、零跳过、实际用例非空                                                             |
| browser      | 全新实例真实登录/刷新/注销、权限、Token、邀请归属与设置交互                        | 桌面/手机 × 明/暗，每组独立数据库；不拦截业务 API，不接已有服务器，不接受任意目标地址                         |
| ui-contracts | `frontend/tests/visual/` 的页面、组件交互、状态及响应式行为                        | 自动发现该目录全部 spec，再逐 ID 核对实际执行；零缺项、跳过、重试；模拟原生 API，不冒充真实后端，不做像素比较 |
| candidate    | 不可变镜像的账务、HTTP/SSE/WS/Compact、插件重启、管理/扩展与 SDK 合同              | 全新 PG、Redis、附件与密钥，internal 网络；精确余额/发送次数断言，分组报告全部通过                            |

`unit` 只把数据库矩阵列为转交 `databases`，把嵌入产物测试转交 `build`；不会把它们计入 unit 通过数量。需要真实 VLLM 的可选用例明确不在本地方案内。`unit` 通过不等于 `all` 通过。数据库清单中测试被删除、改名或缺失不能静默减少覆盖。

工作区部署工具合同使用 `python3 -B scripts/test-local.py infra`，测试临时 Git 管线、安装器门槛、Nginx、生产核验器和密钥转换，不运行正式部署。平台工具缺失而跳过必须使整层不完整。Windows Plink 操作仍按连接技能执行，本地 Linux 结果不代表 Windows 运行验证。

## 隔离、证据与失败

产物默认进入 `.local-tests/run-*`，可用 `--output-parent` 放到仓库外；每轮新目录，禁止覆盖旧证据。记录源码 base SHA、干净时的 candidate SHA、未提交时的内容指纹、工具版本、每层报告、日志哈希与失败原因。私有目录 0700、凭据 0600；不把密钥和生产数据写进仓库。候选 SDK 可通过 `GATEWAY_SDK_PYTHON`、`GATEWAY_SDK_NODE`、`GATEWAY_SDK_NODE_MODULE` 指定现有依赖；未指定时使用仓库内 `.test-tools/` 默认路径。候选在启动容器前检查这三项，缺失时直接以 `explicit_sdk_*` 失败。服务器现行工作区不允许未跟踪目录，正式候选必须指向工作区外的运行时。`success` 只表示各阶段通过，正式更新另需 manifest 的 `deployment_eligible` 为 true（干净源码、镜像修订与 gitlink 一致）；直接运行 `candidate.py` 时 CLI 输出同时给出两者，经 `run.py` 运行时以 `report.json` 的 `stages.candidate` 和 `candidate-manifest.json` 为准。runner 校验运行时版本并记录包元数据哈希，但不保证依赖目录只读或位于仓库外；`.test-tools/` 被 Git 忽略，不属于源码身份。

数据库和候选 runner 自己创建目标，不接收用户提供的 DSN，不继承生产数据库环境变量。容器均带本轮随机 label，清理前核验确切 ID 和 label，不用名称通配符、全局 prune、FLUSHDB 或清空旧库。失败退出非零，保留报告与日志；清理失败也不能算通过。`browser` profile 的真实浏览器实例在只有回环接口的独立 network/PID namespace 内运行，外网请求直接失败；`ui-contracts` 使用同源模拟 API 和独立 loopback Vite 端口，不把这层当作真实网络隔离。

`ui-contracts` 只证明浏览器行为与布局断言。`tests/visual/` 恢复自 9a63062227，没有经过复核的像素参考，因此这一层不声称像素一致；视觉回归需另行对照 9a63062227 截图人工复核，并单独记账。旧 `tests/contracts/` 的像素基线流程随该目录退役，其证据见 [UI_REGRESSION_20261024.md](UI_REGRESSION_20261024.md)。

`ui-contracts` 固定使用专用回环入口 `http://127.0.0.1:41739`，因为邀请链接会真实显示当前 origin。该端口被占用时直接失败，不回退随机端口；同一机器上的完整浏览器验收须串行。历史副本与真实后端 smoke 仍使用各自端口；每轮 `visual-origin.json` 记录实际值。

真实 OAuth、真实付费模型和真实支付不在本地验证范围。SDK 运行时版本必须匹配候选合同要求，缺依赖不能跳过。新库部署不再以旧版本改写新库的回滚演练作为门槛；回退必须连同旧数据库、Redis 和附件整体切回，不能只换镜像。

本地报告、已发布源码、镜像候选及正式部署分别记录。应用部署仍需仓库对应发布流程；本地 runner 本身不部署、不改正式环境。正式入口只用工作区的 `verify-ren2hub-app.sh` 与 `verify-ren2hub-https.py` 做只读核验。

## 旧资产的迁移边界

| 旧资产                                                                    | 新位置与状态                                                                                                                |
| ------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| `scripts/server/ren2hub_*acceptance.py` 与各 `test_*` 薄入口              | 业务断言在 `scripts/testing/contracts/`，由 `candidate` 管理隔离环境和全部7阶段；旧入口删除                                 |
| Go `*_test.go` 与 Vue `src/**/*.spec.ts`                                  | 保留原生行为断言，统一发现并运行；共享SQLite fixture补齐用户硬删除的路由表                                                  |
| 日期审计 spec 的8组业务回归及必要fixtures                                 | 随 `frontend/tests/contracts/` 退役；`tests/visual/` 继续复用其中的 `native-core-fixture.ts` 与 `channel-core-fixtures.ts`  |
| 旧 `frontend/tests/live/`                                                 | 新 `frontend/tests/local/` 对实际产物执行登录、权限、Token、邀请与设置；旧页面穷举不再作为现行门槛                          |
| `frontend/tests/visual/` 与 `frontend/tests/contracts/`                   | 恢复 9a63062227 的 `tests/visual/` 作为 `ui-contracts` 套件；`tests/contracts/` 的 spec、清单与配置退役                     |
| 旧 SDK / admission benchmark / upstream-sync 独立工具及 routing-simulator | 不再存在的接口与实现引用退役；当前SDK合同在 candidate 层                                                                    |
| 工作区 scripts/tests                                                      | 发布/安装/核验/Nginx/密钥断言迁入 scripts/testing/infra；旧独立入口删除。Windows专用测试未在Linux运行，不声称连接层完整验证 |

数据库 runner 发现读取 DSN/dialect 的测试及其 helper 使用者；明确清单覆盖的项目必须执行，未分类新项目使门槛失败。共享 helper 的其他原生用例单列为 SQLite 覆盖，不能宣称全部三方。当前矩阵使用 MySQL 8.4 与 PostgreSQL 15.19，并记录实际镜像 ID、digest 和服务器版本；不覆盖历史 MySQL 5.7、PostgreSQL 9.6 或 ClickHouse，亦不证明现有生产 PostgreSQL 15.18 的所有版本差异。浏览器网络 namespace 隔离 IP 网络，不是完整文件系统沙箱；不得用不可信二进制或不可信浏览器测试。

插件候选当前只验证同版本重启。旧驱动的 preparing 状态恢复后不重新提交（`--resume-state`）及 usage-quantity `p` 选项没有迁入；这些崩溃恢复/计量分支明确未覆盖。下次普通更新需要另外恢复跨版本迁移与重启合同，不能复用本次全新安装结果。

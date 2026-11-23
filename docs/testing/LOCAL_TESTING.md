# 本地测试方案

Ren2Hub 的质量门槛在本地运行，CI 调用同一入口。Go 和 Vue 的业务测试放在源码旁；候选合同在 `scripts/testing/contracts/`。独立应用仓库的入口是 `python3 -B scripts/testing/run.py`，工作区入口 `python3 -B scripts/test-local.py` 把同样的参数转交给它。

默认按改动模块测试：选择方法、模块对照、部署候选的范围和测试编写规则见[按改动模块测试](MODULE_TESTING.md)，已知保护测试见[功能链路](FUNCTION_CHAINS.md)。本文是各层的参考：每层验证什么、怎样隔离、证据怎样判定。

## 前提

需要 Go 1.26.10、Bun 1.4.0、Node 24、Python 3 和 Docker，前端先执行 `cd frontend && bun install --frozen-lockfile`，Chromium 及其系统库事先就绪。runner 不安装依赖、不拉取镜像、不连接真实供应商、不申请凭据；缺工具是失败，不是跳过。RenAI 服务器上的工具路径由工作区的 `skills/task-testing/scripts/ren2hub-env.sh` 设置。

## 各层

```sh
python3 -B scripts/testing/run.py registry
python3 -B scripts/testing/run.py targeted [--go 目录[:正则]] [--db 测试名] [--race 目录] [--vitest 路径] [--lint 路径] [--visual spec] [--python]
python3 -B scripts/testing/run.py affected [--base <提交>] [--plan] [--skip <层>]
python3 -B scripts/testing/run.py <unit|databases|static|build|race|browser|ui-contracts|diagnostics>
python3 -B scripts/testing/run.py <known_upstream_restored|known_upstream_race|known_upstream_logger_race>
python3 -B scripts/testing/run.py routing --image <应用镜像ID> --image-identity <identity.json> --simulator-image <模拟器镜像ID> --simulator-identity <identity.json>
python3 -B scripts/testing/run.py candidate --scope <auto[,部分]|boot|full> --image <应用镜像ID> --image-identity <identity.json> \
  --simulator-image <模拟器镜像ID> --simulator-identity <identity.json> [--base <基线完整SHA>] [--traffic-host-interface eth0]
python3 -B scripts/testing/run.py <quick|all> [候选参数]
```

| 层           | 实际验证                                                                                              | 依赖及通过条件                                                                                                                                                                                   |
| ------------ | ----------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| registry     | 冻结夹具哈希、race 固定选择及测试是否仍存在、数据库矩阵登记、核心来源清单的本地哈希                   | 只读源码，不到一秒；逐项列出偏离的文件和修正办法。`quick`、`all`、`affected`、`targeted` 都先运行它；迭代档位里来源清单的偏离只记为提交前待办                                                    |
| targeted     | 只运行点名的 Go 包（可按测试名筛选）、已登记的三库测试、race 组、Vitest、前端静态检查和视觉 spec      | 选择写入 `selection.json`；名称不存在或筛选为空时报错；报告标记 `release_evidence: false`                                                                                                        |
| affected     | 只运行与 `--base`（默认 HEAD 与 origin/main 的分叉点）以来改动相关的 Go、数据库、race、Vue 与视觉用例 | 选择写入 `selection.json`；`--skip` 跳过的层写入 `skipped_layers`；自动选择达到整层（某一层的八成以上）时拒绝执行；报告标记 `release_evidence: false`，不能写成 `quick` 或 `all` 通过            |
| unit         | Go 三模块、Vue 业务断言；httptest 模拟协议、计费、权限、路由、任务、工单                              | 自动发现 Go 用例，每批最多 20 个，单次 `-timeout=60s`；检查 JSON 测试事件，零用例、失败、意外跳过均失败。数据库矩阵测试转交 `databases`，嵌入产物测试转交 `build`，不计入 unit 的通过数          |
| databases    | 空库初始化、旧账务拒绝、钱包、返利、充值、路由、管理操作、模型目录、任务、日志与迁移                  | SQLite、新建的 MySQL 8.4 和 PostgreSQL 15.19；每包只编译一次测试二进制，每个合同以独立进程、独立新库和 `-test.timeout=60s` 运行，报告记录二进制 SHA256，并检查每个方言的运行与通过事件           |
| static       | Vue 类型、Lint、格式；三个 Go 模块 vet                                                                | 全部退出 0                                                                                                                                                                                       |
| build        | Vue 构建、嵌入资源图、实际 Go 二进制与两个独立模块                                                    | 不用 placeholder 代替生产资源，记录二进制 SHA256                                                                                                                                                 |
| race         | 轮询、并发资金、服务准入、WebSocket 结算、监听器连接计数、流量采样与读取并发                          | 固定 Go 容器与 CGO；`race_selection.json` 的固定选择，零跳过、实际用例非空，任何 DATA RACE 都失败                                                                                                |
| browser      | 全新实例的真实登录、刷新、注销、权限、Token、邀请归属与设置交互                                       | 桌面和手机 × 明暗，每组独立数据库；核心流程直接调用真实后端，插件 UI 场景使用显式同源合同桩；只在仅有回环接口的 network/PID namespace 里运行，不接已有服务器                                     |
| ui-contracts | `frontend/tests/visual/` 的页面、组件交互、状态及响应式行为                                           | 自动发现全部 spec，再逐 ID 核对实际执行；零缺项、跳过、重试；模拟原生 API，不冒充真实后端；除 `bonus-card.spec.ts` 的快照外不做像素比较                                                          |
| diagnostics  | 白名单内的 Go 标签诊断、真实失败指纹和标签包 vet                                                      | 精确测试名；编译失败、超时、缺项、跳过、未知失败、意外通过均失败，本地失败不能归为上游                                                                                                           |
| routing      | 用独立的渠道模拟器镜像执行真实路由合同和精确 Go 诊断                                                  | 需要已构建的镜像和两份 `identity.json`；始终 `deployment_eligible=false`                                                                                                                         |
| candidate    | 不可变镜像的账务、HTTP/SSE/WS/Compact、插件重启、管理与扩展、SDK 合同                                 | 全新隔离的 PostgreSQL、Redis、附件与密钥，internal 网络；精确的余额和发送次数断言。`--scope` 必填，只运行所选部分，`full` 是完整候选；范围候选不运行 routing、诊断和 race，manifest 记录 `scope` |
| quick        | registry、unit、static、build，对应上游 NewAPI 的日常 CI                                              | 全量档位，只在明确要求时运行；任一层失败即中止，不能代替 `all`                                                                                                                                   |
| all          | registry、unit、databases、static、build、race、browser、ui-contracts 和完整候选                      | 全量档位，只在明确要求时运行；诊断由完整候选执行一次，不单独重复                                                                                                                                 |

`targeted` 与 `affected` 共用同一个执行器，区别只在选择来自点名还是差异。工作区部署工具的合同使用 `python3 -B scripts/test-local.py infra [模块|类|方法]`，不运行正式部署。

## affected 的选择规则

规则在 `scripts/testing/affected.py`，由 `test_affected.py` 保护：

- Go：用 `go list` 包图求改动包。生产文件（含删除和受构建标签排除的文件）及 `go.mod`、`go.sum` 的改动会传给反向依赖和测试导入方；`_test.go` 与 `testdata/` 改动只选本包。测试源码中能解析到真实路径的相对路径（如 `../frontend/tests/fixtures/native`）也会选中读取它的包。选中包运行 unit 批次和 vet；与数据库矩阵、race 清单重合的包只运行这些包的套件，数据库仍启动三种引擎。
- Vue：`vitest related` 按导入图选用例；typecheck 在任何前端代码改动时整体运行；lint 与 Prettier 只查改动文件；`package.json`、`bun.lock`、tsconfig、Vite、Vitest、ESLint、Prettier 配置改动时对应层整体运行。改动构建输入时列出 build。
- 视觉：spec 按 URL 打开页面，导入图关联不到 `src`。`ui-contracts` 和 `targeted --visual` 每次通过后，按 Playwright 单 worker 串行的时间窗把 Vite 返回的 `/src`、`/public` 模块归到各 spec，写入 `.local-tests/ui-impact.json`；局部运行只刷新运行过的 spec，没有影响图时从空图开始，已删除的 spec 被移除。affected 只选加载过改动模块的 spec、改动的 spec 和导入改动辅助文件的 spec；没有影响记录的 spec 和关联不到任何 spec 的前端改动不自动选中，在计划输出里单独列出，由计划按行为点名。改动全局配置时选择整层。影响图只决定迭代选择，不进入发布证据。
- Python：改动 `scripts/testing/` 下的文件或 `scripts/refresh-core-source-delta.py` 时运行 `scripts/testing` 的 Python 单测。
- 不覆盖的层：候选合同（`scripts/testing/contracts/`、`candidate.py`、Dockerfile）、真实后端 browser（`browser.py`、`tests/local/`）、diagnostics 和 `known_upstream_*` 的输入改动只在报告中列出应单独处置的档位，不自动启动；改动了冻结清单绑定的夹具或源码文件时同样列出对应的 `known_upstream_*` 档位。被工作区镜像构建器加载的 candidate 模块改动时提示工作区 infra。

## 隔离与证据

产物默认进入 `.local-tests/run-*`，可用 `--output-parent` 放到仓库外；每轮新目录，不覆盖旧证据。报告记录源码 base SHA、干净时的 candidate SHA、未提交时的内容指纹、工具版本、每层报告、日志哈希与失败原因；未提交源码的 `candidate_sha=null`。运行结束时再核对一次源码指纹，期间改过受跟踪文件的运行判失败。私有目录 0700、凭据 0600，密钥和生产数据不写进仓库。

数据库层和候选自己创建目标，不接收用户提供的 DSN，不继承生产数据库的环境变量。容器带本轮随机 label，清理前核验确切 ID 和 label，不用名称通配符、全局 prune、FLUSHDB 或清空旧库。失败退出非零并保留报告与日志；清理失败也不算通过。

数据库矩阵使用 MySQL 8.4 与 PostgreSQL 15.19，并记录实际镜像 ID、digest 和服务器版本。runner 发现读取 DSN 或方言环境变量的测试及其 helper 的使用者：清单覆盖的测试必须执行，未登记的新测试使门槛失败，测试被删除或改名不能静默减少覆盖。只在 SQLite 执行的场景单列，不能写成三库覆盖。矩阵不覆盖历史的 MySQL 5.7、PostgreSQL 9.6 和 ClickHouse。

`browser` 的浏览器实例在只有回环接口的独立 network/PID namespace 内运行，外网请求直接失败；它隔离的是 IP 网络，不是完整的文件系统沙箱。隔离后端把登录关键操作的限流窗口缩短到 1 秒，阈值和启用状态保持原生；插件用例按服务端的 `Retry-After` 有界重试 429，其他错误直接失败。

`ui-contracts` 使用同源模拟 API 和固定的回环入口 `http://127.0.0.1:41739`（邀请链接会显示当前 origin），端口被占用时直接失败，不回退随机端口；同一台机器上的浏览器层要串行运行。视觉 spec 的模拟接口取自 `frontend/tests/contracts/` 的 `native-core-fixture.ts`、`channel-core-fixtures.ts` 和 `frontend/tests/visual/fixtures.ts`。

真实 OAuth、真实付费模型、真实支付和需要真实 VLLM 的可选用例不在本地验证范围。Nginx 配置没有自动化测试；Windows 的连接工具只能在 Windows 上验证。

## 镜像与候选

候选只接受带身份清单的不可变镜像。研究用的镜像由 `python3 -B scripts/testing/build_routing_image.py` 从当前工作树构建；用于发布的镜像由工作区的 `sh scripts/server/build-ren2hub-test.sh <已发布SHA> --output-dir <新的空目录>` 从内部 bare 仓库的干净检出构建。两个入口都在成功后才原子写出 schema 1 的 `identity.json`：不可变 image ID、构建前后的源码指纹、修订、二进制的路径与哈希、实际工具版本，模拟器另外声明协议版本。候选核对清单、镜像标签、实际二进制和工具版本，镜像的内容指纹必须与当前工作树一致；不能用现有发布镜像代表修改后的代码。

完整候选的基础阶段是 `billing`、`ws`、`compact`、`plugin-before`、`plugin-after`、`upstream_sync`（管理与扩展）、`sdk` 和 `routing`，并执行诊断、原生恢复诊断和三组 race；`--scope` 选择其中的部分，规则见[部署候选的范围](MODULE_TESTING.md#部署候选的范围)。供应商、支付、登录、市场下载和安装上报都由本轮的本地服务承接；未知失败、缺项、跳过或外联尝试都是失败。

SDK 运行时通过 `GATEWAY_SDK_PYTHON`、`GATEWAY_SDK_NODE`、`GATEWAY_SDK_NODE_MODULE` 指定，未指定时使用仓库内被 Git 忽略的 `.test-tools/`；候选在启动容器前检查这三项，缺失时以 `explicit_sdk_*` 失败，没有选 `sdk` 部分的范围候选不检查。

候选默认不挂载宿主文件，应用使用主监听器计数。传入 `--traffic-host-interface NAME`（`^[A-Za-z0-9_.-]{1,15}$`）后，候选应用额外得到只读的单文件挂载 `/proc/1/net/dev → /host/proc/net/dev` 和 `TRAFFIC_HOST_INTERFACE=NAME`。除本轮生成的 mock CA 证书外，这是候选唯一允许的宿主文件挂载。管理组的 `traffic_source_matches_configuration` 核对读数的 `scope`、`interface` 与参数一致；监听器模式下还要求两个方向都大于 0，宿主模式下候选在 internal 网络里，0 是有效读数。隔离报告和 `candidate-manifest.json` 记录 `traffic: {scope, interface, mounts}`。

manifest 里 `candidate_success` 表示测试是否全部通过，`deployment_eligible`（也就是 `success`）表示可以用于正式更新：所选阶段通过、清理成功、源码干净、镜像修订与源码一致。完整候选有一条精确的发布豁免：未通过项与 `routing_result.py` 登记的上游缺陷及其阻断逐项一致、其余阶段全部通过时，manifest 记录 `upstream_exemption`，`candidate_success` 仍为 false；多一项、少一项、诊断变了或有本地失败都不豁免。

### 路由合同

`routing` 使用独立 `channel-simulator` 项目的版本 1 服务镜像。模拟器不访问网关数据库，也不实现路由；每个渠道一个端口和独立状态，网关通过原生渠道配置连接，控制接口只用于故障注入和事件屏障。报告的状态是 `pass`、`fail`、`known_upstream`、`blocked`、`not_run`：退出 0 表示所有必测项通过；退出 1 表示本地或未分类的失败、缺项、源码变化或清理失败，并优先于 2；退出 2 只表示已确认的上游缺陷及其阻断，不能计为通过。必测清单独立于执行列表，已知上游的允许表绑定用例名和诊断；新增的执行探针不会自动进入豁免集合。

证据由 HTTP 请求 ID、供应商实际命中、重试轨迹、状态、预期与实际扣费、并发释放共同构成。供应商到达序号不是状态更新游标；不靠固定 sleep 猜测并发交错；取不到请求 ID 的取消和被拒绝的 WebSocket 请求明确记录缺失，保留供应商和余额证据。

## 已接受的原生差异

后端按[同步账本](../upstream/SYNC_LEDGER.md)记录的冻结目标对齐上游后，与上游相同的已知缺陷不在本地修复，而是冻结成诊断，防止它们被悄悄扩大或被误报为通过。这些档位的退出码 2 和 `success=false` 只表示“与冻结的原生差异逐项一致”，不表示测试通过；完整候选里它们仍使 `candidate_success=false`。完整候选读取冻结清单和原始日志重算结果。行为退步和证据边界见[披露](../upstream/NATIVE_B388_DISCLOSURES.md)，历史断言的逐项去向见 `docs/upstream/NATIVE_B388_TEST_MIGRATIONS.json`。

| 档位                         | 冻结清单                                             | 内容                                                                                                                                                                                      |
| ---------------------------- | ---------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `diagnostics`                | `routing_result.py` 的探针清单                       | 既有的 12 项上游诊断和 30 项阻断，不再扩大                                                                                                                                                |
| `known_upstream_restored`    | `scripts/testing/known_upstream_restored.json`       | 每项绑定旧版与原生 b388 的完整身份、原始日志、正确断言、夹具哈希和完整的 Go 测试节点。每个包只编译一次 `-tags=known_upstream_restored` 的测试二进制，再逐项以独立进程运行。不使用 `-race` |
| `known_upstream_race`        | `scripts/testing/known_upstream_race.json`           | 定价缓存的原生数据竞争：按原生函数体、读写源码行、调用栈和完整的 goroutine 创建链验收，竞争必须复现，未知栈或未复现都阻断                                                                 |
| `known_upstream_logger_race` | `scripts/testing/known_upstream_logger_race.json`    | 与 b388 相同的 `logger/logger.go` 日志计数竞争：冻结并行流式测试和双渠道任务测试、访问点、调用帧和创建链，保持原并发度执行。没有竞争且断言全部通过时退出 0，只有精确的已知竞争时退出 2    |
| `race`（普通）               | `race_selection.json` 与 `ordinary_race.py` 的固定值 | 固定的组和执行数；移交给日志计数档位的测试不在这里重复。任何数据竞争、缺项、跳过或断言失败都阻断                                                                                          |

共同规则：执行器不能从本轮失败反推允许集合；超时、编译失败、skip、缺失或重复节点、意外通过、未知断言、源码变化都是失败；新增、缺失或行为变化阻断交付。改动冻结对象后，`registry` 会指出偏离的文件和要重跑的档位：先在固定的旧版与原生树上重新取得对照，再更新该清单的全部哈希，最后重跑对应档位确认冻结断言照原样复现。最终候选必须从已发布的提交重新执行，脏工作树上的准备证据不能代替。

## 发布边界

本地报告、已发布源码、镜像候选和正式部署分别记录。runner 不部署、不改正式环境；应用部署走工作区的更新流程，正式入口使用工作区的 `ren2hub-app.py status` 和 `verify-ren2hub-https.py` 核验。

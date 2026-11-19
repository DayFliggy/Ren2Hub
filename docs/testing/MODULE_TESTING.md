# 按改动模块测试

默认只测本次改动涉及的模块。全量档位（`quick`、`all`、完整候选、整套 `unit`／`databases`／`race`／`ui-contracts`）只在用户明确要求全量测试时运行，部署也不例外。各层的验证内容、隔离和证据规则见[本地测试方案](LOCAL_TESTING.md)。

路由准入、候选排序、质量观测、用量回执或路由合同改动时，`affected --plan` 列出自动路由负载、质量和缓存仿真的完整用例名。`targeted` 根据当前差异补跑同一组用例，已选择 service 单测时合并运行。每个名字必须实际通过。范围候选按旧镜像到新版本的差异自动加入 `routing-workload`，将源码仿真报告写入候选证据。镜像的 HTTP、SSE、WebSocket 与账务合同仍通过独立 `routing` 档验证。

## 流程

每个任务和实施计划先写测试计划，再按计划运行。计划格式、八类接点和“定向不扩大”的规则在工作区的 task-testing 技能（`skills/task-testing/`）里，下面是 Ren2Hub 的摘要。

1. 写计划：基线（迭代时是分支起点，部署时是现行正式提交）；改动 → 功能 → 性质；每个外部可观察的行为按接点逐项判断，点名检查，必要接点的保护测试写完整名字。不涉及的接点写依据。
2. 运行 `registry`（登记检查，不到一秒）。它核对冻结夹具哈希、race 固定选择、数据库矩阵登记和核心来源清单，并列出偏离的具体文件；失败时先修正登记。
3. 迭代中用 `targeted` 运行计划里点名的项；失败后只重跑包含失败项的最小单位，定位后再修。不改代码的重跑只算诊断，见“排查”。
4. 提交前对最终 diff 运行 `affected --plan --base <基线>`，把它列出的每一项要么点名、要么写一行理由，记入计划的“机械对照”。计划外的项只有在 diff 变化、工具指出未处置项、失败根因在计划外、影响面说不清或用户点名时才加入。
5. 只有影响面说不清（依赖、构建配置、全局样式、被大量使用的共用函数）时才运行不带 `--plan` 的 `affected`：先点名已确定的测试，再对仍说不清的层运行，已覆盖的层用 `--skip` 跳过。自动选择达到整层（某一层的八成以上）时它拒绝执行，因为接近整层的运行属于全量档位；改 `model/` 通常会让三库矩阵和 race 都达到整层，这时直接点名 `--db` 测试和 `--race` 包。
6. 部署前运行按范围的候选 `candidate --scope auto[,部分]`，见“部署候选的范围”。
7. 报告写明运行了什么、没有运行什么及原因。定向结果不能写成全量通过。

```sh
python3 -B scripts/testing/run.py registry
python3 -B scripts/testing/run.py targeted --go 'service:^(TestResolveTopUpBonusFromHourlyWindows|TestEvaluateTopUpBonusFormula)$' \
  --db TestTopUpBonusDatabaseMatrix --race service \
  --vitest frontend/src/api/__tests__/topupBonus.spec.ts --lint frontend/src/api/topupBonus.ts \
  --visual topup-bonus.spec.ts bonus-card.spec.ts
python3 -B scripts/testing/run.py affected --plan --base <基线提交>
python3 -B scripts/testing/run.py affected --base <基线提交> --skip <已点名覆盖的层>
python3 -B scripts/testing/run.py candidate --scope auto[,billing,…] --previous-image <现行正式镜像> \
  --image <镜像ID> --image-identity <identity.json> --simulator-image <镜像ID> --simulator-identity <identity.json>
```

`targeted` 的选择项可以任意组合，至少给一项：

| 选择项             | 运行内容                                                                                                                                                                                                  |
| ------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `--go 目录[:正则]` | 该包的 vet 和单测，按所属模块以 `GOWORK=off` 运行；正则按顶层测试名筛选。没有可运行的测试（只有矩阵测试，或正则没有匹配）时报错，不把空运行算通过。同一个包在一次运行里只能给一个正则，计划各行的名字用 ` | `合并；正则写成`^A$`或`^(A | B)$` 这样的完整名字时逐个核对名字存在，缺一个就报错，其他正则只保证非空 |
| `--db 测试名`      | 已登记的矩阵测试，在 SQLite、MySQL、PostgreSQL 各跑一次；单方言测试只跑登记的方言；未登记的名称报错                                                                                                       |
| `--race 目录`      | 该包在固定 race 选择中的全部组（`model`、`service`、`controller`、`common`、`relay`、`relay/helper`），不能只选一个组                                                                                     |
| `--vitest 路径`    | `frontend/` 下的 spec 或源码文件。点名的每个 spec 都必须运行到用例；点名源码时由一次 `vitest related` 找用例（启动约 20 到 35 秒），只保证整批非空，所以计划里点名 spec                                   |
| `--lint 路径`      | 对所列前端文件运行 ESLint 和 Prettier，并整体 typecheck                                                                                                                                                   |
| `--visual spec`    | `frontend/tests/visual/` 下的整个 spec，包括它内部遍历的全部视口、主题和语言；运行时记录该 spec 加载的模块，供 `affected` 选择                                                                            |
| `--python`         | `scripts/testing` 的整套 Python 单测，4 到 5 秒，这一层不再细分                                                                                                                                           |

`targeted` 和 `affected` 可以直接在工作树里运行，不要求提交或独立克隆；报告标记 `release_evidence: false`。`affected --skip <层>` 跳过指定层并把它写入报告的 `skipped_layers`；`--plan` 同时给出 `--skip` 时，被跳过的层照样列出并标明。

实测耗时（4 核主机）：充值加赠这块改动用 `targeted` 运行一个 Go 包筛选、一项三库测试、一个 Vitest 文件和一个视觉 spec，共 4 分钟；同一批改动的 `affected` 是 66 分钟，其中视觉层 37 分钟、三库矩阵 10 分钟、race 9 分钟。`quick` 约 13 分钟。

## 接点与选择项

接点的定义和必测条件见 task-testing 技能，这里只列 Ren2Hub 用什么检查它。

| 接点           | 检查                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| -------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 逻辑           | `--go 包:^(测试名)$`；前端 `--vitest <spec>`                                                                                                                                                                                                                                                                                                                                                                                                    |
| 入口与权限     | 经过真实注册的 router、middleware 测试（调用 `SetApiRouter`、`SetRelayRouter` 等，例如 `router/access_token_scope_test.go`）；浏览器端的真实会话用 `run.py browser`。手工挂载中间件的 controller 测试和直接调用 handler 的合同测试不能代替                                                                                                                                                                                                      |
| 调用方         | 语义受影响的调用方：点名其包内的行为测试。只有编译依赖：对这些调用方所在的包运行 `GOWORK=off go vet <包…>`（vet 连同测试文件一起编译，`go build` 不编译测试文件）；`relaykit`、`tokenkit` 在各自目录 `GOWORK=off go vet ./...`，改它们的导出接口时根模块的调用方也要检查                                                                                                                                                                        |
| 状态           | `--db <矩阵测试>`；改变已存数据读法时加升级种子（见“编写与更新测试”）                                                                                                                                                                                                                                                                                                                                                                           |
| 合同两侧       | 生产侧是 controller 的 `TestFrontendContract*`，用 `-update` 改写 `frontend/tests/fixtures/native/*.json`；消费侧是直接导入该 JSON 的 Vitest spec，以及经 `frontend/tests/contracts/native-core-fixture.ts` 用 `readFileSync` 读取它的视觉 spec。后者不在导入图里，按夹具文件名在 `frontend/tests/` 里搜索。视觉 spec 内联的 `page.route` 模拟与后端没有机械联系，不算合同的消费侧。供应商和支付协议的合同在候选的 `scripts/testing/contracts/` |
| 并发与时序     | `--race <包>`，只含 `race_selection.json` 登记的组；登记外的并发测试用 `--go` 运行，没有 race 证据，计划里写明                                                                                                                                                                                                                                                                                                                                  |
| 呈现           | `--visual <spec>`；`--lint <改动文件>`                                                                                                                                                                                                                                                                                                                                                                                                          |
| 生命周期与交付 | `run.py build`（何时必须运行见下）；升级演练是候选的 `upgrade` 部分，只证明种子覆盖到的数据；改了冻结对象时运行 `registry` 指出的 `known_upstream_*` 档位                                                                                                                                                                                                                                                                                       |

`affected` 对任何 `frontend/src/` 改动都列出构建。以下改动运行 `run.py build`：新增路由、页面或 `frontend/public/` 静态资源；构建配置（`vite.config.ts`、`tsconfig*.json`、`package.json`、`bun.lock`）；嵌入脚本 `scripts/prepare-frontend-embed.mjs`；`go:embed` 的目录；Go 依赖。其余前端改动由 typecheck 和部署时的镜像构建覆盖，计划写一行理由。

## 模块对照

| 改动位置                                                                | 迭代中点名                                                                                                                      | 部署候选在 `auto` 之外追加的部分                                              |
| ----------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| `model/` 表结构、迁移、事务，`setting/` 配置解析，根 `main.go`          | `--go model:^(<测试名>)$`，涉及的矩阵测试用 `--db`，并发路径加 `--race model`                                                   | 无；改变已存数据的读法时先给升级合同补旧数据种子                              |
| `controller/`、`router/` 的支付、充值、兑换、工单、管理接口             | `--go controller:^(<测试名>)$`、`--go router:^(<测试名>)$`，三库管理测试用 `--db`                                               | 无（`upgrade` 已带 `management`）                                             |
| `service/`、`middleware/` 的预扣、结算、鉴权、限流                      | `--go service:^(<测试名>)$`、`--go middleware:^(<测试名>)$`，并发路径加 `--race service`                                        | `billing`                                                                     |
| `relay/`、`relaykit/`、`tokenkit/` 协议转换与计量                       | `--go relay/<子包>:^(<测试名>)$`、`--go relaykit/<包>:^(<测试名>)$`，流式生命周期加 `--race relay/helper`                       | `billing`；WebSocket 加 `websocket`，Compact 加 `compact`，SDK 兼容面加 `sdk` |
| `plugins/` 和任务提交、轮询、结算                                       | `--go plugins:^(<测试名>)$`、`--go service:^(<任务测试名>)$`，加 `--race service`                                               | `plugin`                                                                      |
| 渠道选择、Token 路由                                                    | 对应包的 `--go 包:^(<测试名>)$`，已知测试见[功能链路](FUNCTION_CHAINS.md)                                                       | 不属于范围候选；单独运行 `run.py routing`                                     |
| `logger/logger.go`、`model/pricing.go`、`native_restored_*` 等冻结对象  | `registry` 指出的档位：`known_upstream_restored`、`known_upstream_race`、`known_upstream_logger_race`                           | 无                                                                            |
| `frontend/src` 组件、视图、store、API                                   | `--vitest <源码或 spec>`、`--lint <改动文件>`、`--visual <相关 spec>`                                                           | 无；`auto` 不选任何阶段，只核对镜像并启动                                     |
| 前端文案、全局样式                                                      | `--lint <改动文件>`；`--visual` 运行断言了这些文案或样式的 spec（按 i18n 键或类名在 `frontend/tests/visual/` 里搜索）           | 同上                                                                          |
| 登录、会话、权限的真实后端流程（`frontend/tests/local/`、`browser.py`） | `run.py browser`                                                                                                                | 无                                                                            |
| `scripts/testing/*.py`                                                  | `--python`                                                                                                                      | 无                                                                            |
| `scripts/testing/contracts/`                                            | 候选中对应的部分                                                                                                                | 同左                                                                          |
| `go.mod`、`go.sum`、`Dockerfile`、`package.json`、`bun.lock`、构建配置  | 先按调用方规则点名直接使用改动依赖的包；仍说不清时对那几层运行 `affected`，自动选择达到整层（某一层的八成以上）时按全量档位处理 | 无                                                                            |
| 文档                                                                    | 不运行测试；检查 diff、链接和格式                                                                                               | 不需要部署                                                                    |

后端各行的改动都会让 `auto` 选中 `upgrade`（连带 `management`），表中只列还要追加的部分。

三库矩阵包含 `model`、`controller`、`service`、`middleware`、`pkg/perf_metrics` 的登记测试，完整清单在 `scripts/testing/database.py`。工作区部署脚本（根仓库 `scripts/server/`）用 `python3 -B scripts/test-local.py infra [模块|类|方法]`，路径与测试模块的对照在 task-testing 技能的根仓库适配里。

## 功能链路

资金、权限和路由几条链路已知的保护测试与缺口见[功能链路](FUNCTION_CHAINS.md)。它不是完整清单，运行范围由接点判断决定，不整行运行；任务触及其中没有的功能时，把计划里查到的测试补成一节。

## 部署候选的范围

`candidate --scope` 决定候选运行哪些部分，必须显式给出，漏写时报错而不是默认运行完整候选。规则在 `scripts/testing/release_scope.py`，候选和工作区的发布校验器加载同一份：

- `auto`：比较旧镜像修订与当前提交。差异只含 `frontend/`、`docs/`、`scripts/`、`.github/`、`.agents/`、根目录的 Markdown 和 `*_test.go` 时不选任何阶段；含其他任何文件时加入 `upgrade`。启动时会迁移表结构、解析已存配置、重置内置角色策略、注册 OAuth 提供方并编译已有插件，这些代码分布在多个包，`go:embed` 还会把整棵目录（含其中的 `testdata/`）嵌入程序，所以规则按“改动是否进入服务端程序”划分，不逐个列路径。
- 部分名：`upgrade`、`billing`、`websocket`、`compact`、`plugin`、`management`、`sdk`，逗号分隔，可与 `auto` 同用。`plugin` 自动带上 `billing`，`upgrade` 自动带上 `management`（回读旧版写入的工单附件）。
- `boot`：不选阶段，只核对镜像身份并在全新实例启动。
- `full`：完整候选，含 routing、12 项诊断、原生恢复诊断和三组 race。只在明确要求全量时使用；`all` 档位和首次安装始终用它。

范围候选的 manifest 记录 `scope`，只列所选阶段的报告。没有运行的 routing、诊断和 race 不出现在 manifest 里，也不能写成通过。更新执行器在停机前按现行正式版本复核，以下情况拒绝：

- 现行正式提交到新提交的差异按规则需要的部分没有运行。
- 本次更新改变了流量来源（启用或撤销宿主网卡计数），而候选没有运行 `management`；这项变更只有它实测。
- 报告与范围不符，或所选阶段有失败。
- 升级演练的旧镜像不是现行正式镜像本身；提交相同但镜像 ID 不同的另一次构建不算。
- 候选容器的计数挂载或网卡配置与正式 Compose 不同。候选要带与正式一致的 `--traffic-host-interface`。

## 排查

- `registry` 失败：输出按检查项列出文件和修正办法，先处理它，再运行其他层。
- Go 测试失败：`--go 包:^测试名$` 单独重跑；日志在本轮证据目录的 `go-unit/`。需要更快的循环时直接 `go test ./包 -run '^测试名$' -count=1 -timeout=60s`。
- 只在某个数据库失败：`--db 测试名`，逐方言日志在 `databases/matrix/NN-测试名.jsonl`。
- 视觉 spec 失败：`--visual spec`，截图和 trace 在 `ui-contracts/artifacts/`。
- 回归测试是否有效：用 `go test -overlay` 把旧实现换回去运行一次，新测试应当失败；不需要改动工作树。
- 计划外的测试失败：失败项属于被改动波及的反向依赖，点名加进计划（修订记录写触发和证据）后继续定位。
- 不改代码重跑后通过：两次结果都记，这一项保持未解决，直到查明原因或在基线上复现为既有问题；诊断重跑以一次为限。

## 编写与更新测试

测试放在源码旁，由对应层自动发现；不被任何层运行的测试文件不入库。新测试保护一个真实行为、合同或回归，不照抄实现，也不断言文件布局。

| 测试类型           | 位置                                                                                                | 新增、改名或删除时同步                                                                                                                                                                                                                                                                    |
| ------------------ | --------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Go 单测            | 源码旁 `*_test.go`，单项 60 秒内，不允许 skip                                                       | 核心目录的 Go 文件改动后，提交前刷新 `docs/upstream/CORE_SOURCE_DELTA.json`                                                                                                                                                                                                               |
| 三库矩阵测试       | 读取 `TEST_*_DSN` 或方言环境变量的 Go 测试                                                          | 登记到 `scripts/testing/database.py`；未登记时它在 `unit` 里因 skip 失败                                                                                                                                                                                                                  |
| race 测试          | 并发路径的 Go 测试                                                                                  | `race_selection.json` 的组、`ordinary_race.py` 的 `FROZEN_SELECTION` 和计数一起更新                                                                                                                                                                                                       |
| 冻结诊断夹具       | `known_upstream_*.json` 绑定的文件                                                                  | 内容改变后更新该文件的全部哈希，并重跑对应的 `known_upstream_*` 档位，确认冻结断言照原样复现                                                                                                                                                                                              |
| Vitest             | `frontend/src/**/*.spec.ts`                                                                         | jsdom 没有布局，尺寸和触控结论用视觉 spec 验证                                                                                                                                                                                                                                            |
| 前端合同夹具       | `frontend/tests/fixtures/native/*.json`，由 controller 的 `TestFrontendContract*` 加 `-update` 生成 | 改写内容在 diff 里审阅，再用新夹具运行消费侧的 Vitest 与视觉 spec                                                                                                                                                                                                                         |
| 视觉 spec          | `frontend/tests/visual/*.spec.ts`，模拟接口取自 `tests/contracts/` 和 `tests/visual/fixtures.ts`    | 像素基线只有 `bonus-card.spec.ts-snapshots/`：首次运行写入缺失基线并判失败，第二次必须零差异通过，人工对照后提交。新 spec 第一次 `--visual` 运行后才进入影响图                                                                                                                            |
| 真实后端浏览器流程 | `frontend/tests/local/`                                                                             | 由 `browser.py` 启动隔离后端                                                                                                                                                                                                                                                              |
| 候选合同           | `scripts/testing/contracts/`                                                                        | 新部分要同时进 `release_scope.py`                                                                                                                                                                                                                                                         |
| 升级合同           | `scripts/testing/upgrade_contract.py` 与 `candidate.py` 的升级种子                                  | 改动改变已存数据的读取、迁移或启动时改写时，给旧镜像阶段补对应的种子数据，并在升级后比较；演练只能证明种子覆盖到的数据。给升级快照里的表加列时同步 `ADDED_COLUMNS`（目前每张表只支持一列，且升级后必须全为 0），否则候选报 `upgrade_changed_persisted_contract_data`；改后运行 `--python` |
| runner 自身        | `scripts/testing/test_*.py`                                                                         | `--python`                                                                                                                                                                                                                                                                                |

步骤：先写出会失败的测试并确认它因目标行为失败，再改实现；用 `targeted` 运行它和同包相关测试；运行 `registry` 处理登记项；提交前刷新来源清单。新增、改名或删除保护测试时，同步[功能链路](FUNCTION_CHAINS.md)里引用它的条目。

## 一次性测试文件

临时探针、审计用的故障注入测试、overlay 替换文件、一次性启动脚本和冒烟克隆放在工作区 `tmp/`（根仓库忽略），任务结束时删除，不进仓库。`.local-tests/run-*` 是单轮证据：发布用到的复制进该次发布的证据目录，其余在任务结束时删除；`.local-tests/ui-impact.json` 是视觉选择用的影响图，保留。可重复使用的输入（模拟器镜像身份、SDK 运行时、浏览器库）放在固定位置，不放进按日期命名的证据目录。

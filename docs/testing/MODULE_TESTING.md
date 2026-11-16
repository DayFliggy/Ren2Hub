# 按改动模块测试

默认只测本次改动涉及的模块。全量档位（`quick`、`all`、完整候选、整套 `unit`／`databases`／`race`／`ui-contracts`）只在用户明确要求全量测试时运行，部署也不例外。各层的验证内容、隔离和证据规则见[本地测试方案](LOCAL_TESTING.md)。

## 流程

1. 列出改动文件（`git diff --name-only <基线>`），按下表判断涉及哪些模块。
2. 迭代中用 `targeted` 点名运行相关测试；失败后只重跑失败的那一项，定位到具体包、测试或 spec 再修。
3. 提交前，改动跨多个包或说不清影响面时运行一次 `affected`，由导入图自动选择；能点名覆盖的小改动用 `targeted` 即可。
4. 部署前运行按范围的候选 `candidate --scope auto`。它核对镜像身份并在全新实例启动；差异含前端、文档、脚本和测试文件以外的改动时自动加上升级演练。
5. 报告里写明实际运行了什么、没有运行什么。定向结果不能写成全量通过。

每个档位先运行 `registry`（登记检查，不到一秒）。它核对冻结夹具哈希、race 固定选择、数据库矩阵登记和核心来源清单，并列出偏离的具体文件；这些偏离以前要等数据库矩阵、race 或候选运行到一半才暴露。

```sh
python3 -B scripts/testing/run.py registry
python3 -B scripts/testing/run.py targeted --go model:^TestTopUpBonus --db TestTopUpBonusDatabaseMatrix \
  --vitest frontend/src/api/__tests__/topupBonus.spec.ts --lint frontend/src/api/topupBonus.ts \
  --visual bonus-card.spec.ts
python3 -B scripts/testing/run.py affected --base <基线提交> [--skip ui-contracts]
python3 -B scripts/testing/run.py candidate --scope auto[,billing,…] --previous-image <现行正式镜像> \
  --image <镜像ID> --image-identity <identity.json> --simulator-image <镜像ID> --simulator-identity <identity.json>
```

`targeted` 的选择项可以任意组合，至少给一项：

| 选择项             | 运行内容                                                                                                          |
| ------------------ | ----------------------------------------------------------------------------------------------------------------- |
| `--go 目录[:正则]` | 该包的 vet 和单测；正则按顶层测试名筛选。没有可运行的测试（只有矩阵测试，或正则没有匹配）时报错，不把空运行算通过 |
| `--db 测试名`      | 已登记的矩阵测试，在 SQLite、MySQL、PostgreSQL 各跑一次；未登记的名称报错                                         |
| `--race 目录`      | 该包在固定 race 选择中的组（`model`、`service`、`controller`、`common`、`relay`、`relay/helper`）                 |
| `--vitest 路径`    | `frontend/` 下的 spec 或源码文件，按导入图运行相关 Vitest 用例；一个用例都没有时报错                              |
| `--lint 路径`      | 对所列前端文件运行 ESLint 和 Prettier，并整体 typecheck                                                           |
| `--visual spec`    | `frontend/tests/visual/` 下的 spec 文件                                                                           |
| `--python`         | `scripts/testing` 的 Python 单测                                                                                  |

`targeted` 和 `affected` 可以直接在工作树里运行，不要求提交或独立克隆；报告标记 `release_evidence: false`。`affected --skip <层>` 跳过指定层并把它写入报告的 `skipped_layers`。常见用法是文案或全局样式改动让 `affected` 选中全部视觉 spec 时，跳过 `ui-contracts`，再用 `targeted --visual` 运行断言了这些文案或样式的 spec。

实测耗时（4 核主机）：充值加赠这块改动用 `targeted` 运行一个 Go 包筛选、一项三库测试、一个 Vitest 文件和一个视觉 spec，共 4 分钟；同一批改动的 `affected` 是 66 分钟，其中视觉层 37 分钟、三库矩阵 10 分钟、race 9 分钟。`quick` 约 13 分钟。

## 模块对照

| 改动位置                                                                | 迭代中点名                                                                                            | 部署候选在 `auto` 之外追加的部分                                              |
| ----------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| `model/` 表结构、迁移、事务，`setting/` 配置解析，根 `main.go`          | `--go model:<正则>`，涉及的矩阵测试用 `--db`，并发路径加 `--race model`                               | 无；改变已存数据的读法时先给升级合同补旧数据种子                              |
| `controller/`、`router/` 的支付、充值、兑换、工单、管理接口             | `--go controller:<正则>`，三库管理测试用 `--db`                                                       | 无（`upgrade` 已带 `management`）                                             |
| `service/`、`middleware/` 的预扣、结算、鉴权、限流                      | `--go service:<正则>`、`--go middleware`，并发路径加 `--race service`                                 | `billing`                                                                     |
| `relay/`、`relaykit/`、`tokenkit/` 协议转换与计量                       | `--go relay/<子包>`，流式生命周期加 `--race relay/helper`                                             | `billing`；WebSocket 加 `websocket`，Compact 加 `compact`，SDK 兼容面加 `sdk` |
| `plugins/` 和任务提交、轮询、结算                                       | `--go plugins`、`--go service:Task`，加 `--race service`                                              | `plugin`                                                                      |
| 渠道选择、Token 路由                                                    | 对应包的 `--go`                                                                                       | 不属于范围候选；单独运行 `run.py routing`                                     |
| `logger/logger.go`、`model/pricing.go`、`native_restored_*` 等冻结对象  | `registry` 指出的档位：`known_upstream_restored`、`known_upstream_race`、`known_upstream_logger_race` | 无                                                                            |
| `frontend/src` 组件、视图、store、API                                   | `--vitest <源码或 spec>`、`--lint <改动文件>`、`--visual <相关 spec>`                                 | 无；`auto` 不选任何阶段，只核对镜像并启动                                     |
| 前端文案、全局样式                                                      | `--lint <改动文件>`，`--visual` 运行断言了这些文案或样式的 spec                                       | 同上                                                                          |
| 登录、会话、权限的真实后端流程（`frontend/tests/local/`、`browser.py`） | `run.py browser`                                                                                      | 无                                                                            |
| `scripts/testing/*.py`                                                  | `--python`                                                                                            | 无                                                                            |
| `scripts/testing/contracts/`                                            | 候选中对应的部分                                                                                      | 同左                                                                          |
| `go.mod`、`go.sum`、`Dockerfile`、`package.json`、`bun.lock`、构建配置  | `affected`（这些输入让对应层整体运行）                                                                | 无                                                                            |
| 文档                                                                    | 不运行测试；检查 diff、链接和格式                                                                     | 不需要部署                                                                    |

后端各行的改动都会让 `auto` 选中 `upgrade`（连带 `management`），表中只列还要追加的部分。

三库矩阵包含 `model`、`controller`、`service`、`middleware`、`pkg/perf_metrics` 的 161 项登记测试，清单在 `scripts/testing/database.py`。工作区部署脚本（根仓库 `scripts/server/`）用 `python3 -B scripts/test-local.py infra`。

## 部署候选的范围

`candidate --scope` 决定候选运行哪些部分，规则在 `scripts/testing/release_scope.py`，候选和工作区的发布校验器加载同一份：

- `auto`：比较旧镜像修订与当前提交。差异只含 `frontend/`、`docs/`、`scripts/`、`.github/`、`.agents/`、Markdown、`*_test.go` 和 `testdata/` 时不选任何阶段；含其他任何文件时加入 `upgrade`。启动时会迁移表结构、解析已存配置、重置内置角色策略、注册 OAuth 提供方并编译已有插件，这些代码分布在多个包，所以规则按“改动是否进入服务端程序”划分，不逐个列路径。
- 部分名：`upgrade`、`billing`、`websocket`、`compact`、`plugin`、`management`、`sdk`，逗号分隔，可与 `auto` 同用。`plugin` 自动带上 `billing`，`upgrade` 自动带上 `management`（回读旧版写入的工单附件）。
- `boot`：不选阶段，只核对镜像身份并在全新实例启动。
- `full`（默认）：完整候选，含 routing、12 项诊断、原生恢复诊断和三组 race。只在明确要求全量时使用；`all` 档位和首次安装始终用它。

范围候选的 manifest 记录 `scope`，只列所选阶段的报告。没有运行的 routing、诊断和 race 不出现在 manifest 里，也不能写成通过。更新执行器在停机前按现行正式版本复核，以下情况拒绝：

- 现行正式提交到新提交的差异按规则需要的部分没有运行。
- 本次更新改变了流量来源（启用或撤销宿主网卡计数），而候选没有运行 `management`；这项变更只有它实测。
- 报告与范围不符，或所选阶段有失败。
- 升级演练的旧镜像不是现行正式镜像本身；提交相同但镜像 ID 不同的另一次构建不算。
- 候选容器的计数挂载或网卡配置与正式 Compose 不同。候选要带与正式一致的 `--traffic-host-interface`。

## 排查

- `registry` 失败：输出按检查项列出文件和修正办法，先处理它，再运行其他层。
- Go 测试失败：`--go 包:^测试名$` 单独重跑；日志在本轮证据目录的 `go-unit/`。需要更快的循环时直接 `go test ./包 -run 测试名 -count=1`。
- 只在某个数据库失败：`--db 测试名`，逐方言日志在 `databases/matrix/NN-测试名.jsonl`。
- 视觉 spec 失败：`--visual spec`，截图和 trace 在 `ui-contracts/artifacts/`。
- 回归测试是否有效：用 `go test -overlay` 把旧实现换回去运行一次，新测试应当失败；不需要改动工作树。
- 定向通过、`affected` 失败：失败项属于被改动波及的反向依赖，把它加进 `targeted` 选择后继续定位。

## 编写与更新测试

测试放在源码旁，由对应层自动发现；不被任何层运行的测试文件不入库。新测试保护一个真实行为、合同或回归，不照抄实现，也不断言文件布局。

| 测试类型           | 位置                                                                                             | 新增、改名或删除时同步                                                                                                 |
| ------------------ | ------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------- |
| Go 单测            | 源码旁 `*_test.go`，单项 60 秒内，不允许 skip                                                    | 核心目录的 Go 文件改动后，提交前刷新 `docs/upstream/CORE_SOURCE_DELTA.json`                                            |
| 三库矩阵测试       | 读取 `TEST_*_DSN` 或方言环境变量的 Go 测试                                                       | 登记到 `scripts/testing/database.py`；未登记时它在 `unit` 里因 skip 失败                                               |
| race 测试          | 并发路径的 Go 测试                                                                               | `race_selection.json` 的组、`ordinary_race.py` 的 `FROZEN_SELECTION` 和计数一起更新                                    |
| 冻结诊断夹具       | `known_upstream_*.json` 绑定的文件                                                               | 内容改变后更新该文件的全部哈希，并重跑对应的 `known_upstream_*` 档位，确认冻结断言照原样复现                           |
| Vitest             | `frontend/src/**/*.spec.ts`                                                                      | jsdom 没有布局，尺寸和触控结论用视觉 spec 验证                                                                         |
| 视觉 spec          | `frontend/tests/visual/*.spec.ts`，模拟接口取自 `tests/contracts/` 和 `tests/visual/fixtures.ts` | 像素基线只有 `bonus-card.spec.ts-snapshots/`：首次运行写入缺失基线并判失败，第二次必须零差异通过，人工对照后提交       |
| 真实后端浏览器流程 | `frontend/tests/local/`                                                                          | 由 `browser.py` 启动隔离后端                                                                                           |
| 候选合同           | `scripts/testing/contracts/`                                                                     | 新部分要同时进 `release_scope.py`                                                                                      |
| 升级合同           | `scripts/testing/upgrade_contract.py` 与 `candidate.py` 的升级种子                               | 改动改变已存数据的读取、迁移或启动时改写时，给旧镜像阶段补对应的种子数据，并在升级后比较；演练只能证明种子覆盖到的数据 |
| runner 自身        | `scripts/testing/test_*.py`                                                                      | `--python`                                                                                                             |

步骤：先写出会失败的测试并确认它因目标行为失败，再改实现；用 `targeted` 运行它和同包相关测试；运行 `registry` 处理登记项；提交前刷新来源清单。

## 一次性测试文件

临时探针、审计用的故障注入测试、overlay 替换文件、一次性启动脚本和冒烟克隆放在工作区 `tmp/`（根仓库忽略），任务结束时删除，不进仓库。`.local-tests/run-*` 是单轮证据：发布用到的复制进该次发布的证据目录，其余在任务结束时删除；`.local-tests/ui-impact.json` 是视觉选择用的影响图，保留。可重复使用的输入（模拟器镜像身份、SDK 运行时、浏览器库）放在固定位置，不放进按日期命名的证据目录。

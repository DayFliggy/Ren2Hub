# 依赖维护合同

主模块、RelayKit、TokenKit 分别维护 Go module；Vue 与 Electron 分别维护 `package.json` 和 `bun.lock`。Go 工作区不能替代独立模块验证。Web 前端只使用 Vue，React、Redux、AntD 和 Semi 不属于当前构建。

判断依赖是否可以删除时，同时核对源码、测试、平台专用文件、配置和构建脚本。直接依赖没有业务代码 import，并不意味着可删除：编译器、测试工具、数据库测试库、桌面下载工具和传递依赖都可能有实际用途。不得为消除漏洞告警删除仍被调用的包，或用忽略规则制造审计通过。

## 当前版本边界

构建固定使用 Go 1.26.9 和 Bun 1.4.0。主模块声明 `go 1.26.0`，RelayKit 保留 `go 1.25.1`，TokenKit 保留 `go 1.26`；不由一次依赖修补统一独立模块的最低语言版本。

主模块使用 `compress 1.18.7`，RelayKit 独立选择 `x/text 0.41.0`，主模块仍选择其原有 `x/text 0.42.0`。压缩库由请求 Zstd 解压、ClickHouse 日志传输和 profiling 使用；文本库通过 RelayKit 的 `samber/lo` 实际引入，不能移除。Go 安全补丁和来源见[同步账本](upstream/SYNC_LEDGER.md#依赖与构建)。

Vue 的直接依赖用于页面、状态、路由、国际化、图表、HTTP、Markdown 和 YAML；其开发依赖用于编译、CSS、类型、格式与测试。Electron 41.10.6、electron-builder 和 cross-env 分别用于桌面运行时、打包与跨平台开发脚本。当前用途审计未发现可删的现用框架包；root `go.sum` 中两行旧 moejs alpha.5 校验和已由 tidy 清理，现用 alpha.6 保留。这属于锁记录清理，不代表运行依赖数量减少。

## 未消除的告警

| 包与位置 | 当前边界 | 后续处置 |
| --- | --- | --- |
| Vue 工具链 `braces 3.0.3` | 高危，暂无兼容修复；与已修复的 brace-expansion 不同 | 跟踪已发布修复，见同步队列 SYNC1013-06 |
| Tailwind 3 → postcss-nested 6 → `postcss-selector-parser 6.1.4` | 中危；另一个 7 分支已升至 7.1.6 | 等待兼容更新，不能未经验收强制跨主版本覆盖 |
| Electron 打包下载链 `sprintf-js 1.1.3` | 中危，暂无已发布修复 | 跟踪 roarr/global-agent/@electron/get/electron-builder 的兼容更新 |
| `x/crypto/openpgp` | 模块级告警，无修复；当前源码未导入该包，crypto 的认证用途仍必需 | 不引入该包；不能称整个模块已无告警 |

这些边界不能写成依赖审计全部通过。实际扫描时间、输出、工具版本和发布验收由所属工作区[服务器档案](../../docs/server/SERVER_PROFILE.md)记录。

## 复验

每个 Go module 分别执行，后端单测保持 60 秒超时：

```sh
GOWORK=off go mod tidy -diff
GOWORK=off go mod why -m all
GOWORK=off go mod verify
GOWORK=off go build -mod=readonly ./...
GOWORK=off govulncheck -show verbose ./...
```

根模块全量测试使用 `node scripts/test-go.mjs -p 4 -count=1`，按既有分批规则覆盖完整测试清单。RelayKit 与 TokenKit 各自在模块目录运行 `go test -timeout 60s ./...` 和 `go vet ./...`。压缩库更新还需验证 `TestZstdRequestDecompressionContract` 的正常解码、无效输入、解压后限额及 body 释放。

Vue 和 Electron 各自在目录内执行 `bun install --frozen-lockfile` 与 `bun audit --json`。锁变化后按实际引用和依赖链解释结果，不能仅依赖包名扫描。Vue 继续执行完整单测、类型/lint/格式、受影响浏览器检查及生产构建。Electron 打包使用同一候选的 Vue/Go 产物；Linux 目录打包不能证明 Windows/macOS 安装包、图形启动或系统托盘兼容。

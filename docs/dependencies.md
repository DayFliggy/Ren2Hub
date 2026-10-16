# 依赖维护合同

主模块、RelayKit、TokenKit 分别维护 Go module；Vue 与 Electron 分别维护 `package.json` 和 `bun.lock`。Go 工作区不能替代独立模块验证。Web 前端只使用 Vue，React、Redux、AntD 和 Semi 不属于当前构建。

判断依赖是否可以删除时，同时核对源码、测试、平台专用文件、配置和构建脚本。直接依赖没有业务代码 import，并不意味着可删除：编译器、测试工具、数据库测试库、桌面下载工具和传递依赖都可能有实际用途。不得为消除漏洞告警删除仍被调用的包，或用忽略规则制造审计通过。

## 当前版本边界

构建固定使用 Go 1.26.9 和 Bun 1.4.0。主模块声明 `go 1.26.0`，RelayKit 保留 `go 1.25.1`，TokenKit 保留 `go 1.26`；不由一次依赖修补统一独立模块的最低语言版本。

主模块使用 `compress 1.18.7`，RelayKit 独立选择 `x/text 0.41.0`，主模块仍选择其原有 `x/text 0.42.0`。压缩库由请求 Zstd 解压、ClickHouse 日志传输和 profiling 使用；文本库通过 RelayKit 的 `samber/lo` 实际引入，不能移除。Go 安全补丁和来源见[同步账本](upstream/SYNC_LEDGER.md#依赖与构建)。

Vue 的直接依赖用于页面、状态、路由、国际化、图表、HTTP、Markdown 和 YAML；其开发依赖用于编译、CSS、类型、格式与测试。Electron 41.10.6、electron-builder 26.15.3 与 cross-env 分别用于桌面运行时、打包和开发模式的跨平台环境变量设置。主模块使用 `moejs v0.1.0-alpha.7`；`go.sum` 中可能保留旧版本校验和，实际构建版本以 `go.mod` 和根模块解析图为准。

## Vue 与 Electron 工具链修复

Vue 使用 Tailwind CSS 4.3.3 与同版本 `@tailwindcss/vite`，采用[官方 Vite 集成](https://tailwindcss.com/docs/installation/using-vite)，参考 AxonHub 的 Vite 插件接入方式。NewAPI 的 React/Rsbuild/shadcn 构建与当前 Vue 架构不同，其 Tailwind 4 配置仍有其他包引入 braces；Sub2API 的 Tailwind 3 也没有解决这条链，不能复制参考项目的全部依赖。

删除不再使用的直接 `autoprefixer`、`postcss`、旧 `tailwind.config.js` 和 `postcss.config.js`。PostCSS 仍由 Vue/Vite 所需传递依赖正常安装，不删除其必要用途。移除已可由正常解析满足的 `brace-expansion`、`postcss`、`source-map-js` 强制覆盖；锁文件保留实际安全版本。

Tailwind 3 的 micromatch/fast-glob/chokidar 路径均不再存在，`braces 3.0.3` 高危与 `postcss-nested` 所需 `postcss-selector-parser 6.1.4` 中危随旧链退出。ESLint/Vue 所需 parser 7.1.6 保留。主题 RGB 通道与 v4 主题变量分开存放；保留原有字体、暗色、动画、圆角、阴影、Preflight 和兄弟间距语义。表单控件只继承旧版规定的字体字段，避免新版 `font: inherit` 意外继承暗色账本的数字变体。命名字号保持固定行高及断点重置规则，站点容器在 utilities 前展开，既有 transform 覆盖边界同步处理独立旋转、缩放、平移属性；独立变换取消使用生产压缩后仍保留的初始值；旧版无效而新版开始生效的样式类按原实际外观清理，具体配置见[前端说明](../frontend/README.md)。

2026-10-13 依赖影响复审后，响应断点在同一 `@theme` 恢复 640/768/1024/1280/1536px，与既有像素 CSS/JavaScript 合同一致，保留用户默认字号设置。导航字体校正依据实际横向滚动，普通点击与键盘选页不再误锁定；切页后重新允许校正，真实手动横移仍受保护。这次修复不改变依赖清单、锁文件、Go 模块、后端或数据库格式。

Tailwind 4 将支持下限明确为 Safari 16.4、Chrome 111、Firefox 128；低于这些版本的浏览器不在当前支持范围，见[官方升级指南](https://tailwindcss.com/docs/upgrade-guide)。Chromium 自动检查不能替代 Safari/Firefox 或移动真机验收。

Electron 保留两代 `@electron/get` 与平台可选包，定向使用官方 `global-agent 4.1.3`。其 [v4.1.0 发布](https://github.com/gajus/global-agent/releases/tag/v4.1.0)移除了 roarr，从而移除 roarr → sprintf-js 旧链；这是经过下载代理合同验证的正式主版本更新，不能由漏洞忽略项、伪包或全局省略 optional 包替代。HTTP、HTTPS CONNECT、NO_PROXY、三平台文件名和校验和失败路径都需验证。删除没有脚本或 CI 调用、引用未声明 canvas 的图标占位脚本，保留实际分发的 PNG 图标。固定 Bun 1.4.0 的 Bun Shell 在父环境 NODE_ENV=production 时未通过 Electron 开发入口合同，因此保留实际有用的 cross-env，不依据文档猜测可删。global-agent 4 不再消费 `ROARR_LOG`，get 3/Builder 需要显式 logger，get 5 使用 Undici；不能把下载器的 DEBUG 当作原日志替代。代理覆盖的 API、只输出固定摘要的诊断命令与复验边界见[桌面说明](../electron/README.md#download-diagnostics)。

Vue 和 Electron 的 Bun 审计分别记录；JS 工具链三项旧告警关闭后的证据在[同步账本](upstream/SYNC_LEDGER.md#依赖与构建)及所属工作区[服务器档案](../../docs/server/SERVER_PROFILE.md)。

## 保留的 Go 模块告警

`golang.org/x/crypto v0.57.0` 的 [GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932) 指向没有修复的 OpenPGP 包。当前源码没有导入该包，也没有可达漏洞符号；同一模块的 bcrypt/HKDF 仍用于认证和证据密钥派生，不能删除整个模块。主模块仍须报告该模块级告警，不称全部语言依赖告警清零。Go 源码与模块图本轮未变，复用上一批固定 Go 1.26.9 的独立模块扫描；引入 OpenPGP 前重新审计。

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

## 依赖许可与分发

`THIRD-PARTY-LICENSES.md` 按三个 Go module 的直接依赖、Vue 与 Electron 的 manifest 和冻结锁文件维护；Go 版本采用根模块实际解析图。当前清单包含 103 项直接依赖，区分运行、生产、开发与测试用途，不把 Electron 的 `devDependencies` 声明误解为不分发运行时，也不复制上游 React 清单。许可证以模块缓存或冻结安装包中的真实文件为依据，保留包版权、完整文本和附带子组件许可；完全相同的文本只收录一次。`NOTICE` 收录这些直接依赖提供的 8 份 NOTICE 原文。此边界不宣称穷尽传递依赖、系统镜像或浏览器组件。

MySQL 驱动 `github.com/go-sql-driver/mysql v1.7.0` 的 MPL-2.0 全文、准确版本源码地址和修改后分发义务写入同一清单及 NOTICE。升级或替换依赖时，同时更新实际版本、许可正文、版权声明和对应源码获取方式，不能只更新 SPDX 名称。Go module 的实际文件与 npm 包内标识不一致时，以包内许可文件逐项核对，不用未知许可的静默默认值。

Docker 最终镜像在 `/licenses/` 携带 `LICENSE`、`NOTICE`、`THIRD-PARTY-LICENSES.md`；Electron 的 macOS、Windows、Linux `extraResources` 将三份文件放入应用资源的 `licenses/`，并保留 `licenses/electron/LICENSE` 与 `licenses/electron/LICENSES.chromium.html`。本次沿用现有路径，不改变打包依赖；实际发布构建仍须核对产物内容。单独分发二进制或前端产物时也应随附这些文件，不能把容器或桌面配置的检查当作其他分发形式已经携带。

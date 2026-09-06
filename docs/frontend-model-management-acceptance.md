# 模型管理与系统实例验收矩阵

功能基线为工作区本地 NewAPI `2d8e50bf36e94200b809dfb39e73624ec48b1e23`，参考 `web/src/features/models/` 和 `web/src/features/system-info/`。供应商与预填充组均为 NewAPI 原有管理功能。本项目用 Vue 实现对应业务流程，并采用 Desert Ledger / One Night 主题及中英文文案。

## 功能与证据

| 范围 | 已实现行为 | 验证依据 |
| --- | --- | --- |
| 模型列表 | 图标、名称复制、四种匹配规则与数量、状态、供应商、描述、标签、端点、渠道、分组、计费类型、同步状态、时间 | `ModelMetadataCell.vue`、页面视觉测试 |
| 筛选与分页 | 名称、供应商、状态、同步筛选；URL 同步；列显隐；移动列表 | 路由及 `model-management.spec.ts` |
| 批量操作 | 当前页选择、启用、禁用、删除、复制；逐项汇总并保留失败选择 | `modelMetadataForm.spec.ts` |
| 模型编辑 | 右侧抽屉、供应商选择、图标预览、标签列表、端点表格/JSON/模板 | 抽屉单元测试、视觉测试、真实 Go API 保存 |
| 端点兼容 | 数组、JSON 字符串、路径简写、缺省 POST、小写方法；未编辑推导数组原样保留；非法字段/重复项拒绝 | API contract、端点编辑器与表单测试 |
| 本地端点 | Chat、Responses、Compact、Alpha Search、Video、Claude、Gemini、图像、Embedding、Rerank | 模板与 Go 实际 Relay 路由对应；表单回归测试 |
| 计费 | Root 专属；按次/按量、USD/百万 Token、缓存/图像/音频倍率 | `modelPricing.spec.ts`、`ModelPricingEditor.spec.ts`、真实 Vue 保存并回读 Option |
| 计费写入边界 | 保存前重读、合并目标模型、单次 bulk；改名冲突、并发变化、清空、模式切换、零值、未加载配置保留 | 价格合并测试 |
| 部分成功 | 元数据成功后价格失败明确报错；重试不重复创建模型 | `ModelMetadataDrawer.spec.ts` |
| 缺失模型 | 搜索、分页、名称预填；取消不创建 | 缺失模型及视觉测试 |
| 上游同步 | 语言、冲突搜索/分页、逐字段选择、结果统计；配置来源禁用；取消不写入 | 同步单元/视觉测试、Go locale 测试 |
| 供应商 | 模型页管理菜单内搜索、分页、完整 CRUD、图标；保存刷新模型供应商 | 视觉测试、独立 SQLite CRUD |
| 预填充组 | 分类、搜索、编辑、删除；模型/标签列表与端点 JSON；旧数据兼容 | API contract、视觉测试、独立 SQLite CRUD |
| 渠道预填组 | 仅加载 model 分类，追加去重保序，只修改草稿 | `ChannelModelPrefill.spec.ts` |
| 管理深链接 | 旧供应商/预填地址转 metadata 管理界面，保留 query/hash | 路由测试、真实浏览器 |
| 系统实例 | Root 专属；身份与命名来源、主/工作节点、平台/架构、启动/心跳、资源与磁盘；缺失值为空 | 严格 contract、视觉测试、真实本地实例 |
| 任务 | 活动/历史独立加载和刷新，最近 20 条；状态、进度、执行节点、时间、错误；任务名称本地化 | 系统单元/视觉测试 |
| 轮询 | 实例 30 秒；活动任务 8 秒；隐藏/卸载取消，恢复可见重读 | `useSystemManagement.spec.ts` |
| 实例清理 | 单项/批量确认、结果与错误；已恢复在线实例不能删除 | `model/system_instance_test.go`、真实在线实例删除拒绝 |
| 日志清理 | 任务操作菜单、截止时间、确认后执行原删除接口 | 页面与 API 测试 |
| 权限 | Admin 不挂载价格编辑器，不请求 Root Option；普通用户拒绝管理；缺失 capability 不加载业务 | 抽屉/路由测试、真实 Root/Admin/User API 与页面 |
| iONet 退出 | 控制器、客户端、页面、导航、连接测试和设置字段删除 | 源码引用审查、Go 构建 |
| 退休路由 | 部署及旧前缀路径 HTTP 404；部署 API 为 JSON 404；不进入 SPA | Go Web 路由测试、真实 HTTP |
| 自动定价 | 六个 auto_pricing 字段保留，旧混合设置地址重定向 | catalog、路由测试 |

## 本地验收

- 全量前端：123 个测试文件、571 项通过；随后新增的两项端点兼容测试及受影响 28 项定向测试通过。
- 最终 TypeScript、ESLint、Prettier、Vite 生产构建通过。ECharts 共享 chunk 仍有既存的大于 500 kB 提示。
- Go `go test -timeout 60s ./...`、主程序构建及真实 `TestProductionEmbeddedAssetGraph` 通过；保留 `all:frontend/embed-dist`。
- 两个页面共 18 项 Playwright 通过，覆盖中英文、桌面/移动、明暗主题、抽屉/弹窗焦点、非法 JSON、加载/错误/空态、无横向溢出与无模块 404。
- 独立临时 SQLite 和真实 Go 二进制验收返回 `LOCAL_MANAGEMENT_RUNTIME_OK`：实际 Vue 保存元数据与价格、三种角色权限、供应商/预填 CRUD、旧入口、在线实例拒删、部署页面/API 404。测试凭据随机生成，仅用于本地临时数据库。

## 发布与边界

Docker 冷构建、镜像身份、数据库备份、应用容器替换和公网渲染证据统一记录于外层工作区 `docs/server/SERVER_PROFILE.md`，不以 HTTP 200 或 healthy 单独认定前端可用。

本轮没有数据库结构或持久化格式变更。iONet 外部资源、第三方密钥及历史 Option 值未处理；旧值不再被部署功能消费。未使用生产账号或付费凭据执行管理写入或 Relay 请求，上游同步写入也未对生产目录执行。配置文件同步、Playground、Chat、Chat2Link、Chat Presets 及既定 disabled capability 边界保持不开放。

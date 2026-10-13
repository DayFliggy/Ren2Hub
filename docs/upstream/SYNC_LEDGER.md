# 上游同步账本

本账本按领域记录 Ren2Hub 当前代码对 NewAPI、Sub2API、AxonHub 改动的处置：已吸收的行为、合并上游时必须保留的本地偏离、等待上游或延期的问题，以及刻意不吸收的项目和复评条件。当前同步基线为 NewAPI `63cecab824bad1a421d19950004c309bb2054c09`（main）、Sub2API `4d9ac3e482106d2da3a39fe3e546d8d6801f1cc1`（main）、AxonHub `3cb7e3335cadd463fe7748c47dd003802125e3dd`（unstable），下一轮从这三个提交向后比较。

逐批的比较窗口、执行过程和验收数据保存在 Git 历史中：在 Ren2Hub 目录运行 `git log -- docs/upstream/SYNC_LEDGER.md` 定位批次，再用 `git show <commit>:docs/upstream/SYNC_LEDGER.md` 读取。接口字段以[网关 API](../routing/GATEWAY_API.md)、[协议支持矩阵](../routing/GATEWAY_SUPPORT_MATRIX.md)、[插件 API](../plugin-api/v1.md)、[模型定价](../billing/PRICING.md)和[鉴权合同](../authentication.md)为准，本账本只记处置与本地合同。

| 状态 | 含义 | 复评要求 |
| --- | --- | --- |
| 已吸收 | 本地具有上游行为，包括按本地资金、价格或转换策略合同改写后的形态 | 上游再改同一行为时按新增量比较 |
| 本地偏离保留 | 与 NewAPI 基线不同，有缺陷证据或结构约束；合并上游时须保留 | 写明上游修复或改变后回归的条件 |
| 等待上游 | 本地存在已知问题，等待对应来源修复；已登记的用户决定继续保留 | 保留风险及等待条件，不提前替换用户已决定等待的实现 |
| 待验证 | 尚缺实际供应商或环境证据 | 补齐指定证据后重新判断，不把静态核对当作运行验证 |
| 延期 | 有价值但未实施 | 写明依赖与进入条件 |
| 刻意不吸收 | 不符合当前产品或架构边界 | 写明现有替代与复评触发条件 |
| 不适用 | 本地没有对应代码或产品 | 对应能力重新引入时复评 |

状态沿用[适配矩阵](../../../skills/ren2hub-upstream-sync/references/adaptation-matrix.md)的定义：部分吸收的上游提交拆成多行分别标注；百炼实际 `reasoning_effort` 行为仍是待验证项（队列 `SYNC1008-06`）。处置状态描述代码行为，守护测试列指明相关回归，不代表本轮候选验收或部署已完成；“—”表示没有专门测试。

## 2026-10-13 增量处置

本轮官方 HEAD 与发布元数据采集完成于 `2026-10-13T09:33:55.4938040Z`。完整比较窗口为 NewAPI `767a82b8764778b847462ebecdc231eacd1e282c..63cecab824bad1a421d19950004c309bb2054c09`（16 提交：15 个有文件变化、1 个空提交）、Sub2API `9591136d45cef26453348ed9532a7ebbce7cf99c..4d9ac3e482106d2da3a39fe3e546d8d6801f1cc1`（47 提交：35 普通、12 merge）、AxonHub `7a0cfbf42b00829dd8811cc87488f6194ed5ae02..3cb7e3335cadd463fe7748c47dd003802125e3dd`（1 普通提交）。计数按完整提交图，空提交与无净变化合并也登记，不用路径过滤后的列表冒充完整窗口。

| 来源 | 官方最新发布 | 标签完整 SHA | 发布时间（UTC） |
| --- | --- | --- | --- |
| NewAPI | [v1.0.0-rc.44](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.44) | `633b4c5b190dc1af99a2814391354d86948e7943` | 2026-10-12 13:42:01 |
| Sub2API | [v0.2.16](https://github.com/Wei-Shaw/sub2api/releases/tag/v0.2.16) | `01842576b3d5c1e2545b5a99ad555d36e28ed306` | 2026-10-13 07:20:24 |
| AxonHub | [v1.0.0-beta11](https://github.com/looplj/axonhub/releases/tag/v1.0.0-beta11) | `b8bb3421e1fe946cfc4f0d86e8412098738f2793` | 2026-10-08 05:43:47 |

标签与主干分别锁定；三份官方 release 元数据均为 `draft=false`、`prerelease=false`，不据此或 rc/beta 名称推定生产稳定性。“已吸收（既有）”表示现有合同覆盖，不能统计成本轮新功能。同一提交的已吸收与未采用部分拆行登记。此前等待上游（用户决定）的项目未在本窗口修复，全部保留。

本节登记实现与复评边界；候选验收与实际部署结果见[服务器档案](../../../docs/server/SERVER_PROFILE.md)。随后按用户要求修复 Vue 重构工具链，移除 Tailwind 3 与 Electron 旧代理链，关闭三项 JS 告警；未导入的 x/crypto/openpgp 模块级告警仍保留，见依赖与构建。独立副本、回环供应商及本地测试不能替代真实供应商认证、物理输入法或桌面平台兼容性证据。

### NewAPI

| 提交 | 分类 / 优先级 | 状态 | 当前行为与复评边界 |
| --- | --- | --- | --- |
| `63cecab824bad1a421d19950004c309bb2054c09` | 渠道 / P2 | 已吸收 | type 59 余额查询使用 `/v1/usage`，保留明确的 `remaining:0`、传输设置、管理权限及旧余额失败边界；不是本地订阅或钱包产品。详见下方渠道余额合同。 |
| `b4be2a7847c4db4a956c3e71ae4bc1f31bc71b60` | 界面 / P3 | 不适用 | React GroupRatio 编辑器新增分组置顶。本地已退役分组计费编辑器；充值组倍率 JSON 不是该组件，不为此补丁恢复旧产品。 |
| `633b4c5b190dc1af99a2814391354d86948e7943` | 日志 / P2 | 已吸收（Vue 改写） | 用量日志专用 NDJSON 导出、普通用户开关和默认 365 天共享范围；普通用户列表、统计、图表与两种专用导出使用同一范围规则，超限明确拒绝；管理员全局列表不受普通用户查询范围限制。保留用户 DTO 隔离、精确金额、PAT 权限和每用户单进程互斥；前端校验成功终态及计数，取消中止下载。见[日志合同](../console/LOGS.md)。 |
| `dd59854050f26f4f67bb3dac5274af5e17013462` | 依赖 / P1 | 已吸收 | Electron 的 node-gyp 分支 undici 6.29.0；Electron 41 所需 @electron/get 5 分支使用 7.30.0，不强制跨分支统一主版本。 |
| `93cf4ba6fb92e3f755fdeb6af9a9484bfa748816` | 记录 / P3 | 不适用 | 完整提交窗口中的空提交，无文件变化；保留计数，不记为新增能力。 |
| `3c9fb2a0de168d88ce5bffbf44acecdc1cdcdd39` | 依赖 / P1 | 已吸收 | brace-expansion 安全补丁按 Electron 实际主版本分支更新到 1.1.21、2.1.7、5.0.12；Vue 同包正常解析为 5.0.12，已移除冗余 override，仍按其自身锁文件验证。 |
| `9f048107edbba2156691718126de7c4577614533` | 依赖 / P3 | 不适用 | React web 的 ai 7.0.136；Vue 控制台没有该依赖。引入同类 SDK 时复评。 |
| `3fe394cdba280c00bc566de493b5fc8399b0a7d1` | 依赖 / P1 | 已吸收（既有） | Go 已使用 gorilla/websocket v1.5.3，无需重复改动。 |
| `1c58f3d161fa32755b372d9449f2d5e077d50a5d` | 依赖 / P1 | 已吸收 | fast-uri 安全补丁；本地在相同兼容范围解析到 3.1.8。 |
| `7f51a97226c43bc082a065bdc8b40b719c4bdcce` | 依赖 / P1 | 已吸收 | Electron 传递依赖 js-yaml 4.3.2。 |
| `7e25797d14681e4e16c2b46831450e7d6a5ccc74` | 桌面 / P1 | 已吸收 | Electron 39.8.10 升至 41.10.6，保留 electron-builder 26.15.3 和现有打包结构。锁文件升级不证明桌面打包、系统托盘与各平台启动兼容。 |
| `bf4e1ebfb88579930d75d852d2d2bca74cbd5f62` | 依赖 / P3 | 不适用 | 上游 oxfmt；本地 Vue 使用现有 ESLint/Prettier。 |
| `82306b3c35945bc0ed2c567688afe28d8a281300` | 依赖 / P3 | 不适用 | 上游 @tanstack/router-plugin；本地使用 Vue Router。 |
| `c730add954e1b565656a9320444cac5fee12cfe4` | 依赖 / P3 | 不适用 | 上游 stream-markdown-parser；本地没有该依赖。 |
| `b8c2dc12660dceb2a905008a672f8bc0260c98c8` | 依赖 / P3 | 不适用 | 上游 yace；本地没有该依赖。 |
| `ba29f06453942d9b5c0b32330340b8a000692c4f` | 依赖 / P1 | 已吸收 | Electron 传递依赖 @xmldom/xmldom 0.8.15。 |

### Sub2API

普通提交按能力逐项处置；合并提交只核对净差异与冲突修正，不额外计一份功能。

| 提交 | 分类 / 优先级 | 状态 | 当前行为与复评边界 |
| --- | --- | --- | --- |
| `983cf297da59e3be23b5e3f2c4eded945b53f408` | 协议 / P2 | 刻意不吸收 | Responses 结构化输入被 400 拒绝后退回 Chat；本地在发送前统一选择目标协议，不新增由 HTTP 错误触发的隐式协议重试。 |
| `5a25507dcbb33fc40e3ef0659af210f206229a31` | 账号池 / P2 | 刻意不吸收 | 临时不可调度规则 scope；本地已有渠道/模型/密钥健康作用域，不新增账号池规则引擎。 |
| `a46249aac5611fca71fb0d955ad19be7602aca71` | 账号池 / P2 | 刻意不吸收 | 上条 scope 测试随账号池能力复评，不移植不对应本地合同的测试。 |
| `c15d858eca81b269c41ba3bc161f923b179a860c` | 协议 / P2 | 刻意不吸收 | Responses→Chat fallback 保留模型与耗时；无运行时 fallback 路径。未来协议探测需绑定 attempt、预算、重新报价与完整错误体。 |
| `f7f1112cdab09e9bee982d77ae253d5d20a56e73` | 依赖 / P1 | 已吸收（既有） | 本地 Vue 3.5.43、source-map-js 1.2.2 已覆盖上游补丁。 |
| `63800c50d462efc6436e96b9dd46ae8f98a054bc` | 协议 / P2 | 已吸收（既有） | 取消、客户端提交或副作用发生后不重放的安全边界已在统一尝试生命周期实现；不同时引入其隐式协议 fallback。 |
| `daa79157491f25939b502f5c040eb11768a46e30` | 依赖 / P1 | 不适用 | SheetJS 安全升级；本地无 xlsx 依赖，当前 Excel 导出不使用 SheetJS。 |
| `d56f5e787976c8b2a7bd471b9a3c30b0ccee50c1` | 协议 / P2 | 已吸收 | 固定模型目录增加 claude-haiku-5-5；adaptive、medium 默认与 effort 能力由既有共享 renderer 覆盖。 |
| `d56f5e787976c8b2a7bd471b9a3c30b0ccee50c1` | 定价/身份 / P2 | 刻意不吸收 | 不预置 Haiku 阶梯售价或模拟 OAuth 身份；管理员保存 model_pricing 后价格才生效。 |
| `9a51333101a849bb948abf94c5ed53682e373edd` | 界面 / P3 | 延期 | 分组显示名/描述多语言需要独立展示元数据，不能修改作为路由、权限、价格标识的组名；见 SYNC1013-03。 |
| `ce6242fca1ef84da4855a92cd80117028b3a99c0` | 账号池 / P3 | 不适用 | 国际 Coding endpoint 的订阅用量 probe allowlist；本地没有账号池用量探测。 |
| `3bfd40f5afd820ff7fae32f3f26c75fe3e0dd504` | 健康 / P2 | 已吸收 | Google cachedContent 不存在的准确 403 消息归 input，不重试、换渠道或记健康失败；普通 403 保持 permission。 |
| `d5a802f84a6ae3405bee2a6e0160675785abf237` | 缓存产品 / P2 | 延期 | 显式缓存 CRUD、TTL 存储计费新增持久状态和资金路径；先定义 ownership、冻结报价、预留/结算/释放及三库恢复。 |
| `3abd64658297f5ec47ad09120a0a26d6081d42de` | 健康 / P2 | 已吸收（既有） | 本地转换/解析失败已由 AttemptOriginLocalConvert/LocalParse 归 input，不计供应商健康故障。 |
| `64331a97921bd5a43e7b54ba6a517476c985e8e5` | 界面 / P3 | 不适用 | 账号 UI 中 Vertex 改名 Agent Platform；本地无该账号 UI，渠道仍按 Google Vertex AI 合同命名。 |
| `81e6ec5346c645fa03d794495198e32e4b910598` | 身份 / P2 | 已吸收（既有） | Vertex adaptor 已支持 API Key；Claude/Gemini 与开源模型域的认证支持范围保持现有合同。 |
| `2588675ce7c0fa13189379489cc4edabe48f6fcd` | 配额 / P2 | 不适用 | 本地没有 AI Studio 模拟日配额或第三方账号等级，执行显式 Provider RPM/TPM。 |
| `1c68f9996df8e0bd7933d94d4ca826bc3dc570c7` | 脱敏 / P1 | 已吸收 | 共享 MaskSensitiveInfo 在域名规则前屏蔽裸 AQ.Ab（40+）与 AIza（35+）凭据；认证路径原本无旧 Key 前缀限制。 |
| `6f9b02f12c3d414ed324460a0e82d7793a471f72` | 目录 / P3 | 延期 | Vertex 服务账号原生模型列表；先复用现有认证、代理/TLS、分页、错误脱敏与 manifest 可见性合同。 |
| `c9e4b267b6fac1f00060eab983e606f7a2b2d4fc` | 界面 / P3 | 已吸收（既有） | Vue ChannelModelSyncDialog detect/apply 已显示 API 层投影的后端错误原因；保持取消和旧响应守卫，失败不关闭弹窗。 |
| `05d7946af84228589e5a6a8d8b6bc08d686ba778` | 缓存产品 / P2 | 延期 | 缓存错误透传范围及延长沿用创建时报价，随显式缓存产品统一设计，不能绕过当前错误与冻结报价合同。 |
| `f0ee427f0ad2f7eab6ab8f095ba05041af3a48f8` | 定价 / P2 | 刻意不吸收 | 显式缓存内置官方价/模型族兜底价不成为本地有效售价；缓存产品本身仍延期。 |
| `4426593e9d7dc38a009f28da038ac2906a57c17e` | 定价 / P3 | 刻意不吸收 | Gemini 3.6 Flash 内置优惠兜底价；仅管理员选取外部草案并保存后生效，不新增自动价格 fallback。 |
| `daebb7a80c720763a01e5cef8799c030aed0b058` | 目录 / P3 | 延期 | Vertex 模型列表/详情随原生发现能力复评；供应商全目录不能直接向普通用户开放。 |
| `26b31a83e4190a3b38bce112fc597defbfb9515b` | 缓存产品 / P2 | 延期 | 缓存修改串行化、回滚与 Key/订阅预留不能复制为第二套资金机制；复用唯一资金事件与幂等恢复。 |
| `fc82e831026a15eaf609cc1f7cd4d4a3690e5cad` | 账号池 / P2 | 刻意不吸收 | 模型级 401 自定义规则；保留本地 401 凭据分类，不新增订阅账号池规则。 |
| `8b74741efe35a42c67bbb35b821af9a12aca2c24` | 界面 / P3 | 延期 | 分组译文语言规范化与 Vary 保留 Origin，随独立展示元数据/cache 合同复评。 |
| `ba2a71733272b75e948c28a6fb41ffa506a40edc` | 协议 / P2 | 已吸收（既有） | Claude 工具图片转 Gemini 已保留调用 ID、原顺序、inline/url 媒体及能力/损失策略，不增加 base64 专用转换支线。 |
| `492c63059980ea5797cc2c34ba642ffee9a56aee` | 协议 / P2 | 已吸收（既有） | 上条测试 lint 修正无新增能力；本地工具媒体守护覆盖相同合同。 |
| `2023c290e1bbfa63e3f05bdddeeae1f264b8e637` | 账号池 / P2 | 刻意不吸收 | 计数、Bedrock 与重试耗尽入口把模型交给账号规则；本地 attempt 已持有实际映射模型，不接入账号池错误处理器。 |
| `43a3c197cb7663582ad1b6b21c761b84776193c9` | 缓存产品 / P2 | 延期 | 区分账号已删除/暂不支持缓存/必已过期的恢复意图，需先定义缓存 ownership 与三库恢复状态机。 |
| `cfbbfd6b3b41bbdeee748e25d8aa3de866b811c7` | 账号池 / P2 | 不适用 | 无 Antigravity 账号池或专属图像限流产品。 |
| `26f70c6a873bbeed126f1944068a7eb183c89f16` | 协议 / P2 | 已吸收（既有） | Haiku 5.5 Responses→Claude 的 adaptive、effort、强制工具已由唯一 renderer 覆盖。 |
| `26f70c6a873bbeed126f1944068a7eb183c89f16` | 协议 / P2 | 刻意不吸收 | 不把 Claude 签名封装成 Responses 私有 encrypted_content；保留跨协议 provider_state_dropped 与 ownership 合同。 |
| `c4bdc484753acc95f9a775854af303146389286c` | 协议 / P2 | 本地偏离保留 | 跨协议采样移除记 warning，strict 拒绝；无 modifier 的原生 Claude 请求继续透传，不照搬订阅网关的原生参数校验。 |
| `b669dc6daf1a9909b02ecf1f9cb692d88875e086` | 构建 / P3 | 不适用 | 参考项目 Makefile 的模型白名单前端入口不对应本地 Bun/Vite 构建体系。 |
| `4d9ac3e482106d2da3a39fe3e546d8d6801f1cc1` | 版本 / P3 | 不适用 | 只记录 Sub2API VERSION 0.2.16，不覆盖 Ren2Hub 项目版本。 |

| 合并提交 | 状态 | 合并内容与边界 |
| --- | --- | --- |
| `07d51a6aeff41824730aa8276afef150e3d03a03` | 已核对（合并记录） | 安全依赖合并，处置见 Vue/SheetJS 行；行为处置见上表，不新增独立适配。 |
| `b68684271670b4b301abb9ca61a8fd41750c9448` | 已核对（合并记录） | 路由合并，未放宽 Responses fallback 安全边界；行为处置见上表，不新增独立适配。 |
| `ab80abc6d1f929a6864c8804fd077b509d21fda2` | 已核对（合并记录） | 分组多语言 PR #7960；行为处置见上表，不新增独立适配。 |
| `7c5afa4c361b7b69afeb9aaf5b47c0bbe9af0180` | 已核对（合并记录） | Haiku 5.5 PR #7957；行为处置见上表，不新增独立适配。 |
| `53c21fed252cec522035cfc2d4dd890a8c47140b` | 已核对（合并记录） | 显式缓存 PR #8006；行为处置见上表，不新增独立适配。 |
| `9d5e63a598c827dde2d8b864ac7719125615bd82` | 已核对（合并记录） | main 合并与 PR #7828 测试冲突修正；行为处置见上表，不新增独立适配。 |
| `bba85923b96c277b9cbe4a2d21c661ff741c7e29` | 已核对（合并记录） | Antigravity 图像限流 PR #8046；行为处置见上表，不新增独立适配。 |
| `8d7d3185e987f0241f4cd99c2bc2b220378b4860` | 已核对（合并记录） | 工具图片 PR #8038；行为处置见上表，不新增独立适配。 |
| `49b2122dc623f7f2face4a2f89dbd6bceffb781a` | 已核对（合并记录） | 国际 Coding 用量 PR #7991；行为处置见上表，不新增独立适配。 |
| `01842576b3d5c1e2545b5a99ad555d36e28ed306` | 已核对（合并记录） | Responses fallback PR #7810，同时为发布标签提交；行为处置见上表，不新增独立适配。 |
| `49dc6e236380a7198e0658054b0d7c595b597387` | 已核对（合并记录） | scope PR #7828，无净文件变化；行为处置见上表，不新增独立适配。 |
| `df3c38e10f80deb226f61c9fd3cc0c818b185bb4` | 已核对（合并记录） | 分支对齐，无净文件变化；行为处置见上表，不新增独立适配。 |

### AxonHub

| 提交 | 分类 / 优先级 | 状态 | 当前行为与复评边界 |
| --- | --- | --- | --- |
| `3cb7e3335cadd463fe7748c47dd003802125e3dd` | 原生图像 / P2 | 已吸收（既有） | 标准 OpenAI 图像按入口原生请求 generations/edits；校验后原始字节复制保留 generation_id、safety_identifier、usage 与显式 reasoning_tokens=0。JSON 编辑 images/mask 的 URL/file_id 按原生合同透传，不强制 multipart。字段完整透传来自字节复制代码核对，不宣称已获真实供应商响应验证。 |
| `3cb7e3335cadd463fe7748c47dd003802125e3dd` | 订阅目录 / P2 | 不适用 | Codex 订阅原生图像及自动 gpt-image-2 目录不对应已退役的 type 57；授权渠道 manifest 仍是模型目录来源。重新批准订阅能力后复评。 |
| `3cb7e3335cadd463fe7748c47dd003802125e3dd` | 原生图像 / P2 | 刻意不吸收 | 不复制上游禁止图像 SSE 的限制；本地保留图像 JSON/SSE 及 JSON 转 SSE 合同。 |

## 协议转换

| 来源 | 状态 | 当前行为与本地合同 | 本地位置 | 守护测试 | 复评条件 |
| --- | --- | --- | --- | --- | --- |
| NewAPI `e0483122c` | 已吸收 | Responses 转 Claude、Gemini 时省略最后一个用户消息或工具结果之后、不含工具调用的 assistant 轮次，记 warning `trailing_assistant_omitted`（safe/allow 放行，strict 拒绝）；以待回结果的工具调用结尾或只有 assistant 轮次的输入原样发送 | `relaykit/relayconvert/internal/oai_responses/req_helpers.go` | `TestResponsesTrailingAssistantTextIsOmittedForClaudeAndGemini` | 上游调整省略条件时 |
| 本地 AUDIT1012-01（Responses 并行调用间的 reasoning） | 本地偏离保留 | 工具调用之间夹有 reasoning 项时，`keepToolOutputsAdjacent` 把整段调用视为同一批，结果仍接在最后一个调用之后；转 Chat 时跳过 reasoning 项，不再生成空 user 消息切断批次。Chat、Claude、Gemini 均只得到一个含两次调用的 assistant 轮。转 Gemini 的工具声明只由 `toolconv.AttachRequest` 按 JSON Schema 能力投影，删除两个转换器中运行时不可达的重复投影 | `relaykit/relayconvert/internal/oai_responses/req_helpers.go`、`to_oai_chat_req.go` | `TestResponsesReasoningBetweenCallsKeepsOneBatch`、`TestGeminiJSONSchemaUpstreamReceivesResponsesToolSchemaUnchanged` | 上游调整工具结果归位时按其实现比较，保留同批语义 |
| NewAPI `0a03088e0`、`78bd5b1c`、`036ba376d`（Claude 推理能力） | 已吸收 | Sonnet 5.5 关闭思考发 `between_tools`，Opus 5.5、Fable、Mythos 发 adaptive 加 `low`，分别记 warning `claude_thinking_disable_as_between_tools`、`claude_thinking_disable_unsupported`；其余模型发 `disabled`。Opus 5.5、Haiku 5.5 默认 effort 为 medium | `relaykit/relayconvert/reasoning/claude.go` | `TestDisabledReasoningReachesClaudeAsAcceptedThinking` | Anthropic 或上游改变模型能力表时 |
| NewAPI `0a03088e0`（强制 `tool_choice`） | 本地偏离保留 | 非 Claude 源的强制 `tool_choice`（`required` 或具名工具）在 Fable 5.1、Mythos 5.1、Opus 5.5、Sonnet 5.5 上以及渲染为手动思考时降为 `auto`，记 warning `forced_tool_choice_downgraded`（strict 拒绝）。上游只在手动思考时降级并记 error；本地默认策略为 safe，error 会让默认请求失败 | `relaykit/relayconvert/internal/toolconv/encode.go` | `TestConvertedForcedToolChoiceIsDowngradedWhereClaudeRejectsIt`、`TestStrictPolicyRejectsForcedToolChoiceDowngrade` | 上游按模型判断或改用 warning 时回归上游写法 |
| NewAPI `0a03088e0`（无工具时的 `tool_choice`） | 已吸收 | 跨协议转换后目标请求没有函数工具时，`tool_choice` 与 `parallel_tool_calls` 不发送，记 warning `tool_choice_without_tools`（safe/allow 放行，strict 拒绝）；Claude 与 Responses 目标的托管工具计入工具列表。与上游一致，合同见[支持矩阵](../routing/GATEWAY_SUPPORT_MATRIX.md) | `relaykit/relayconvert/` | `TestToolChoiceIsOmittedWhenNoToolReachesTheTarget` | — |
| NewAPI `acb347a19`（重复托管搜索转 Chat） | 已吸收 | Chat 只承载一个托管搜索，转 Chat 时多出的托管搜索定义记 error `duplicate_hosted_web_search`：safe、strict 发送前拒绝（400），allow 保留第一个并丢弃其余。与上游一致，合同见[支持矩阵](../routing/GATEWAY_SUPPORT_MATRIX.md) | `relaykit/relayconvert/internal/toolconv/encode.go` | `TestDuplicateHostedWebSearchToChatIsAPolicyDecision` | — |
| Sub2API `normalizeEffortModelID` | 本地偏离保留 | `canonicalClaudeModelID` 去掉 `models/`、命名空间、云区域前缀、`anthropic.` 与日期后缀，并把点号版本名（如 `claude-sonnet-5.5`）映射为连字符名；思考渲染与强制工具降级共用。Bedrock `-v1:0` 等版本后缀保留，前缀匹配不受影响。NewAPI 能力表只认 `claude-` 前缀；不采用 Sub2API 对强制工具返回 400 的做法 | `relaykit/relayconvert/reasoning/claude_model_id.go` | `relaykit/relayconvert/claude_rejected_controls_test.go` | NewAPI 能力表支持带前缀模型名时 |
| NewAPI `0a03088e0`（Gemini schema） | 已吸收 | Gemini 函数参数的 OpenAPI 子集转 Chat、Claude、Responses 时由共享工具编码改写为 JSON Schema，无等价写法的关键字丢弃并记 warning `gemini_schema_keyword_dropped`（strict 拒绝）；转 Gemini 保持原样 | `relaykit/relayconvert/internal/toolconv/` | `TestGeminiOpenAPISchemaBecomesJSONSchemaForNonGeminiTargets`、`TestStrictToolPolicyRejectsTheDroppedGeminiSchemaKeywords` | `strict:true` 函数工具对联合类型的接受度未在线核实，供应商报错时复评 |
| NewAPI `0a03088e0`、`78bd5b1c`（Responses 工具媒体转 Gemini） | 已吸收 | 移植上游 `responsesToolOutputToGemini`：图片与文档按调用 ID 和原顺序转 `inlineData`，Gemini 3 及未知版本放入 `functionResponse.parts`，1.x/2.x 放在同轮函数结果之后。无法承载的媒体省略并记 error `unsupported_media_type`，上游两处静默省略路径也改为记该诊断；safe/strict 发送前返回 400，allow 丢弃媒体后发送并扣费。Claude 工具图片可转 Chat、Responses、Gemini，文档仅可直转 Responses；Chat/Gemini 来源工具媒体、未知媒体块和没有转换路径的其他媒体在所有策略下拒绝 | `relaykit/relayconvert/internal/oai_responses/to_gemini_chat_req.go`、`relaykit/relayconvert/media_plan.go` | `TestGatewayResponsesToolMediaOnGemini` | 上游改变媒体放置或省略诊断时 |
| 本地响应守卫 | 本地偏离保留 | 跨协议响应中的 Claude `signature`、Gemini `thoughtSignature` 去掉后交付，每个响应或每条流记一次 warning `provider_state_dropped`（safe/allow 放行，strict 拒绝）；strict 下非 Claude 源转 Claude 且渲染为手动扩展思考的请求在发送前以同一代码拒绝；其他组合（如 Claude 自适应思考、Gemini 思考模型的函数调用）只在响应实际带签名时拒绝，此时上游已生成并计费，非流式请求按无有效输出释放预留、不向用户扣费（AUDIT1009B-16 选方案 B 时接受的残余成本）；托管搜索与引用的 `encrypted_content`、`encrypted_index` 在所有策略下拒绝跨协议转换；同协议原生响应保留签名。NewAPI 没有响应守卫 | `relaykit/relayconvert/response_guard.go`、`relaykit/relayconvert/request_registry.go` | `TestCrossProtocolResponseDropsProviderState`、`TestCrossProtocolStreamDropsProviderStateOnce`、`TestStrictRejectsCrossProtocolManualThinkingBeforeSend` | 上游加入同类守卫时比较 |
| NewAPI Gemini 思考签名绕过 | 已吸收 | `gemini.function_call_thought_signature_enabled` 默认开启，控制台可关闭；其他协议转 Gemini 时为历史中每个模型轮次填入绕过值。网关出站状态复核用 `IsThoughtSignatureBypass` 跳过绕过值，真实签名仍须绑定 | `relaykit/relayconvert/internal/shared/gemini/request.go`、`setting/model_setting/gemini.go`、`service/gateway_state.go` | `TestRelayInfoConvOptionsCarriesGeminiThoughtSignatureSetting` | Google 文档称绕过值是最后手段；Google 改变签名要求时复评 |
| AxonHub `d6e00c00`（NewAPI `requestOpenAI2Ali` 保持原文） | 本地偏离保留 | `requestOpenAI2Ali` 与上游逐字节一致；`renderAliDisabledThinking` 在宿主解析的 effort 为 none 时发 `enable_thinking:false` 并去掉 `reasoning_effort`，保留客户端显式值，关闭意图与显式 `enable_thinking:true` 冲突时返回客户端错误 | `relay/channel/ali/text.go` | `TestRequestOpenAI2AliDisabledThinking` | 百炼对非 none `reasoning_effort` 的实际处理未在线核实，接入阿里渠道后用真实调用核实；NewAPI 加入同类映射时比较 |
| NewAPI `c6bac617` | 已吸收 | 思考后缀适配器开关同时控制 effort 后缀；开关关闭时 `-high`、`-low` 等原样用于路由、映射、计费和上游请求。Claude 开关默认开启，Gemini 默认关闭 | `setting/reasoning/suffix.go`、`relay/helper/` | `TestParseKnownProviderModelSuffix`、`TestApplyReasoningModelSuffix*`、`TestBaseModelNameFollowsSuffixAdapterToggles` | Gemini 开关关闭时 `gemini-*-high` 请求找不到基础模型渠道，需要时打开开关 |
| NewAPI `335ebcff`、`1a4166d8`（custom/freeform 工具） | 已吸收 | custom 工具在 Chat、Claude、Gemini 间以单一字符串 `input` 包装并还原，历史调用与结果保持关联；带 grammar 的输入格式记 `custom_tool_grammar_not_portable`（safe/strict 拒绝，allow 记录损失） | `relaykit/relayconvert/internal/toolconv/encode.go` | `TestCustomToolStreamHoldsNameAndArgumentsUntilCallID`、`TestCustomToolMalformedArgumentsNeverBecomeSuccess` | — |
| NewAPI `d0cb7347`、`789c9701` | 已吸收 | 共享 OpenAI 能力表覆盖 Sol、Luna 及快照名；Claude 原生保留 `safeguards`，跨协议记 `claude_safeguards_not_portable` | `relaykit/dto/openai_request.go`、`relaykit/relayconvert/request_semantics.go` | `TestClaudeSafeguardsRemainNativeAndCannotSilentlyDisappear` | — |
| NewAPI `c2b7a9a9e` | 已吸收 | Claude 消息级 `output_config` 原生保留；本地按当前 user 回合解析 effort 计价，跨协议出现消息级配置记 `claude_message_output_config_not_portable`（safe/strict 拒绝）。合同见[网关 API](../routing/GATEWAY_API.md#claude-消息级-effort-与计价) | `relaykit/dto/`、`relay/common/override.go`、`relay/helper/billing_expr_request.go` | `TestGatewayClaudeEffortProtocolAndIncomingParamBilling`、`TestGatewayClaudeMessageOutputConfigConversionBeforeBilling` | — |
| Sub2API `97bdde31` | 已吸收 | Claude 普通工具 `content_block_start.input` 作为后备参数；有 `input_json_delta` 时使用累计值，不拼接两份 JSON | Claude 流式转换 | `TestClaudeStreamFirstFramePreservesParallelTools` | — |
| Sub2API apicompat（拒答转 Chat） | 本地偏离保留 | Responses 拒答在非流式、流式、缓冲三条路径转为 Chat `refusal` 字段，增量与终态快照只交付一次 | `relaykit/relayconvert/internal/oai_responses/` | `TestResponsesRefusalReachesChatRefusalField` | NewAPI 提供拒答映射时比较 |
| 本地（拒答转 Claude） | 本地偏离保留 | 拒答转为 Claude text 块并设 `stop_reason: "refusal"`，优先于 tool_use、end_turn 与长度截断；记 warning `responses_refusal_reduced`（safe/allow 放行，strict 拒绝）；只交付一次。AxonHub 静默丢弃，三个上游都没有该映射 | `relaykit/relayconvert/` | `TestResponsesRefusalReachesClaudeUnderConversionPolicy` | 上游提供映射时比较 |
| 本地 AUDIT1009-02a（拒答转 Gemini） | 等待上游 | Responses 拒答转 Gemini（请求历史与响应两个方向）被丢弃，三个项目都没有映射 | — | — | NewAPI 修复后按其实现同步 |
| 本地 AUDIT1009B-09（Haiku 5.5 能力行） | 已吸收 | 按 NewAPI `036ba376d` 补齐：Haiku 5.5 只用 adaptive、默认开启思考、五档 effort、默认 effort `medium`，转换请求不再发 `budget_tokens`（记 `claude_budget_to_adaptive`），并去掉 `temperature`、`top_p`、`top_k`；`disabled` 时 effort 最高 `high`。点号名与云前缀名经 `canonicalClaudeModelID` 命中同一行 | `relaykit/relayconvert/reasoning/claude.go`、`relaykit/relayconvert/reasoning/suffix.go` | `TestClaudeHaiku55RendersAdaptiveThinkingWithoutSampling` | Anthropic 或上游改变 Haiku 5.5 能力时 |
| 本地 AUDIT1009B-10（Bedrock Sonnet 5 强制工具） | 等待上游 | Bedrock 上 Sonnet 5 配强制 `tool_choice` 时 Anthropic 要求显式 `thinking:disabled`，转换不发送 thinking，模型按默认开启思考运行；与 NewAPI 一致 | `relaykit/relayconvert/reasoning/claude.go` | — | NewAPI 修复后按其实现同步 |
| 本地 AUDIT1009B-11、NewAPI `13c8e90b8` | 已吸收 | Responses `input_image` 的字符串 `image_url` 转 Chat 时包成 `{"url": ...}`；对象形态与 `file_id` 不变 | `relaykit/relayconvert/internal/oai_responses/to_oai_chat_req.go` | `TestResponsesInputImageReachesChatAsImageURLObject`、`testdata/golden/request/openai_responses_to_openai.golden.json` | 上游再改图片部件形态时 |
| NewAPI `13c8e90b8`（`detail`） | 本地偏离保留 | 客户端显式给出的 `detail` 随 URL 一起放进 Chat `image_url` 对象。上游一律不带 `detail`，理由是用旧版 OpenAI SDK 校验的 Chat 上游拒绝 `original` 等新值；本地媒体规划合同承诺 Chat 与 Responses 之间保留 `detail` 且不记损失 | `relaykit/relayconvert/internal/oai_responses/to_oai_chat_req.go`、`relaykit/relayconvert/media_plan.go` | `TestMediaChatResponsesImageDetailIsPreserved`、`TestResponsesInputImageReachesChatAsImageURLObject` | Chat 上游因 `detail` 值返回 400 时复评 |
| 本地 AUDIT1009B-12（Fable 5 effort 诊断） | 已吸收 | 按 NewAPI `036ba376d`：Fable 5/5.1、Mythos 5/5.1、Mythos Preview 标记为接受 effort，关闭思考意图渲染为 adaptive 加 `low`，`claude_thinking_disable_unsupported` 的“最低档”与实际一致 | `relaykit/relayconvert/reasoning/claude.go` | `TestClaudeDisableIntentRendersAcceptedThinkingMode`、`TestClaudeEffortIsSentOnlyAtALevelTheModelAccepts` | Anthropic 或上游改变能力表时 |
| NewAPI `036ba376d`（effort 与 Gemini 2.5 预算） | 已吸收 | `reasoning.ClaudeEffort` 给出模型接受的 effort：不支持的档位降到最近一档并记 `claude_effort_adjusted`，不接受 effort 的已知模型去掉 effort 并记 `claude_effort_unsupported`，均为 warning（strict 拒绝）；后缀或修饰符关闭思考时，客户端 `output_config.effort` 按同一规则保留或去掉，`output_config` 其他成员不变；Opus 5、Haiku 5.5 的 `disabled` 与 Sonnet 5.5 的 `between_tools` 最高 `high`；去掉 `claude_disabled_effort_ignored`。未知模型与不带后缀、修饰符的原生请求不改写。Gemini 2.5 Pro 的 `xhigh`、`max` 用 32768 预算，`high` 仍为 24576 | `relaykit/relayconvert/reasoning/`、`relaykit/relayconvert/internal/shared/claude/reasoning.go` | `TestClaudeEffortIsSentOnlyAtALevelTheModelAccepts`、`TestThinkingOffSuffixKeepsClientEffortWithinModelLimits`、`TestGemini25EffortBudgetsFollowTheModelMaximum` | Anthropic 或上游改变 effort 能力时 |
| NewAPI `5ac756284` | 已吸收 | Chat 转 Claude 流中同一块同时带推理与正文时，先发 thinking 增量并关闭 thinking 块，再开 text 块发正文；用量、停止原因与结束事件不变 | `relaykit/relayconvert/internal/oai_chat/to_claude_messages_resp.go` | `TestStreamResponseOpenAI2ClaudeKeepsTextSharingAChunkWithReasoning` | — |
| NewAPI `4b13d9388`、`c10780e05`（转 Gemini 的工具与结构化输出 Schema） | 已吸收 | typed Schema 能表达的工具参数与结构化输出照旧清洗为 `parameters`、`responseSchema`，非字符串 `enum` 去掉；只有 `additionalProperties`、`$schema` 时不换字段。不能表达的 Schema 在宿主判定接受 JSON Schema 的上游（Vertex AI，或该模型 Gemini API 版本不是 `v1`）原样放入 `parametersJsonSchema`、`responseJsonSchema`；判定规则只在 `model_setting.GeminiAcceptsJSONSchema`，`RelayInfo.ConvOptions` 与渠道规划共用。`v1` 上照旧清洗，转换器记 warning `gemini_schema_keyword_dropped`（safe/allow 放行，strict 拒绝）。本地适配：发送前规划把只在 typed Schema 上发生的损失单独记录，由 `PlanConversionFor` 按上游能力决定是否计入，因此 JSON Schema 上游在 safe 下不再因 `const`、`oneOf` 等被拒；`v1` 上规划阶段仍对清洗实际丢掉的约束记 `schema_field_not_portable` 等 error（safe/strict 拒绝） | `relaykit/relayconvert/internal/shared/gemini/schema.go`、`relaykit/relayconvert/internal/toolconv/encode.go`、`relaykit/relayconvert/conversion_plan.go`、`relaykit/relayconvert/request_semantics.go`、`relay/common/relay_info.go`、`service/routing_preview.go`、`setting/model_setting/gemini.go` | `TestGeminiJSONSchemaUpstreamReceivesSchemasUnchanged`、`TestGeminiTypedSchemaUpstreamCleansSchemasUnderLossPolicy`、`TestGeminiJSONSchemaUpstreamReceivesResponsesOutputSchemaUnchanged`、`TestGeminiSchemaWithOnlyAdditionalPropertiesStaysTyped`、`TestRelayInfoConvOptionsGeminiJSONSchemaSupport`、`TestGeminiChannelPlanFollowsTheJSONSchemaCapability` | Google 改变 v1 字段支持或上游调整判定时 |
| AxonHub `5659a526` | 已吸收 | Responses 输入中工具输出与调用之间夹有消息时，`InputItems` 把每个输出前移到请求它的那组连续调用之后（组内保持客户端顺序，`custom_tool_call` 与 `function_call` 同组），其他调用的输出留在原位；Chat、Claude、Gemini 三个转换都经 `InputItems` 读取输入 | `relaykit/relayconvert/internal/oai_responses/req_helpers.go`、`to_oai_chat_req.go` | `TestResponsesToolOutputsFollowTheirCallsForClaude`、`TestResponsesToolOutputsFollowTheirCallsForGemini`、`TestResponsesToolOutputStaysAdjacentToItsCallInChat` | — |
| Sub2API `412718edf` | 本地偏离保留 | `ClaudeMediaMessage` 对 thinking 块始终输出 `thinking` 与 `signature` 键，未知签名为空串，其他块不变；转换生成的 thinking 块不伪造签名 | `relaykit/dto/claude.go` | `TestClaudeThinkingBlockAlwaysCarriesSignatureKey`、`TestGeneratedClaudeThinkingBlocksCarryEmptySignature` | NewAPI 同样输出该键时回归 |
| Sub2API apicompat（旧版 `function_call`） | 本地偏离保留 | `dto.Message` 带 `function_call`；旧版调用与紧随的同名结果配对为工具调用与结果，Claude、Responses 用稳定的 `call_legacy_N`，Gemini 按名匹配；无法配对记 error（safe/strict 拒绝，allow 省略未应答调用）；缺 `name` 或 JSON 无效在请求分析阶段返回 400，原生 Chat 转发也不例外 | `relaykit/dto/`、`relaykit/relayconvert/` | `TestLegacyFunctionCallPairBecomesToolExchange`、`TestUnpairedLegacyFunctionHistoryIsDiagnosed` | NewAPI 支持旧版 `function_call` 时比较 |
| 本地（无名工具） | 本地偏离保留 | Chat 转 Responses 的无名工具调用在 JSON 与 SSE 返回协议错误，不伪装 completed，不重放已接受请求；NewAPI 跳过无名调用 | `relaykit/relayconvert/internal/oai_chat/` | `TestNamelessChatToolCannotBecomeCompletedResponse`、`TestGatewayNamelessToolFailsWithoutReplayingAcceptedWork` | — |
| AxonHub `81e519e9` | 已吸收 | Chat 转 Claude 流中 finish 块只有自带真实计数时才结束消息，否则等待仅含用量的块或正常结束 | `relaykit/relayconvert/internal/oai_chat/to_claude_messages_resp.go` | `relaykit/relayconvert/internal/oai_chat/to_claude_messages_resp_test.go` | — |
| AxonHub `ac7f4889` | 已吸收 | Responses 流中断错误事件带 `param: null` | 宿主流式错误输出 | `TestGatewayOllamaInterruptedStreamHasOneTerminalError` | — |
| AxonHub `2f8de312` | 本地偏离保留 | 终态之后的传输读错误仍判为失败 | 宿主流式错误输出 | `TestGatewayOllamaInterruptedStreamHasOneTerminalError` | 诊断出现“终态后读错误”样本时复评 Claude `message_stop` 规则 |
| NewAPI `0a03088e0`（转换流失败追加 JSON 错误体） | 刻意不吸收 | 转换流失败后按已交付输出结算，只发一个 `stream_interrupted` 事件 | `relaykit/relayconvert/stream_outcome.go`、`controller/relay_attempt.go` | — | 客户端需要 JSON 错误体时复评 |
| NewAPI `0a03088e0`（Codex namespace 平铺） | 延期 | Responses `function_call.namespace` 原生保留；跨协议 safe/strict 拒绝，allow 接受已登记降级并给诊断，不提供可逆映射 | `relaykit/relayconvert/` | — | 有客户端需要跨协议 namespace 且能定义可逆映射时 |
| NewAPI `bc8235792` | 刻意不吸收 | 上游把 relaykit 转换测试移出仓库；本地保留这些测试，它们保护 Gemini 工具媒体、Opus 5.5 默认 effort、后缀别名等合同 | `relaykit/relayconvert/*_test.go` | — | 上游以其他形式恢复同等覆盖时 |
| NewAPI `1d4328e97` | 刻意不吸收 | 上游重写英文 relaykit README；本地 `relaykit/README.md` 是按本地合同维护的中文版 | `relaykit/README.md` | — | 上游 README 出现本地未记录的接口时对照补充 |
| AxonHub `81029467`、`02269f12`、`0ff5b6f8` | 已吸收 | 以其严格工具、终态、流预读、拒答、音频、logprobs、推理案例补共享 DTO 字段和损失诊断；不引入第二个聚合管线 | `relaykit/` | — | — |
| AxonHub `e863c6fe` | 不适用 | 修复 Codex 私有 `additional_tools` 输入项转发；本地 Codex 订阅渠道已退役 | — | — | 重新引入 Codex 订阅渠道时 |
| Sub2API `d56f5e787976c8b2a7bd471b9a3c30b0ccee50c1`、`26f70c6a873bbeed126f1944068a7eb183c89f16`、`c4bdc484753acc95f9a775854af303146389286c`（Haiku 5.5） | 已吸收 | 固定目录提供 `claude-haiku-5-5`，共享 renderer 使用 adaptive、medium 默认、模型能力允许的 effort 与强制工具；disabled 的 effort 最高 high。原生无修饰符请求不额外改写；跨协议采样移除记 warning、strict 拒绝 | `relay/channel/claude/constants.go`、`relaykit/relayconvert/reasoning/` | `TestClaudeHaiku55RendersAdaptiveThinkingWithoutSampling`、`TestClaudeEffortIsSentOnlyAtALevelTheModelAccepts` | Anthropic 能力变化后更新唯一能力表；不复制内置售价或私有签名封装 |
| Sub2API `ba2a71733272b75e948c28a6fb41ffa506a40edc`、`492c63059980ea5797cc2c34ba642ffee9a56aee`（工具图片） | 已吸收（既有） | Claude 工具结果图片转 Gemini 经过现有有序媒体管线，保留调用 ID、inline/url 与能力/损失策略；不新增只接受 base64 的支线 | `relaykit/relayconvert/` | `TestClaudeToolImageReachesGeminiImagePart`、`TestToolMediaRejectsUnsupportedSourcesAndRoutes` | 上游扩展媒体来源或 Gemini 承载位置时比较 |
| Sub2API `983cf297da59e3be23b5e3f2c4eded945b53f408`、`c15d858eca81b269c41ba3bc161f923b179a860c`（Responses fallback） | 刻意不吸收 | 当前协议在发送前统一规划；不因上游 400 自动切换 Chat。重试继续受取消、客户端提交、有效输出和副作用边界约束 | `service/request_route.go`、`controller/relay_attempt.go` | `TestRelayAttempt*`、`TestRouteSendFence*`、`TestRouteRetryBudgetStop*` | 运行时协议探测独立立项后定义完整错误体、attempt、预算和重新报价 |
| AxonHub `3cb7e3335cadd463fe7748c47dd003802125e3dd`（原生 Images） | 已吸收（既有） | 原生 generations/edits 及 JSON 编辑原样透传；响应校验后复制原字节保留未知元数据和显式零，保留本地图像 SSE 能力 | `relay/channel/openai/adaptor.go`、`relay/channel/openai/relay_image.go`、`relaykit/dto/openai_image.go` | `TestOpenaiImage*`、`TestImageOutputAndCachedDetailsReachCanonicalUsage`、`TestConvertImageEditRequestMultipart` | OpenAI 图像请求/响应改变时复评；type 57 原生订阅图像不适用 |

## 计量与 token 估算

| 来源 | 状态 | 当前行为与本地合同 | 本地位置 | 守护测试 | 复评条件 |
| --- | --- | --- | --- | --- | --- |
| NewAPI `bb28f2cc` | 已吸收 | 独立模块 `tokenkit`（不被 relaykit 引用）负责文本与图片计数；工具定义、工具调用与结果、Gemini 系统指令计入估算；单类字符超过 1024 字节分段计数；出站 JSON 预算仍用于预扣和 Provider TPM。上游未返回用量时结算同样使用这一估算 | `tokenkit/`、`service/token_counter.go` | `tokenkit` 全量测试、`TestGetImageTokenBoundsOverflowingDimensions` | 上游重新校准权重或图片规则时 |
| 本地（tokenkit 符号段换行） | 本地偏离保留 | o200k 把符号段与其后的 `\r`、`\n`、`/` 合为一个预分词片段，交替的 `/\n` 不形成单类长串；本地把符号段后的换行并入同一串后照常分段。上游对 1 MiB 此类输入约需 6 分钟 | `tokenkit/tokenizer.go` | `TestCountCutsSymbolNewlinePieces` | 上游修复同类分段问题时回归 |
| 本地 AUDIT1009-01 | 等待上游 | 组合附加符号（`\p{M}`）被归为字母类，两个感叹号加一个组合重音符（U+0301）反复出现可绕过 1024 字节分段，gpt/o 系列预扣前计数随长度平方增长（128 KiB 约 5 秒），任何持有 Token 的用户都可在预扣前占用一个 CPU 核。NewAPI 同一代码同样存在，Sub2API、AxonHub 无对应限制 | `tokenkit/tokenizer.go` | — | NewAPI 修复后按其实现同步；候选修法是附加符号沿用当前分段类别，需用户决定是否在无参考依据下本地修复 |
| 本地（Claude `tool_result` 逐块解码） | 本地偏离保留 | 无法解码的块（如字符串来源的 `search_result`）按原文计入，纯文本 document 计入正文；上游一块解码失败时整段工具输出从估算与敏感词检查中消失 | `relaykit/dto/claude.go` | `TestClaudeToolResultCountsUndecodableAndTextDocumentBlocks` | 上游修复整段丢失时回归 |
| NewAPI `getImageToken` | 已吸收 | 函数与上游一致；非流式请求在 `GET_MEDIA_TOKEN_NOT_STREAM` 关闭（默认）时按典型尺寸估算图片。调用处保留本地接缝：URL 图片只在 `EstimateRequestToken` 唯一的取文件决定允许时下载，Gemini 原生格式或关闭媒体计数时直接按典型尺寸估算 | `service/token_counter.go` | `TestEstimateRequestTokenImageMeasurePolicy` | 上游改变取文件决定时 |
| NewAPI `usage_merge.go` | 已吸收 | 合并函数与上游一致；`output_tokens_details` 别名在计费读取器处理，`CompletionTokenDetails` 全零时才读 `OutputTokensDetails`。转换后的最终 Chat 用量块不带 `output_tokens_details`，带 `usage_source`、`billing_usage`，与上游 DTO 一致 | `relaykit/dto/usage_merge.go`、`service/billing_usage.go` | `TestOpenAIBillingUsageOutputDetailsSurviveSnapshotMerge`、`TestUsageMergeOutputDetailsLaterNonZeroWins` | — |
| NewAPI Gemini 原生流式用量 | 已吸收 | 每帧用 `MergeGeminiUsageMetadataNonZero` 累计，后续帧缺字段时保留前帧报告值 | `relay/channel/gemini/relay-gemini.go` | `TestGeminiStreamHandlerAccumulatesUsageMetadataAcrossFrames` | — |
| NewAPI `chat_via_responses.go` | 已吸收 | 经 Responses 转 Chat 的流失败且已有输出时，用估算补齐缺失的 completion 或 prompt | `relay/channel/openai/chat_via_responses.go` | `TestOaiResponsesToChatStreamFailureEstimatesMissingCompletion` | — |
| AxonHub `c9e866cb` | 已吸收 | 用量 `cost` 为数字或字符串时原样往返 | `relaykit/dto/` | `TestUsageCostKeepsProviderNumberOrString` | — |

## 工具计费

| 来源 | 状态 | 当前行为与本地合同 | 本地位置 | 守护测试 | 复评条件 |
| --- | --- | --- | --- | --- | --- |
| NewAPI `IsBuiltInToolPriceKey` | 本地偏离保留 | 保留名使用上游同名函数，但键来自 17 个键的静态列表，不随运营价格变化；另有 `web_fetch`、`code_execution`、`tool_search` 三个无内置价格的保留名。客户端同名函数或 `tool_use` 不按这些键计费 | `setting/operation_setting/tools.go`、`relay/common/tool_usage.go` | `TestCountBillableToolCallFunctionCallSkipsReservedNames` | 上游增删内置价格键时同步列表 |
| 本地（不预置工具价格） | 本地偏离保留 | `tool_price_setting.prices` 初始为空；声明工具缺适用价格时发送前拒绝，明确 0 有效。上游预置的 8 个人民币计价键会按美元扣费，约多收 7.3 倍 | `setting/operation_setting/tools.go`、`relay/helper/pricing_requirements.go` | `relay/helper/pricing_requirements_test.go` | 上游预置价格改为正确币种且产品决定预置时 |
| NewAPI `acb347a19`（工具计数） | 已吸收 | Claude `web_search` 与 Gemini `google_search` 计数记入本次尝试的 `BuiltInTools`，重试换渠道不继承上一次尝试的次数；结算时单个工具次数封顶 10,000 并记 warning | `relay/common/tool_usage.go`、`service/text_quota.go` | `relay/common/tool_usage_test.go` | — |
| NewAPI Claude、Gemini 工具计数 | 已吸收 | 计数函数与上游一致；`web_fetch`、`code_execution`、`tool_search`、`google_search_grounded_prompt` 只在冻结报价为该键定价时计入（`collectBillingQuantities`），未定价不计入也不让结算失败 | `service/text_quota.go` | `TestTextToolQuoteBillsQuotePricedOnlyKeysWhenPriced`、`TestClaudeServerToolUseKeys` | — |
| NewAPI `acb347a19`（Claude 服务端工具） | 已吸收 | `server_tool_use` 的 `web_fetch`、`code_execution`、`tool_search` 按各自键计费；Claude 线格式声明这三类工具时发送前要求定价（明确 0 有效）。`web_search_20260209`、`web_fetch_20260209` 内部使用的代码执行未定价时不计费 | `relay/helper/pricing_requirements.go`、`relay/channel/claude/` | `TestClaudeServerToolUseKeys`、`relay/helper/pricing_requirements_test.go` | 运营为 `code_execution` 定价后，动态过滤搜索的内部代码执行也会计费，与上游一致 |
| NewAPI 图片工具计数、`acb347a19`（图片终态） | 已吸收 | 带最终 `result` 且状态不是 failed、cancelled、incomplete、partial 的 `image_generation` 输出在任何终态计费，标为 `in_progress` 的同样计费；超过 128 张按 128 张结算；网页、文件、函数调用的 `in_progress` 不计 | `relay/common/tool_usage.go`、`service/responses_usage.go` | `TestImageGenerationCallCounterCommitCapsAtMaxImageN`、`TestResponsesStreamBillsDeliveredInProgressImage`、`TestResponsesTerminalImageBilling` | — |
| 本地（图片去重身份） | 本地偏离保留 | 图片输出按 ID、`call_id`、输出序号去重，三者都没有时才用结果 SHA-256 | `relay/common/tool_usage.go` | `TestImageGenerationCallCounterCompletedOutputs` | — |
| 本地（非流式失败终态） | 本地偏离保留 | 非流式 Responses 的 failed、cancelled 返回 502，不转发正文；没有有效输出时释放预留 | `relay/channel/openai/relay_responses.go` | `relay/channel/openai/relay_responses_billing_test.go` | — |
| NewAPI `service/responses_usage.go` | 已吸收 | 流式在 `output_item.done` 确认的网页、文件、函数调用在任何终态都计入；终态快照只在 completed 时补计未见过的调用 | `service/responses_usage.go` | `service/responses_usage_test.go` | — |
| NewAPI `acb347a19`（供应商搜索计数） | 已吸收 | Azure Responses 按 `tool_usage.web_search.num_requests` 计 `bing_web_search` 并把协议键置零；xAI Responses 按 `server_side_tool_usage_details` 计 `web_search`、`x_search_posts`、`x_search_profiles`；百炼 Responses 按 `usage.x_tools` 计 `search_strategy_agent`，请求路径为 `/compatible-mode/v1/responses` | `relay/channel/openai/`、`relay/channel/xai/`、`relay/channel/ali/` | 各渠道读取器测试 | 字段名取自上游原文，未用真实供应商响应核实 |
| 本地（`VendorToolPriceKeys`） | 本地偏离保留 | 各供应商在读取器旁声明“托管工具 → 额外价格键”，发送前一并要求定价；`InitChannelMeta` 清除上一渠道的读取器、价格键和它新建的计数，重试换渠道不带旧键 | `relay/common/relay_info.go`、`relay/helper/pricing_requirements.go` | `TestTextToolQuoteIgnoresZeroCountWithoutPrice` | 上游加入发送前定价检查时比较 |
| NewAPI `acb347a19`（百炼 Anthropic 端点） | 已吸收 | 渠道设置 `WebSearchBillingKey` 时按该键计 `search_strategy_agent` 并补 `cc_entrypoint` 系统标记；发送前只要求实际计费的 `search_strategy_agent`，不要求永不计费的 `tool:web_search` | `relay/channel/ali/web_search.go`、`relay/helper/pricing_requirements.go` | `TestWebSearchBillingKeyReplacesProtocolRequirement` | — |
| NewAPI `d10f11fff` | 已吸收 | Gemini grounding 按一次响应内去重后的非空网页与图片搜索查询数计 `google_search`，跨流式帧与全部候选，原生、Chat、Responses 共用计数；`google_search_grounded_prompt` 只在冻结报价定价时计入。不按模型代际预置计费键，按次计费由运营为 `google_search_grounded_prompt:gemini-2.5*` 定价并把同前缀 `google_search` 设为 0 | `relay/channel/gemini/` | `TestGeminiHandlersCountGroundingQueries` | 查询数不保证与 Google 账单一致；Google 在响应中报告计费次数时改用该值 |
| 本地 AUDIT1009-02b（发送前定价） | 等待上游 | Gemini `enterpriseWebSearch` 与 `/v1/alpha/search` 计费但发送前不要求定价，未定价时结算回落到冻结估算；NewAPI 依靠预置价格 | — | — | NewAPI 修复后按其实现同步 |
| 本地 AUDIT1009B-13（转换流失败的 prompt 估算） | 等待上游 | 转换流以 `response.failed` 结束且只报告 completion 时，handler 补齐的 prompt 估算没有写入 BillingUsage，结算按 input 0 计；补齐 completion 的一侧正常并标为估算。与 NewAPI `chat_via_responses.go` 同一段代码一致 | `relay/channel/openai/chat_via_responses.go` | — | NewAPI 修复后按其实现同步 |
| NewAPI `acb347a19`（其余部分） | 延期 | 智谱与百炼 Chat 的 `WebSearchEncoder` 渠道级搜索编码（含强制搜索推断计数）、Advanced Custom 转 Responses、Moonshot Responses 透传未吸收 | — | — | 属于新能力；编码诊断在本地默认 safe 策略下会拒绝请求，需单独设计后立项 |
| Sub2API `d5a802f84a6ae3405bee2a6e0160675785abf237`、`05d7946af84228589e5a6a8d8b6bc08d686ba778`、`26b31a83e4190a3b38bce112fc597defbfb9515b`、`43a3c197cb7663582ad1b6b21c761b84776193c9`（显式缓存产品） | 延期 | 保留当前 cachedContent 引用 ownership；不新增缓存 CRUD、TTL 存储收费、修改租约及恢复产品，也不复制订阅 Key 预留机制 | — | — | 先定义用户/Token/渠道 namespace、不可变创建/延长报价、预留/结算/释放、幂等与三库恢复；见 SYNC1013-01 |
| Sub2API `f0ee427f0ad2f7eab6ab8f095ba05041af3a48f8`、`4426593e9d7dc38a009f28da038ac2906a57c17e` | 刻意不吸收 | 不预置 Gemini 缓存价格或 3.6 Flash 模型族优惠 fallback；外部来源只生成管理员可选草案，保存才生效 | `model/model_pricing_config.go` | — | 产品明确需要预置售价且满足本地资金/币种合同后复评 |

## 插件与异步任务

| 来源 | 状态 | 当前行为与本地合同 | 本地位置 | 守护测试 | 复评条件 |
| --- | --- | --- | --- | --- | --- |
| NewAPI `b23471112` | 已吸收 | moejs `v0.1.0-alpha.6`；本地 `pluginValue` 先用宿主编解码转换非基础类型再交给 `FromGo`。沙箱禁用 `eval` 与 `Function`，内置对象深度冻结，调用深度 512，钩子 5 秒超时、单插件并发 8 | `go.mod`、`pkg/jsplugin/` | `TestPluginsCannotPatchSharedBuiltins`、`TestUnboundedRecursionIsARangeError`、`TestUsageHooksCannotBeDecodedIntoGoValues` | moejs 仍为 alpha，可选内存上限本地与上游都未启用；升级时复核沙箱测试 |
| NewAPI `6370b2942`（`json-order@1`） | 延期 | 未接入，当前无插件使用；冲突面在 `engine.go`、本地 `BindTaskPluginEndpointToChannel` 与中文 `v1.md` | — | — | 有插件需要时接入；用量钩子守卫须先于获取调用槽并覆盖 `CallJSONInto`，`BindTaskPluginEndpointToChannel` 每次解码都要重设请求原文 |
| NewAPI `7aa3531ef` | 已吸收 | 多部件提交重建正文后，宿主用重建得到的 `Content-Type`（含新边界）覆盖插件给出的值 | `relay/channel/task/jsplugin/adaptor.go` | `TestTaskAdaptorBuildsMultipartFromOpaqueFileReference` | — |
| NewAPI `EvaluateTaskCompletionUsage` | 已吸收 | 与上游相同，结算总以 attempt 预扣估算中经 `validateBillingAttemptEstimate` 回放验证的提交事实为底，再用完成事实逐键覆盖（含显式 0 与 false）；完成结果为空（无钩子、null 或 `{}`）时按提交事实求值，只比较类别事实的表达式按提交值取分支；验证失败明确报错并保留任务，不退回估算；带 `UsageError` 时不合并，按冻结估算结算；没有持久化估算的旧任务以空集为底 | `service/task_billing.go`、`model/billing_fallback.go` | `TestTaskCompletionMergesCompletionFactsOverFrozenSubmitFacts`、`TestEmptyTaskCompletionPricesFrozenSubmitFacts`、`TestTamperedSubmitFactsStopTaskSettlement`、`router/gateway_task_submit_facts_test.go` | — |
| NewAPI `2ffc59590` | 已吸收 | 插件 API 文档写明钩子参数只读、不在模块变量保留参数引用、宿主不修改已传入值 | `docs/plugin-api/v1.md` | — | — |
| NewAPI `0cce21db0` | 已吸收 | 用量键替换器只建一次；同一来源对象的规范化请求体在钩子间复用，用量钩子仍经 `submitUsageContext` 取得新规范化的请求值 | `relay/channel/task/jsplugin/` | `relay/channel/task/jsplugin/` 测试 | — |
| NewAPI `0a532f7f7`、`c8d234baa` | 已吸收（本地补丁） | 内置插件 xai `1.1.0+ren2hub.1`（上游 1.1.0 加十进制相减补丁：延长任务的输入秒数用 `utils.decimal` 精确计算，上游浮点相减如 16.37−6 会被用量边界拒绝）承载 `grok-imagine-video` 系列。编辑和延长不再使用"source"档，预扣 720p，完成后用 `utils.fetch` 读取输出 MP4 轨道头，短边不超过 480 像素按 480p 结算，否则或读取失败按 720p。审核拦截和报告费用的失败按 SUCCESS 结算，其他失败退款。生产价格若只给"source"档定价，编辑和延长需另配 720p/480p 档 | `plugins/tasks/xai/plugin.js` | `TestGatewayXAIVideoSettlesChargedModerationAndRefundsFailures`、`plugins/xai_responses_test.go` | 上游改为十进制计算后去掉本地补丁；未调用真实 xAI |
| NewAPI `29509776e` | 已吸收 | 驱动钩子（buildSubmitRequest、buildQueryRequest、parseTaskResult、buildBatchQueryRequest、parseBatchResult、buildContentRequest）可用 `utils.fetch` 只读访问渠道 Base URL 主机或插件 `allowedHosts`：仅 GET/HEAD，重定向原样返回，每次钩子调用最多 4 次、单次 15 秒、合计 30 秒、正文 1 MiB；等待期间让出执行槽并暂停看门狗。用量钩子与渲染器调用即抛错，用量守卫不变；走渠道代理与按渠道 TLS 设置。与上游一样不拦截私网或回环地址 | `pkg/jsplugin/fetch.go`、`relay/channel/task/jsplugin/adaptor.go` | `TestUsageHooksCannotFetch`、`TestTaskAdaptorHookFetchReachesRequestHostsOnly`、`TestTaskAdaptorDriverHooksFetchButUsageHooksCannot`、`TestTaskAdaptorHookFetchFollowsChannelTLSSetting` | 上游增加地址限制时同步 |
| NewAPI `b66674ac2`、`ba3b2c015` | 已吸收（本地补丁） | 声明 `duration-auto@1` 的插件可收到时长 `-1`（供应商决定时长），其他负值和超限值仍 400，用量事实保持非负，预扣按模型最长时长。doubao `1.3.0+ren2hub.2`（只有 Seedance 1.5 pro 与 2.x 接受 -1，1.0 pro/lite 本地拒绝；-1 一律按最长时长预扣，不按 `metadata.frames` 折算；Seedance 1.0/1.5 pro 预扣 12 秒、2.0 15 秒、2.5 30 秒；`/v1/videos` JSON 时长转给 Ark）、alibaba `1.4.2+ren2hub.1`（wan3.0 智能时长）；两者 multipart 时长下界本地改为 -1。`json-clone@1`、`submit-sse-delta@1` 不再必需：只导出 `parseSubmitEventDelta` 即启用增量钩子，两个能力字符串仍接受 | `pkg/jsplugin/registry.go`、`plugins/tasks/doubao`、`plugins/tasks/alibaba` | `TestAlibabaWan3`、`plugins/doubao_responses_test.go`、`relay/channel/task/jsplugin/adaptor_test.go` | — |
| NewAPI `66c3e1428` | 已吸收 | hailuo `1.2.1+ren2hub.2`：MiniMax v1 视频经 `/v1/files/retrieve`（`utils.fetch`）取签名 `download_url` 代理下载，不带渠道 Key，HEAD 客户端用 GET；修复 `/v1/files/download` 返回 410。完成轮询取一次链接存入 `plugin_state`，下载时 fileId 一致且签名 `Expires` 剩余超过 600 秒才复用，否则实时 retrieve，内容请求不再按 1:1 放大到 MiniMax。内容请求失败仍明确报错，不回退 ResultURL | `plugins/tasks/hailuo/plugin.js` | `TestHailuoArtifactContentProxy`、`TestHailuoArtifactContentReusesCompletionLink`、`TestTaskArtifactContentFollowsHailuoRetrieveLink` | — |
| 本地 `plugin_state` 完成视图 | 本地偏离保留 | `extractUsageOnComplete` 第二个参数带上本次解析返回的 `plugin_state`（xai 1.1.0 读取输出分辨率需要）；上游完成视图本来就含该字段。xai 的分辨率来自 `parseTaskResult` 经 `utils.fetch` 取回的 MP4 头，因此计费事实间接依赖上游字节；取值限于 480p/720p，读取失败按 720p，不超过预扣 | `relay/channel/task/jsplugin/usage_exact.go` | `plugins/xai_responses_test.go` | 与上游完成视图对齐后删除 |
| NewAPI `9df450fe5` | 已吸收 | 轮询按上游 `classifyPollHTTP` 分类：404/410 立即失败，其余故障累计 `PollFailures`，达到 `TASK_POLL_MAX_FAILURES`（默认 20）后失败；终态走本地 `failTaskWithoutResult` 释放预留，`fail_reason` 只含分类和状态码；宿主取消不计数 | `service/task_polling.go` | `TestPollMissingUpstreamTaskReleasesReservationOnce`、`TestPollTransientFailuresStopAtLimitAndRelease` | 供应商长故障仍可能先于上游收费完成而退款，与上游策略一致 |
| 本地（未完成任务查询，上游同样存在缺陷） | 本地偏离保留 | 未完成任务查询只按 status 判断；插件对运行中任务报告“100%”时保留状态推导的进度，不永久冻结预留 | `model/task.go`、`service/task_polling.go` | `TestUnfinishedTaskQueries_IgnoreReportedProgress`、`TestPollRecognizedRunningResultResetsFailures` | 上游修复时回归，可反馈上游 |
| 本地（立即结果交付） | 本地偏离保留 | 立即终态结果在任务与资金意图落库后返回客户端；资金投递失败由恢复任务补完，不返回 503 | `controller/relay_task.go`、`service/task_billing.go` | `TestGatewayDoubaoImmediateDeliveryFailureReturnsResultAndKeepsSettlement` | — |
| 本地 PB-1（立即任务原子完成） | 本地偏离保留 | 立即任务的创建、终态、完成用量和结算意图同事务提交；失败沿用冻结估算，均不重发或错误退款 | `model/task_submission.go`、`model/task_completion.go` | `TestImmediateTaskAtomicCompletionAcrossDatabases` | 调整资金事务时复验 |
| NewAPI 插件 ABI（运行中立即结果） | 已吸收 | 立即返回的运行中结果按异步任务落库并轮询；`retainResult:false` 与 OpenAI Images 结果清理只作用于终态 | `service/task_polling.go` | `TestRunningImmediateResultBecomesPollableTask` | — |
| NewAPI `1a4166d8`（`retainResult`） | 已吸收 | `retainResult` 只允许在原生 submit/dynamic 路由声明，协议对象中出现即加载失败（与上游一致）；false 只作用于立即终态；Images 立即结果一次交付，Responses JSON、SSE、background 保留结果 | `pkg/jsplugin/`、`relay/channel/task/jsplugin/` | `TestGatewayDoubao*` | — |
| NewAPI `ae73ef8e`、`87bb71e7` | 已吸收 | 豆包插件保留 Seedream Flash、逐模型 profile、`compatibilitySchema` 与 OpenAI Images generate/edit | `plugins/tasks/doubao/` | `TestGatewayDoubao*` | 供应商价格变化独立验收，不自动导入价格 |
| NewAPI `controller/channel.go`（`CopyChannel`） | 已吸收 | 复制 type 61 或带插件绑定的渠道要求 `task_plugin.bind` 权限 | `controller/channel.go` | `TestCopyChannelWithTaskPluginBindingRequiresBindPermission` | — |
| NewAPI `UpdateBatchTasks` | 已吸收 | `fetchMode: "batch"` 插件按固定插件版本和同一凭据合并为一次请求；缺项计一次失败；共享响应不写入其他任务 | `service/task_polling.go` | `TestBatchPlugin*` | 旧 `suno` 平台专用路径未合并，合并时复评 |
| NewAPI `dto/task.go`、`controller/task.go` | 已吸收 | 任务列表不返回 data，成功任务不返回 result_url；管理员可见 `admin_info`，Root 可见 `root_info`；结果经 `GET /api/task/:task_id/artifacts` 读取 | `controller/task.go` | `TestTasksToDto*`、`TestDashboardTaskArtifacts*` | — |
| NewAPI `common/init.go`、`model/option.go` | 已吸收 | 支持 `TASK_PLUGIN_ENABLED`；写入选项后总开关立即生效。禁用工厂键仍由同步路径合并数据库禁用项 | `common/init.go`、`model/option.go` | `TestTaskPluginEnabledOption*` | — |
| 本地（`UpdateWithStatus`，上游同样 `Select("*")`） | 本地偏离保留 | 只写轮询拥有的列，不覆盖 quota、user_id、channel_id；`private_data` 整列写入，由 status CAS 保护 | `model/task.go` | `TestUpdateWithStatus_Keeps*` | 上游修复时回归 |
| 本地（提交资金检查） | 本地偏离保留 | 每轮轮询的提交资金检查分块批量查询，规则与单任务一致 | `model/task_submission.go` | `TestTaskSubmissionsReady*` | — |
| NewAPI `controller/video_proxy.go` | 已吸收 | `/v1/videos/:id/content` 与产物接口共用所有权检查；旧视频任务免凭据读取结果 URL | `controller/video_proxy.go` | `TestVideoContentUsesOriginalTaskCredential`、`TestVideoContentServesLegacyResultURLToOwner` | — |
| NewAPI `controller/video_proxy.go`（回退 ResultURL） | 本地偏离保留 | 插件内容解析失败明确报错，不回退到 ResultURL，避免用无凭据请求访问需凭据的地址 | `controller/video_proxy.go` | `TestVideoContent*` | — |
| 本地（用量钩子数值边界） | 本地偏离保留 | 钩子返回安全整数，或最短往返表示不超过 15 位有效数字的非整数；`0.1+0.2`、超出 2^53、NaN、Inf 明确报错，不舍入。`utils.decimal`、`utils.json.parseNumbers` 作为本地扩展保留，已发布固定源码依赖它们 | `pkg/jsplugin/engine.go`、`pkg/jsplugin/utils.go` | `TestUsageHooksCannotBeDecodedIntoGoValues`、`TestUsageDecimalMultiplyPreservesExactProducts`、`TestReleasedFactorySourcesSettleExactProviderUnits` | 吸收上游用量改动时须保留边界 |
| NewAPI `pkg/jsplugin/utils.go`（`volcSignV4`） | 已吸收 | 按上游运行时读取 PascalCase 字段；本地 `v1.d.ts` 改为 PascalCase（上游 d.ts 写 camelCase，与其运行时不符） | `pkg/jsplugin/utils.go`、`docs/plugin-api/v1.d.ts` | `TestVolcSignV4IgnoresCamelCaseFields` | 上游修正 d.ts 时比较 |
| 本地（`metadata_preview.go`） | 本地偏离保留 | Root `POST /api/plugin/task/preview` 用 `moejs/syntax` 解析，只解析可证明安全的常量，不执行插件 | `pkg/jsplugin/metadata_preview.go` | `TestMetadataPreviewDoesNotExecuteSource`、`TestMetadataPreviewResolvesSafeConstants` | — |
| NewAPI `relay/helper/stream_scanner.go` | 已吸收 | `NewStreamScanner(maxBytes ...int)` 的调用方上限取与全局上限的较小值；插件 SSE 提交按 1 MiB+1 读取 | `relay/helper/stream_scanner.go` | `TestNewStreamScannerCallerLimit` | — |
| NewAPI `plugins/tasks/*` | 本地偏离保留 | 已发布版本的源码不改写，带本地补丁的内置插件使用 `<上游版本>+ren2hub.N`：alibaba `1.4.2+ren2hub.1`、doubao `1.3.0+ren2hub.2`、google 与 vertex-ai `1.0.2+ren2hub.2`、hailuo `1.2.1+ren2hub.2`、jimeng `1.1.0+ren2hub.1`、kling、sora、sunoapi `1.1.0+ren2hub.1`、vidu `1.0.2+ren2hub.1`、xai `1.1.0+ren2hub.1`。补丁让时长与帧数输入走 `utils.decimal.bounded`、计费乘法与时长相减走十进制守卫。各次发布的源码固定在 `plugins/testdata/pinned-*`（最新 `pinned-ce505c9b5`），在途任务按提交时的源码执行 | `plugins/tasks/`、`plugins/testdata/` | `TestReleasedFactorySourcesCompileAndKeepTheirIdentity` | 再吸收这些插件时用 `git merge-file` 以旧上游版本为基础重放本地补丁 |
| NewAPI `tryRealtimeFetch` 与提交即结算 | 刻意不吸收 | 提交只预留，终态按冻结报价、用量和费用结算；请求发出后不重放；凭据按 ID 绑定，不存明文 key；锁顺序 recovery → task → wallet → Token；执行源按 `TaskPluginRef{Key,Version,SourceHash}` 固定 | `service/task_billing.go`、`model/` | — | 资金模型改变时复评 |
| NewAPI 管理员任务日志插件列（`task-logs-columns.tsx`） | 延期 | 本地没有管理员全站任务日志页，插件列不可达 | — | — | 立项管理员全站任务日志时一并吸收插件列与筛选 |

## 路由与可用性

| 来源 | 状态 | 当前行为与本地合同 | 本地位置 | 守护测试 | 复评条件 |
| --- | --- | --- | --- | --- | --- |
| Sub2API `926441461`（钱包冷却部分） | 已吸收 | `classifyProviderRouteError` 对消息中任意位置的文本只在状态码已表明类别时采信（Key 文本配 401/403，模型文本配 404）；消息以失败短语开头时视为供应商自己的措辞，任何状态码下都计入（错误码或类型为 `invalid_request` 时除外）。`authentication_error` 码或类型标记 Key，Anthropic 404 `not_found_error` 的 `model:` 标记能力，401 先于 400 判为 Key 错误；结构化错误码在任何状态下照常分类。客户端不能用回显字段冷却共享渠道或 Key | `service/route_health.go` | `TestClassifyRouteErrorIgnoresEchoedTextOutsideMatchingStatus`、`TestClassifyRouteErrorRecognizesProviderKeyAndModelFailures` | NewAPI 没有对应分类器；上游引入时比较 |
| NewAPI `model/channel_cache.go`（权重语义） | 已吸收 | 健康档位是硬排序键，不缩放权重；同一比较组内首次尝试按权重比例分配，权重 0 的渠道在有正权重同伴时只作故障转移候选，全零组均分；决策 JSON 不含 `health_factor` | `service/route_score.go` | `TestOrderAutoRouteCandidatesFollowsNewAPIWeightSemantics` | — |
| 本地（自动路由冷启动） | 本地偏离保留 | 样本不足 50 的渠道按 good 比较，在同 priority 与负载桶内按权重获得首次尝试 | `service/route_score.go`、`service/route_health.go` | `TestOrderAutoRouteCandidatesGivesColdChannelItsWeightShare` | 故障的新渠道在熔断前会承接其权重份额的首次尝试 |
| Sub2API Retry-After 系列 | 本地偏离保留 | `ParseRetryAfter` 是唯一解析入口，秒数与 HTTP 日期都封顶为 `RouteRetryAfterCap`（5 分钟，等于本地最长健康冷却），健康行与 Provider 范围共用；过去日期不设冷却，负数与无法解析的值忽略 | `service/route_health.go` | `TestParseRetryAfter*`、`TestGatewayProviderRateCapsAbsurdRetryAfter` | 供应商确实要求更长等待时，每 5 分钟会多一次探测 429 |
| NewAPI `04c597bf6` | 已吸收 | 参数覆盖条件支持 `regex`（Go RE2，按条件懒编译，键不存在时不编译，非字符串或非法模式报错） | `relay/common/override.go` | `TestApplyParamOverrideConditionRegex` | — |
| NewAPI `8ca26d81d` | 已吸收 | 使用上游 `GlobalWebRateLimit(frontendFS)`、`ServeFrontendFiles` 和三个静态限流环境变量（默认关闭）；本地 `frontendFiles.Exists` 统一施加扩展名白名单并排除目录；嵌入的构建产物不计入 Web 限流，构建产物只压缩一次 | `middleware/rate-limit.go`、`middleware/frontend_static.go`、`router/web-router.go` | `TestVueStaticAssetsAreCompressedOnce`、`TestVueStaticAssetsDoNotConsumeWebRateLimit` | — |
| NewAPI `common/init.go`（`GLOBAL_WEB_RATE_LIMIT`） | 本地偏离保留 | 默认值为 600，上游为 120 | `common/init.go` | — | 控制台请求模式改变时复评 |
| NewAPI `feefe09f`（TLS） | 已吸收 | 中继、受保护 Fetch 与模型同步各自克隆 TLS 配置 | `common/init.go`、`service/` | `TestHTTPTransportsKeepIndependentTLSSettings`、`TestModelSyncTLSConfigurationIsIndependent` | — |
| AxonHub `2bf8e2c9` | 已吸收 | 内部 Token 0 回归请求以逻辑模型冻结价格，跨渠道重试分别映射上游模型，真实响应模型只用于诊断，未授权别名不发送 | `router/`、`controller/relay.go` | `TestGatewayPlaygroundModelIdentityWithoutPersistentToken` | 改变无持久 Token 的内部请求或路由时复验；Playground/Lab 产品入口已退役 |
| Sub2API Redis 失败放行资金预占 | 刻意不吸收 | 准入与资金预留不因 Redis 故障放行；未配置 Redis 时实际准入改用进程内预留 | `service/` | — | 只有保持现有预留与恢复不变量的证据充分时复评 |
| 本地 AUDIT1009B-14（Gemini 400 API key not valid） | 等待上游 | Gemini 以 400 `API key not valid` 报告失效 Key 时归为输入错误，不换渠道、不冷却 Key；NewAPI 同样不重试 400 | `service/route_health.go` | — | NewAPI 修复后按其实现同步 |
| NewAPI `c6741c36a` | 已吸收 | 渠道设置 `tls_insecure_skip_verify` 让该渠道的 API 请求跳过证书校验：中转与任务提交、Realtime/Responses WebSocket、余额与模型列表（Ollama 模型列表除外，见下行）、经渠道代理的媒体、Midjourney 提交与轮询（本地这两条走渠道代理，一并纳入）、插件轮询与 `utils.fetch`。跳过校验的渠道不与校验渠道共用客户端或连接池；Vertex 与插件服务账号的凭据交换始终校验。Vue 渠道表单的开关在"高级设置"，需要 `sensitive_write`。Codex 部分不适用（type 57 已退役） | `relaykit/dto/channel_settings.go`、`service/http_client.go`、`service/http_transport_policy.go` | `service/http_client_transport_test.go`、`TestChannelUpstreamQueriesHonorTLSInsecureSkipVerify`、`TestMidjourneyPollHonorsChannelTLSInsecureSkipVerify`、`TestTaskAdaptorHookFetchFollowsChannelTLSSetting` | — |
| 本地 AUDIT1011-01（Ollama 模型列表） | 等待上游 | Ollama 渠道获取模型列表使用 `&http.Client{}`，不走渠道代理和 `tls_insecure_skip_verify`；自签证书的 Ollama 渠道开启跳过校验后，取模型列表仍报证书错误。与 NewAPI `c6741c36a` 一致 | `relay/channel/ollama/relay-ollama.go` | — | NewAPI 修复后按其实现同步 |
| NewAPI `801456a06` | 已吸收 | Responses WebSocket 会话结束前向双方发送关闭帧：Token 被拒（401/403），或同一连接的身份、凭据、最终模型变化需要重连时为 1008；空闲上游正常关闭时沿用上游的 1000/1001；请求被截断、超时或失败时为 1011；客户端关闭时向上游发 1000；关闭后才完成的上游握手同样收到 1000。每帧最多等待 1 秒，不等待忙碌的写入方；计费与预留不变。本地没有上游按渠道登记活动连接的 `wsmanager`，停用或删除渠道不会主动关闭已建立的连接，渠道停用的 1008 场景不适用 | `controller/responses_websocket_session.go`、`controller/responses_websocket_call.go` | `TestGatewayResponsesWSCloseCodes`、`TestResponsesWSShutdownSendsCloseFramesWithoutWaitingForWriters` | 本地引入停用渠道时关闭活动连接的机制后补 1008 |
| NewAPI `977450a3e` | 不适用 | 本地逻辑请求频率限制没有进程内计数：未配置 Redis 时返回 503 `rate_limit_unavailable`，Redis 计数拒绝返回 429 `rate_limit_exceeded`，两者都带 OpenAI 错误体并写日志，见[发布与回滚](../routing/GATEWAY_RELEASE.md)。Responses WebSocket 错误映射的注释改为上游措辞 | `middleware/model-rate-limit.go`、`controller/responses_websocket.go` | `TestModelRateLimitWithoutRedisExplainsRejection` | 本地恢复进程内逻辑频率计数时，按上游返回同一错误体与日志 |
| NewAPI `1ca79d57e` | 不适用 | 本地 `/api/perf-metrics/summary` 只返回各模型 24 小时的延迟、成功率与 TPS，没有逐小时状态序列；Vue 模型卡用成功率驱动健康条，不渲染样本，因此没有灰色空档。不增加 `recent_model` 参数与 `recent_success_series` | `pkg/perf_metrics/metrics.go`、`frontend/src/composables/useModelMarket.ts` | — | 模型卡改为展示逐小时样本时按上游补齐查询 |
| AxonHub `b6d184a8` | 已吸收 | 本地没有空响应检测管线，同一缺陷在流式有效输出判定中：只含 `delta.audio` 的 Chat 流不计有效输出，超过 256 KiB 时在前导缓冲上限处失败，已交付的音频也按无输出释放。`StreamPayloadState` 把带 `data` 或 `transcript` 的音频增量计为有效输出，空音频对象仍不算 | `relay/helper/stream_scanner.go` | `TestChatAudioDeltaIsSemanticOutput` | — |
| AxonHub `9ca15ea4` | 不适用 | Antigravity 5 小时与每周额度窗口；本地没有 Antigravity 渠道或供应商额度检查 | — | — | 引入该渠道时 |
| Sub2API `3bfd40f5afd820ff7fae32f3f26c75fe3e0dd504`（缓存引用 403） | 已吸收 | HTTP 403 仅准确匹配 `CachedContent not found (or permission denied)` 时归 input；不重试、换渠道、标记渠道/模型/密钥健康失败。结构化凭据错误仍优先，普通权限 403 及只包含该词组的其他消息继续归 permission | `service/route_health.go` | `TestClassifyRouteErrorRecognizesProviderKeyAndModelFailures` | Google 改变缓存失效错误结构时增加明确识别，不扩大为任意 403 输入错误 |
| Sub2API `63800c50d462efc6436e96b9dd46ae8f98a054bc`、`3abd64658297f5ec47ad09120a0a26d6081d42de` | 已吸收（既有） | 取消、已提交和副作用不重放；本地转换/解析错误通过唯一尝试分类器归 input，不污染供应商健康 | `controller/relay_attempt.go`、`service/route_outcome.go` | `TestRelayAttempt*`、`TestClassifyAttempt*` | 增加协议探测或新错误来源时复评 |

## 支付

| 来源 | 状态 | 当前行为与本地合同 | 本地位置 | 守护测试 | 复评条件 |
| --- | --- | --- | --- | --- | --- |
| NewAPI 依赖 `Calcium-Ion/go-epay` v0.0.5 | 已吸收 | 下单签名与回调验签由 go-epay 计算；本地 `pkg/epay` 保留报文边界、金额精确解析、网关地址、商户与订单快照核对；go-epay 错误文本不进入回调日志。大写十六进制签名按签名无效拒绝，订单留待补单 | `pkg/epay/epay.go` | `TestNotificationRejectsUppercaseHexSignature`、`pkg/epay/` 全量测试 | — |
| NewAPI `0908d9e7`；Sub2API `d1aac6b9` | 已吸收 | 键为空或含 `&`、`=`，以及参与签名的值含 `&` 时拒绝；`BuildPurchase` 拒绝带查询串或片段的回调地址；通知中出现 `device`、`notify_url`、`return_url` 即拒绝；未知扩展字段仍参与验签；解析拒绝只记录请求方法和固定原因 | `pkg/epay/epay.go` | `pkg/epay/epay_test.go` | 网关在扩展字段值里带 `&` 或回显下单字段时回调被拒，走管理员补单 |
| NewAPI `feefe09f`（Waffo） | 已吸收 | Waffo 验签后校验配置店铺，创建结账前拒绝空店铺 | `service/waffo_pancake.go` | `TestWaffoPancakeWebhookBindsVerifiedStore` | — |
| Sub2API `5637f469`（充值赠金阶梯） | 延期 | 本地只有金额折扣和报价快照，没有赠金阶梯 | — | — | 先明确订单快照、幂等资金事件、退款与展示合同 |
| Sub2API `f3dc0835`（匿名订单枚举） | 不适用 | 本地没有匿名订单查询，用户订单由登录鉴权和订单归属保护 | — | — | 公开订单恢复流程独立立项时 |

## 前端

| 来源 | 状态 | 当前行为与本地合同 | 本地位置 | 守护测试 | 复评条件 |
| --- | --- | --- | --- | --- | --- |
| Sub2API `e0019da46`（`BaseDialog`） | 已吸收 | 叠放弹窗按打开顺序判断最上层，关闭即出栈；最上层弹窗独占全局 Esc 与 Tab | `frontend/src/utils/modalStack.ts` | `ConsoleModal.spec.ts` | — |
| NewAPI `3f0224478` | 已吸收（Vue 改写） | 上传插件时在源码编辑框下方显示后端预览的模型、接口和连接配置，含 `allowedHosts` 或"未声明额外域名"；源码停止变化 300 ms 后请求预览，旧请求中止、迟到结果丢弃 | `frontend/src/features/task-plugins/components/UploadDialog.vue` | `upload-dialog.spec.ts`、`TestPluginMetadataPreviewReturnsDeclaredAllowedHosts` | — |
| NewAPI `128089fa7` | 已吸收（Vue 改写） | 全站国际化：控制台接口返回英文 `message` 加 `message_key`/`message_params`，Vue 在 API 层按以英文原文为键的扁平译文（`server-messages/*.json`，`{{name}}` 占位）翻译，控制台只保留 zh-CN 与 en；AI 客户端错误、邮件、通知按账户语言、`Accept-Language`、`DEFAULT_LANGUAGE` 依次决定（en、zh-CN、zh-TW），未声明语言时为英文，与上游默认一致，生产不设 `DEFAULT_LANGUAGE`；Vue 控制台只在手动切换语言时写回账户语言，所以从未保存过语言的账户收到的额度、渠道、工单与上游模型更新异步通知为英文，按用户决定保留此上游默认行为。本地偏离：登录时设备未选过语言才采用账户语言，手动切换写回账户，每个请求带 `Accept-Language`；`UpdateSelf`（`PUT /api/user/self`）只保存 en、zh-CN、zh-TW，别名存为规范值，不支持的值（含 `null`）返回 HTTP 400，NewAPI `128089fa7` 接受任意字符串并返回 200；`content_parts` 同样覆盖本地计费路径；`i18n` 首次使用时加载内置译文；网关写入的任务失败原因（`constant.TaskFailReasons`）和列入 `constant.ChannelStatusReasons` 的渠道停用原因为固定英文，前端按原文精确翻译，响应超时停用原因不在其中，见下一行；安全校验、账户、个人访问令牌的错误以及运维监控、任务插件管理接口的拒绝同样带 `message_key`；活动卡片名称由前端译文提供，接口不再返回中文 `title`/`tagline`；后端消息键只保留代码在用的部分；控制台原有的硬编码中文（系统设置目录与面板、模型广场供应商标语与“平台”分组、首页画布品牌名）改为 vue-i18n 键，系统设置目录只存键 | `i18n/`、`common/message.go`、`constant/`、`frontend/src/i18n/` | `TestServerMessageCatalog`、`TestLocalesRenderEveryKey`、`TestWebConsoleMessageResponse`、`TestBackendMessageLanguage`、`TestUpdateSelfSavesOnlySupportedLanguages`、`TestLogViewsKeepStructuredContent`、`TestSecurityErrorsCarryMessageKeys`、`TestAdminConsoleRejectionsCarryMessageKeys`、`serverMessages.spec.ts`、`systemSettingsCatalog.spec.ts`、`SystemSettingsDomainView.spec.ts`、`useAccountLanguage.spec.ts`、`account-language.spec.ts`、`log-content-parts.spec.ts` | 上游调整消息键或默认语言时比较；只有设 `DEFAULT_LANGUAGE=zh-CN` 时才需补本地日志格式的中文译文 |
| 本地（渠道响应超时停用原因） | 等待上游 | 渠道测试因响应超时自动停用时，原因 `status_code=408, response time …s exceeds the threshold of …s` 带耗时与阈值，按 `DEFAULT_LANGUAGE` 生成（生产为英文），不在 `constant.ChannelStatusReasons` 中，控制台无法按原文翻译，中文界面显示英文；与 NewAPI 一致 | `controller/channel-test.go` | — | NewAPI 修复后按其实现同步 |
| Sub2API `8167d85b3` | 已吸收 | `FilterSelect` 被禁用时收起已展开菜单，禁用状态下选择无效 | `frontend/src/components/common/FilterSelect.vue` | `FilterSelect.spec.ts` | — |
| NewAPI `95601910`、`users-table.tsx` | 已吸收 | `/api/next/admin/users` 支持校验过的 `group` 参数并返回 `group_counts`；Vue 用户页有分组筛选和分组列，接口未返回 `group_counts` 时按空对象处理。列显示设置存储键为 `_v2` | `controller/`、`model/`、`frontend/src/composables/useAdminUsers.ts` | `TestNextAdminUsersFilterByGroup`、`TestUserGroupFilterAndCountsAcrossDatabases`、`UsersView.spec.ts` | — |
| NewAPI `973cf8ef` | 刻意不吸收 | React 专属；Vue 密钥页不显示分组徽标 | — | — | Vue 密钥页显示分组时复评 |
| NewAPI `web/src/features/task-plugins/**` | 已吸收 | 任务插件页按上游目录和组件对应：总开关、已安装与市场标签、详情抽屉、上传与 URL 导入、源码差异、沙箱 dryrun、更新日志；渠道表单绑定插件时填入插件默认 Base URL。本地差异：diverged 与降级安装二次确认、市场索引与源码只接受 HTTPS、删除不提供 `force`、源码纯文本查看 | `frontend/src/features/task-plugins/` | `frontend/src/features/task-plugins/__tests__/` | 渠道表单选插件后自动填模型和名称未吸收，有需求时复评 |
| NewAPI `f5e54ccdb` | 已吸收 | 插件市场索引、源码和图标请求做 HTTP 重验证，保留取消、大小上限、`credentials: omit` 与 SHA-256 校验 | `frontend/src/features/task-plugins/lib/plugin-url.ts` | `frontend/src/features/task-plugins/__tests__/api.spec.ts` | — |
| NewAPI `task-artifacts.tsx` | 已吸收 | 任务日志结果列按需请求产物接口，只接受宿主签名地址；`result_discarded` 不发请求，`artifact_not_found` 显示空态；签名地址不缓存 | `frontend/src/components/console/log-ui/TaskArtifactsCell.vue` | `TaskArtifactsCell.spec.ts` | — |
| NewAPI `6c14c0762` | 已吸收 | 比价表按字段比较，表达式按词段显示增删；不执行表达式、不注入 HTML | `frontend/src/features/pricing/diff.ts`、`frontend/src/components/admin/PriceDiffCell.vue` | `frontend/src/features/pricing/__tests__/diff.spec.ts` | — |
| NewAPI `81121206` | 已吸收 | 渠道列表投影只读 `status_reason`、`status_time`，服务端去除凭据、账号、URL 和原始响应正文，最多 500 字符 | `controller/channel_status_reason.go`、`frontend/src/components/console/channels/ChannelStatusBadge.vue` | `TestChannelStatusSummaryRedactsBothAdminRepresentations`、`TestChannelStatusSummaryBoundsAndStatus` | 新错误结构须先定义安全投影 |
| NewAPI `dfd3cd893` | 已吸收 | Vue 系统设置路由使用 `requiresRoot` 守卫 | `frontend/src/router/adminRoutes.ts`、`frontend/src/router/index.ts` | — | — |
| NewAPI `5fe8917be`、`56758edf9` | 刻意不吸收 | React 的 zhTW 规范化链路；Vue 已有语言持久化 | — | — | 图表或字体出现本地具体缺陷时复评 |
| Sub2API `53158587` | 已吸收 | Axios 固定 `1.20.0`；公开 HTTP 请求使用 Fetch `credentials: omit`，不带同源 Cookie | `frontend/package.json`、`frontend/src/api/httpTransport.ts` | `frontend/src/api/__tests__/httpTransport.spec.ts` | — |
| Sub2API `51060657` | 已吸收 | 渠道 priority、weight 由 `ChannelInlineNumber` 原地编辑，失败恢复原值 | `frontend/src/components/console/channels/ChannelInlineNumber.vue` | — | — |
| Sub2API 其余界面竞态修复 | 刻意不吸收 | 本地已有等价守卫或没有对应页面 | — | — | 引入对应页面时复评 |
| NewAPI `93ea55f3c` | 已吸收（Vue 改写） | 访问令牌到期时间与日志清理时间的 `datetime-local` 输入设 `step="1"`，选到秒并按秒提交；日志清理确认文案显示到秒。时间框部分清空时浏览器按无效输入拦截提交，不发出无效时间；访问令牌到期时间整体清空表示使用默认有效期。本地偏离：日志、看板等页面的日期范围筛选只按天选择（开始取当天 0 点，结束取 23:59:59），没有时间部分，不做秒级改造 | `frontend/src/components/console/PersonalAccessTokensPanel.vue`、`frontend/src/components/admin/ops/OpsSystemTasksCard.vue` | `PersonalAccessTokensPanel.spec.ts`、`OpsMonitorView.spec.ts` | 日期范围筛选加入时间部分时复评 |
| NewAPI `96275d224` | 已吸收 | 本地已有该行为，未改代码：`DateRangePicker` 的月份与星期名称用 `Intl.DateTimeFormat` 按界面语言生成；登录会话列表的相对时间按界面语言输出（`relativeTime`）；通行密钥卡片显示绝对时间；公告编辑器不显示相对时间。控制台只有 zh-CN 与 en，没有上游 `zh`/`zhCN` 键名错配。原生日期输入的日历语言由浏览器决定 | `frontend/src/components/common/DateRangePicker.vue`、`frontend/src/utils/format.ts` | `format.spec.ts` | 新增界面语言或改用第三方日历组件时复评 |
| NewAPI `06dd76a9d` | 不适用 | Vue 的 Playground 与 Lab 页面及业务入口已退役；浏览器只保留界面偏好，没有该对话存储路径 | `router/web-router.go`、`frontend/src/router/` | — | 独立批准重新引入对话页面时复评 |
| NewAPI `1fb6d07fe` | 不适用 | Vue 没有引入 `react-icons` 或 Simple Icons；图标只按名称从 `lucide-vue-next` 具名导入，供应商标志是 `public/models/` 下的静态图片 | — | — | 引入整包品牌图标库时复评 |
| NewAPI `b892a6b2e` | 已吸收（Vue 改写） | 路由页面代码加载失败（Chromium、Firefox、Safari 的模块加载错误与 Vite 的 CSS 预加载错误）时，用 `sessionStorage` 标记后整页加载失败的目标地址一次；再失败显示可访问的重试页，导航成功后清除标记；无法写入标记时不自动加载，避免循环。本地原有逻辑只识别 Chromium 与 Safari 的脚本错误，且加载的是失败前的页面；Vue 错误页本来不显示状态码。可视化测试预置标记后断言重试页，契约不变 | `frontend/src/router/index.ts` | `chunkReload.spec.ts`、`tests/visual/routes.spec.ts`（重试页） | — |
| NewAPI `0f38a14a5` | 不适用 | Vue 控制台没有字体偏好设置，正文字体固定为内置 Inter、Noto Sans SC 加系统无衬线回退 | `frontend/src/styles/base.css` | — | 加入字体偏好时复评 |
| NewAPI `1ca79d57e`（前端） | 不适用 | Vue 模型卡只读 `/api/perf-metrics/summary` 的 24 小时汇总 `success_rate`，用 `HealthMeter` 画单个健康度，不读逐小时 `recent_success_series`，没有灰色占位格；解析器忽略新增字段，汇总字段不变时无需改动。后端查询改动由运行时流处理 | `frontend/src/composables/useModelMarket.ts`、`frontend/src/api/liveContracts.ts` | — | 模型卡改为显示逐小时成功率时按上游只渲染有数据的样本 |
| NewAPI `633b4c5b190dc1af99a2814391354d86948e7943`（日志导出） | 已吸收（Vue 改写） | CSV/Excel/JSON 由一次 NDJSON 请求生成，只有成功终态、计数匹配才下载；取消会 abort。导出字节保留精确金额、字段名随语言、日期独立选择；真实 Excel 打开长数字的精度仍需验证。移除日志旧 30 天静默裁剪及 10,000 行分页截断，仪表盘不再以固定 30 天拆分绕过共享范围 | `frontend/src/views/console/LogsView.vue`、`frontend/src/api/logExport.ts`、`frontend/src/components/console/log-ui/logExport.ts` | `frontend/src/api/__tests__/logExport.spec.ts`、`frontend/tests/visual/log-export.spec.ts` | 前端选格式和后端终态保持对齐，见[日志合同](../console/LOGS.md) |
| Sub2API `9a51333101a849bb948abf94c5ed53682e373edd`、`8b74741efe35a42c67bbb35b821af9a12aca2c24`（分组多语言） | 延期 | 当前组名同时是路由、权限、价格标识；没有独立展示名/描述元数据，不把翻译值写回稳定标识 | — | — | 先定义展示元数据、语言规范化、权限、缓存键及 Vary 保留 Origin，见 SYNC1013-03 |
| Sub2API `c9e4b267b6fac1f00060eab983e606f7a2b2d4fc`（同步模型失败） | 已吸收（既有） | detect/apply 已显示 API 层统一错误投影，失败保留弹窗；取消和 sequence 避免迟到结果覆盖 | `frontend/src/components/console/channels/ChannelModelSyncDialog.vue`、`frontend/src/api/client.ts` | 现有 API 错误投影测试；本轮未新增该组件专项 | API 错误 envelope 或同步交互变化时复评 |

## 依赖与构建

2026-10-13 11:27:00 UTC 重新采集三个官方 HEAD 与 release，均未变化。采集器仍报告 `incomplete`：live 参考目录已在上一批原样迁出；另行核对保留副本的完整 HEAD、干净状态及官方对象，不能改写原始报告为成功。此次采用 Tailwind 官方指南、AxonHub 的 Vite 插件接入与官方 global-agent 发布方案；NewAPI/Sub2API 未提供可直接复制且能关闭这些旧链的 Vue 方案。未将参考项目的 React、Rsbuild、shadcn 或账号池框架重新引入。

| 来源 | 状态 | 当前行为与本地合同 | 本地位置 | 守护测试 | 复评条件 |
| --- | --- | --- | --- | --- | --- |
| 本地安全升级（Go 与 x 系列依赖） | 本地偏离保留 | 构建镜像为 `golang:1.26.9-alpine`；`golang.org/x/net` v0.60.0、`x/text` v0.42.0、`x/image` v0.45.0、`gorilla/websocket` v1.5.3，x 系列保持本地安全升级、gorilla/websocket 已与本轮上游对齐；relaykit 保持上游 `go 1.25.1`。前批次 govulncheck 可达漏洞为 0，本轮证据另按候选门槛记录 | `Dockerfile`、`go.mod` | `govulncheck ./...`、`go mod verify` | 合并上游 `go.mod` 时保留本地版本；NewAPI 升级到同等或更高版本后回归 |
| NewAPI `192802ee0` | 已吸收 | Dockerfile、release 与 Electron 构建以 moejs 的 `default.pgo` 编译 | `Dockerfile`、`.github/workflows/release.yml`、`.github/workflows/electron-build.yml` | — | 构建时间与运行收益未测 |
| Sub2API `0f9d460d` | 已吸收 | Vue 使用 3.5.43 系列；原 `source-map-js` 覆盖已删除，正常解析仍为安全版 1.2.2 | `frontend/package.json`、`frontend/bun.lock` | frozen 安装与类型/构建 | 锁变化后检查实际解析版本 |
| NewAPI `7e25797d14681e4e16c2b46831450e7d6a5ccc74`、`ba29f06453942d9b5c0b32330340b8a000692c4f`、`3c9fb2a0de168d88ce5bffbf44acecdc1cdcdd39`、`1c58f3d161fa32755b372d9449f2d5e077d50a5d`、`7f51a97226c43bc082a065bdc8b40b719c4bdcce`、`dd59854050f26f4f67bb3dac5274af5e17013462` | 已吸收 | Electron 41.10.6；xmldom 0.8.15、brace-expansion 各主版本安全补丁、fast-uri 3.1.8、js-yaml 4.3.2、undici 6.29.0/7.30.0。保留 electron-builder 26.15.3，定向改锁而非整锁升级 | `electron/package.json`、`electron/bun.lock` | frozen-lockfile 安装、Node 入口语法检查、Electron 运行时版本与 Linux --dir 打包；包内 Go 二进制哈希与同次 Vue/Go 构建一致，目标系统图形启动、托盘和安装包仍待验收 | 桌面候选验证绑定真实 Vue/Go 构建与目标平台，见 SYNC1013-05 |
| 本地依赖审计 [GHSA-ch52-4w7c-c8xp](https://github.com/advisories/GHSA-ch52-4w7c-c8xp) | 已吸收 | electron-builder 的下载工具链中 http-cache-semantics 4.2.0 高危采用兼容修复 4.3.0；不改变服务端缓存策略 | `electron/bun.lock` | `bun audit --json` | 后续锁变化重新检查真实依赖链 |
| 本地依赖审计 [GHSA-hp3w-g68c-fv3c](https://github.com/advisories/GHSA-hp3w-g68c-fv3c)、[global-agent v4.1.0](https://github.com/gajus/global-agent/releases/tag/v4.1.0) | 已修复 | 保留 electron-builder 26.15.3、get 3/5 两分支及各平台 optional 包，定向使用官方 global-agent 4.1.3；保留 CJS bootstrap 与原代理环境变量合同。正式代理库移除 roarr→sprintf-js 链，不忽略漏洞或伪造包。`ROARR_LOG` 不再生效，get 3/Builder 的显式 logger 与 get 5 的 Undici 分层说明、安全清单下载诊断见[桌面说明](../../electron/README.md#download-diagnostics) | `electron/package.json`、`electron/bun.lock` | frozen 安装与 audit；两代实际 downloadArtifact 的 HTTP/HTTPS CONNECT、NO_PROXY、三平台文件名及损坏 SHA256 拒绝共 12 项；同次 Vue/Go 产物 Linux --dir 打包；安全诊断的代理/NO_PROXY/TLS拒绝/初始化失败四场景 | 原 SYNC1013-04 关闭；get 或代理库升级后重新跑下载合同，目标平台安装与 GUI 仍按 SYNC1013-05 验证 |
| AxonHub `3cb7e3335cadd463fe7748c47dd003802125e3dd`（Vite 集成）、[Tailwind 官方升级指南](https://tailwindcss.com/docs/upgrade-guide) | 已吸收（Vue 适配） | Tailwind/@tailwindcss/vite 4.3.3，Vite 原生插件与 CSS 主题；分离 RGB 通道与主题变量，保留深浅色、固定字号行高与断点重置；全部断点单点恢复 640/768/1024/1280/1536px，避免浏览器默认字号变化与既有 CSS/JavaScript 像素判断冲突；保留容器层级、动画/按压 transform 覆盖、阴影/圆角和 v3 兄弟间距/Preflight 语义（含控件字体继承字段）；清理旧无效而新版开始生效的类。删除旧 PostCSS/autoprefixer 构建入口。支持下限 Safari 16.4、Chrome 111、Firefox 128 | `frontend/vite.config.ts`、`frontend/src/styles/`、`frontend/README.md` | 旧生产 CSS/页面基线；嵌套/断点字号与普通/减少动画合同；完整 Vitest、类型/lint/格式/生产构建、浏览器全量及桌面/手机明暗页面复查；16/20px 默认字号下的 sm/md/lg 实际路由边界 | 新增主题或工具类时保持单一 CSS 主题来源；Chromium 不代表 Safari/Firefox/真机运行证据 |
| 本地 Vue 生产页面回归 | 已修复 | 手机导航初次按后备字体居中后，真实字体变宽而容器不变，旧 ResizeObserver 不再触发。沿用同一居中入口，在字体就绪且组件仍挂载、当前页未实际横向滚动时重算；点击、Tab/Enter 不误锁定，路由切换重新允许校正，新选择后的横移仍受保护；桌面隐藏后再显现逻辑保留 | `frontend/src/components/console/ConsoleNavScroller.vue` | 12 项字体/实际横移/卸载/路由单测；8 项明暗主题真实迟字体的点击、键盘选页、旧横移后切页、新选页后横移浏览器合同；原手机导航和隐藏再显现用例 | 修改字体加载、导航结构或居中逻辑后重验；用户手动位置不能由异步字体完成事件覆盖 |
| 本地前端工具链审计 | 已吸收 | Vitest/mocker 4.1.11、parser 7.1.6 等安全版本保留；Vue/Vite 正常解析 PostCSS 8.5.30、brace-expansion 5.0.12、source-map-js 1.2.2 后删除无必要 override。Tailwind 4 的样式与最低浏览器版本按下方合同独立验收 | `frontend/package.json`、`frontend/bun.lock` | frozen-lockfile、全量 Vitest、typecheck/lint/format/build 与完整浏览器清单 | 锁变化后重新核对审计、许可和真实依赖链 |
| 本地依赖用途审计（用户要求清理） | 已清理 | 除此前两行旧 moejs alpha.5 校验和，本次删除直接 autoprefixer/PostCSS、旧 Tailwind/PostCSS JS 配置、三项不必要 override，以及无调用 canvas 图标占位脚本。Electron dev-app 的 NODE_ENV 确有运行模式用途；Bun 1.4.0 的 Bun Shell 未通过父环境 production 的赋值合同，保留 cross-env。现用 Go 模块与 Vue 页面库保留，锁文件没有 React/Redux/AntD/Semi | `go.sum`、`frontend/`、`electron/` | 实际引用/环境消费路径核对、开发入口环境语义与两套 frozen 安装；受影响构建和页面验收 | 入口删除或替换后重新清理，不能仅按 import 字面匹配或包脚本引用判断用途 |
| 本地 Go 漏洞审计 | 已修复有兼容补丁的条目 | compress 1.18.7 修复 [GO-2026-5841](https://pkg.go.dev/vuln/GO-2026-5841)；独立 RelayKit 的 x/text 0.41.0 修复 [GO-2026-5970](https://pkg.go.dev/vuln/GO-2026-5970) 和 [GO-2026-6629](https://pkg.go.dev/vuln/GO-2026-6629)，保持 go 1.25.1。root 原有 x/text 0.42.0 不变。x/crypto 的 [OpenPGP 模块告警](https://pkg.go.dev/vuln/GO-2026-5932) 无修复，但当前代码未导入该包；不能称全部模块告警清零。ClickHouse Zstd 和 profiling 的实际运行尚未验证 | `go.mod`、`go.sum`、`relaykit/go.mod`、`relaykit/go.sum` | 补丁后独立模块构建/验证、RelayKit 全量测试、主模块全量/vet/govulncheck；`TestZstdRequestDecompressionContract` 覆盖正常解码、无效帧、解压后限额及 body 释放 | 锁变化后重新查实际包图和官方漏洞；引入 OpenPGP 前重新设计依赖 |
| 本地依赖审计 [GHSA-vfj7-8cjw-p6xm](https://github.com/advisories/GHSA-vfj7-8cjw-p6xm) | 已修复 | Tailwind 3 的 micromatch、fast-glob、chokidar 均随真实构建迁移退出；braces 3.0.3 不再被任意锁路径引入。没有等待不存在的 braces 补丁，也没有删除现用 CSS 能力 | `frontend/bun.lock` | 锁与已安装包图核对，bun audit={}，完整浏览器/构建检查 | 原 SYNC1013-06 关闭；新增 glob 工具或锁变化后复查所有路径 |
| 本地依赖审计 [GHSA-rj75-hqrm-r3gf](https://github.com/advisories/GHSA-rj75-hqrm-r3gf) | 已修复 | 旧 postcss-nested→parser 6 链随 Tailwind 3 退出；Vue ESLint 所需 parser 7.1.6 保留。按已授权的 Vue 工具链迁移修复，没有强制覆盖旧解析器主版本 | `frontend/bun.lock` | frozen 安装、实际依赖图、bun audit={} 与完整前端检查 | 原 SYNC1013-07 关闭；CSS/ESLint 工具升级后复查 |

## 鉴权、管理与目录

| 来源 | 状态 | 当前行为与本地合同 | 本地位置 | 守护测试 | 复评条件 |
| --- | --- | --- | --- | --- | --- |
| NewAPI `caca52f8` | 已吸收 | `personal_access_tokens` 保存摘要、scope、过期与撤销，明文只展示一次；名称与权限 PATCH 需单次 Proof 并与本地动态权限求交集；旧用户行令牌路径已删除。接口见[鉴权合同](../authentication.md) | `model/personal_access_token.go`、`controller/access_token.go` | `TestPATLifecycleScopeDigestAndProof`、`TestPATLimitsAndExpiredOrDeletedOwner` | 新增 scope 时复评字段权限 |
| NewAPI `2506e1b9`、`6cbb1c7e` | 已吸收 | 共用角色枚举；管理入口的敏感操作绑定单次 Proof，复用 JWT、AuthFlow 和现有权限，不引入上游浏览器 session | `controller/`、`middleware/` | `TestManagedUserActionPreservesConcurrentSecurityAndWalletChanges` | — |
| Sub2API v0.2.11（Token 创建限制） | 已吸收 | Token 数量与每小时创建窗口在同一用户锁事务内检查，软删除记录仍计数；不用 Redis 失败放行 | `model/token_creation.go` | `TestTokenCreationLimitsAcrossDatabases`、`TestTokenCreationRollbackDoesNotSpendRateLimit` | — |
| Sub2API `2529371b` | 已吸收 | 验证码只存 SHA-256，持锁累计失败 5 次失效；密码重置在账户写入前原子消费 token，并发只有一个成功，失败不恢复 | `common/verification.go`、`controller/misc.go` | `TestVerificationCodeConcurrentFailuresCannotLoseAttemptCount`、`TestPasswordResetConsumesTokenBeforeConcurrentAccountWrites` | — |
| 本地（Token 更新） | 本地偏离保留 | 设置类 PUT 只写请求体携带的字段，漏传的 `allow_ips`、`model_limits_enabled`、`model_limits`、`expired_time`、名称与预算保持原值；`""` 与 `null` 仍清空；`status_only` 缺 `status` 拒绝。NewAPI 整条替换，AxonHub 漏传保留 | `controller/token.go` | `TestUpdateTokenKeepsOmittedSettings` | 上游改为按字段更新时回归 |
| NewAPI `31d70fca3`（#6329 会话刷新限流） | 本地偏离保留 | `POST /api/user/auth/refresh` 使用独立的会话刷新 IP 额度（60 次/分钟），不再与登录、注册共用 Critical 额度（20 次/20 分钟）；同由 `CRITICAL_RATE_LIMIT_ENABLE` 控制。Vue 前端每次整页加载都刷新会话，共用额度时同一 IP 刷新十余次即无法打开页面或登录 | `router/api-router.go`、`middleware/rate-limit.go`、`common/constants.go` | `TestSessionRefreshDoesNotSpendTheLoginBudget` | 上游调整 refresh 限流或前端不再每次加载都刷新时复评 |
| AxonHub `8dfb6091`、`95fc2df2` | 已吸收 | 映射 IPv4 地址与 IPv4 规则互相匹配；清空 `allow_ips` 解除限制并清缓存 | `common/`、`model/token.go` | `common/` 测试 | — |
| AxonHub `b5031b0f` | 已吸收 | 模型能力卡为可空 TEXT JSON，manifest 复用授权渠道、能力快照和价格资格；市场、模型列表和 `/v1/models` 同源投影，Token 页导出 Codex、Pi 配置。原生与插件端点各用实际报价解析判价。合同见[网关 API](../routing/GATEWAY_API.md#模型-manifest-与客户端配置) | `dto/model_capabilities.go`、`service/model_manifest.go`、`service/model_manifest_export.go` | `TestModelManifestEndpointPricingMatchesExecutingProtocol`、`service/model_manifest_test.go` | 客户端导出格式变化仍只从 manifest 派生 |
| AxonHub `2bf8e2c9`（attempt 与关联思路） | 已吸收 | 请求诊断为无正文投影，记录真实发送、排队与首个有效输出，默认保留 7 天，按用户与 Token 隔离；合同见[网关 API](../routing/GATEWAY_API.md#请求诊断与关联) | `service/`、`model/`、`controller/` | `TestRequestDiagnosticOwnershipProjectionAndTokenCorrelation` | — |
| Sub2API `6e315616`、`c2d5bbd9`、`bbba01da`、`66a9e2e6` | 刻意不吸收 | root 由安装向导按操作者密码创建，无默认凭据；有预留或在途恢复时拒绝删除 Token；Codex 导出为静态 TOML；无 TypeSafe 渠道 | `model/` | `TestWalletIngressDeletionProtectsZeroAmountRecovery`、`TestAccountDeletionWaitsForSettledConsumptionDelivery` | 引入自动初始化、新删除路径、远程目录或 TypeSafe 时 |
| Sub2API `fdce5b8d`、`ad05eda1` | 刻意不吸收 | Grok CLI/OAuth 身份头与 billing probe；本地 xAI 使用官方 API | — | — | 独立批准 CLI/OAuth 产品能力和认证设计后 |
| Sub2API `34fe845f`（TypeSafe 协议） | 延期 | 没有 System One 原生适配 | — | — | 新供应商范围确认后，先定义请求、响应、计量和转换损失，再接现有渠道 |
| Sub2API `71901305`（Token 分组排序） | 延期 | 未实现 | — | — | 先定义排序允许列表、分页前排序、稳定并列顺序、用户隔离和三库回归 |
| Sub2API `43c4c882`、`46f5f5e8` | 不适用 | Antigravity 账号池错误脱敏与账号池文案，本地没有该账号池 | — | — | 引入账号池时 |
| AxonHub `5c789977`、`563f89db`、`89d36d61`、`5af5400e`、`dddd7d70`、`4904a5df` | 刻意不吸收 | Codex 订阅渠道与 Vue Playground 已退役；敏感词为拦截不改写；模型开发商数据与 neuralwatt 窗口不进入价格和路由 | — | — | 对应产品能力重新获批时 |
| AxonHub Decisions API、Codex 窗口、Antigravity、React/GraphQL/Ent、全文请求存储；Sub2API 完整账号池与自动兑换 | 刻意不吸收 | 不符合当前单实例网关和资金模型 | — | — | 对应产品能力获批时 |
| 本地 AUDIT1009B-15（MySQL 分组大小写） | 等待上游 | 用户列表分组统计在 MySQL 默认排序规则下不区分大小写，`vip` 与 `VIP` 合并计数并互相筛中；PostgreSQL、SQLite 分开计数。只影响显示，与 NewAPI 一致 | `model/user.go` | — | NewAPI 修复后按其实现同步 |
| 本地（通知邮件转义） | 等待上游 | 邮件通知正文不对上游文本做 HTML 转义：渠道停用通知的原因（`{{.Reason}}`）和上游模型更新通知中的模型名原样进入邮件 HTML，`service/user_notify.go` 只转义工单通知；与 NewAPI 一致 | `service/channel.go`、`service/user_notify.go`、`controller/channel_upstream_update.go` | — | NewAPI 修复后按其实现同步 |
| 本地（兑换失败日志） | 等待上游 | 兑换失败时服务器日志 `failed to redeem key %s for user %d: %s` 写入兑换码明文；与 NewAPI `controller/user.go` 相同 | `controller/user.go` | — | NewAPI 修复后按其实现同步 |
| NewAPI `633b4c5b190dc1af99a2814391354d86948e7943`（日志权限与范围） | 已吸收 | `/api/log/self/export` 保留用户归属、业务类型与 DTO 字段白名单；`/api/log/export` 保留管理员/PAT 权限。普通用户导出开关默认开启，普通用户查询与两种专用导出的最大范围默认 365 天；管理员全局列表不受该普通用户查询限制。每用户导出锁按单实例进程管理，取消/失败释放，SQL 或流写入失败不伪造成功终态 | `controller/log_export.go`、`model/log_export.go`、`setting/operation_setting/log_setting.go` | `TestLogExportPreservesUserScopeFiltersAndProjection`、`TestLogExportLockDisableAndFailureHaveNoSuccessTerminal`、`TestLogExportCancellationReleasesLock`、`TestLogExportFiltersAcrossDatabases` | 三库与 PAT/鉴权回归按候选门槛验证；扩成多实例前改锁合同，见[日志合同](../console/LOGS.md) |
| NewAPI `63cecab824bad1a421d19950004c309bb2054c09`（Sub2API 余额） | 已吸收 | type 59 使用 `GET <base_url>/v1/usage` 与 Bearer Key；明确 remaining 优先于 balance（含零），只有缺失 remaining 才读 balance。拒绝无效 Key、缺字段、非法/非有限数值、非 200 与无日/周/月额度的无限订阅；负钱包余额合法。查询/解析失败不刷新旧余额/更新时间，公共 UpdateBalance 返回持久化错误、调用方显式传播并拒绝假成功；保留渠道代理、TLS 与管理权限/no-store envelope；响应 Body 各路径释放 | `controller/channel-billing.go` | `TestSub2APIBalance*`（含持久化失败）、`TestBalanceQueryClosesFailedResponse`、`TestChannelUpstreamQueriesHonorTLSInsecureSkipVerify` | 供应商 usage envelope 改变时复评；不扩展为本地用户订阅资金源 |
| Sub2API `1c68f9996df8e0bd7933d94d4ca826bc3dc570c7`（Google Key） | 已吸收 | 裸 AQ.Ab（40+）和 AIza（35+）凭据在公共脱敏入口优先屏蔽，避免域名遮掩遗留长尾；普通 AQ. 文本不误伤，认证无需前缀白名单 | `relaykit/relayconvert/kitutil/mask.go` | `TestMaskSensitiveInfoRedactsGoogleCredentialsInUpstreamErrors` | Google 新增凭据格式时补同一入口，不另建脱敏规则源 |
| Sub2API `6f9b02f12c3d414ed324460a0e82d7793a471f72`、`daebb7a80c720763a01e5cef8799c030aed0b058`（Vertex 模型发现） | 延期 | Vertex 服务账号原生发现/详情未实现；通用模型同步不能当作 Vertex 原生发现已支持 | — | — | 复用认证、代理/TLS、分页、错误脱敏，定义 publisher ID 与授权 manifest 投影后实现，见 SYNC1013-02 |

## 结构差异

以下差异是 Ren2Hub 的产品与架构边界，合并任何上游改动时都须保留；上游在这些位置的改动只吸收行为合同，不复制其实现结构。

| 来源 | 状态 | 当前行为与本地合同 | 本地位置 | 守护测试 | 复评条件 |
| --- | --- | --- | --- | --- | --- |
| NewAPI React 控制台 | 本地偏离保留 | Vue 3 控制台是唯一 Web 前端，挂载在 `/`；上游界面改动以 Vue 实现必要部分 | `frontend/` | 前端全量检查 | — |
| NewAPI 额度模型 | 本地偏离保留 | 钱包是唯一资金来源，Token 是附加限额；schema 5 资金事件、冻结报价、持久化预留与先计费兜底，见[账务架构](../billing/IMPLEMENTATION.md) | `model/billing_*.go`、`service/billing_*.go` | 账务三库测试 | — |
| NewAPI 渠道选择 | 本地偏离保留 | 单实例统一选路、本地 FIFO 准入、发送前资格复核，见[统一路由](../routing/UNIFIED_ROUTING.md) | `service/request_route.go`、`service/route_score.go` | 网关路由测试 | — |
| NewAPI `2a15e3b1`、`1292b8b2`（ChatGPT Subscription 渠道） | 刻意不吸收 | type 57 已退役：无 OAuth、ChatGPT 转发、模型发现、凭据刷新与用量窗口；编号保留，历史行可读、可删除，不参与路由。标准 OpenAI 渠道保留 Codex 模型、Compact、WebSocket 与 Codex/Pi 导出，见[渠道接入类型](../channel/TYPES.md) | `constant/channel.go`、`controller/channel.go` | `TestGatewayOfficialCodexSDK`、`TestStandardOpenAICodexCompactAndWebSocket` | 独立批准订阅接入并重新明确认证、路由与资金边界后 |
| Sub2API 供应商用量窗口 | 刻意不吸收 | 原单凭据 Codex 窗口随 type 57 退役撤下；观察结果不能成为资金、路由或额度的第二份状态 | — | — | 新供应商窗口独立立项并复用已有合法认证时 |
| NewAPI 插件 API 英文文档 | 本地偏离保留 | 插件 API 文档以中文维护，并写入本地不可变版本、冻结报价与恢复合同 | `docs/plugin-api/` | — | — |
| 项目权限与租户 | 刻意不吸收 | 用户组、渠道组不当作租户；项目权限与预算隔离未实现 | — | — | 独立立项时 |

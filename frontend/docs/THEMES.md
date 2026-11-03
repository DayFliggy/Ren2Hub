# 主题与样式结构

前端只维护一套语义令牌和两种视觉主题：日间 `Desert Ledger` 与夜间 `B1 Studio`。夜间采用曜石炭蓝、暖金强调、展示级衬线标题、轻玻璃导航和不透明数据表面；日间保留原有配色、字体与手账质感，共享必要的响应式结构改进。

本轮实施范围、运行证据和未验证边界见 [B1 Studio 实施与验收](B1_STUDIO_ACCEPTANCE.md)。

## 文件职责

- `src/styles/fonts.css`：本地自托管 WOFF2 字体声明。
- `src/styles/tokens.css`：颜色、阴影、状态、品牌和 Canvas 共享令牌。
- `src/styles/tailwind.css`：Tailwind 4 入口、CSS 主题映射和保持现有布局的兼容 utility。
- `src/styles/tailwind-defaults.css`：恢复 Tailwind 3 生产 CSS 的边框、占位文字、控件字体继承、按钮光标和对话框默认值，并在 utilities 前输出公共页面容器。
- `src/styles/base.css`：文档默认值、焦点和选区规则。
- `src/styles/home.css`：首页专属视觉，选择器限制在 `.landing-shell`；仅首页滚动状态允许作用于 `html.hero-scrollbar-hidden`。
- `src/styles/console.css`：Console 的滚动区域，由懒加载的 Console 布局引入。
- `src/styles/theme-transition.css`：仅在人工主题切换时生效的根快照交融与图标动画。
- `src/styles/index.css`：全局汇总入口，依次引入字体、令牌、Tailwind、基础规则、首页和主题动效，不定义新的业务令牌。

组件必须使用 `--page-background`、`--surface-container-*`、`--text-*`、`--outline-*`、`--accent`、`--signal`、`--support` 和 `--status-*` 等语义变量，不得在业务组件中复制主题色板。旧的 `--surface-*`、`--card-shadow` 和 `--sketch-*` 仅作为兼容别名。

## Tailwind 构建与兼容约定

Tailwind 4 通过 `vite.config.ts` 中的官方 `@tailwindcss/vite` 插件构建，配置位于 `tailwind.css`，不再使用 JavaScript 配置或独立 PostCSS 配置。项目已移除直接声明的 `autoprefixer` 和 `postcss`；Vue 与 Vite 所需的间接 PostCSS 依赖仍由各自维护。

`tokens.css` 的 `--palette-white`、`--palette-black`、`--palette-brand-*` 和 `--palette-accent-*` 保存原有 RGB 通道值；`tailwind.css` 用 `@theme inline` 将它们包装为完整颜色，映射到 Tailwind 的 `--color-*` 命名空间。业务组件继续使用语义变量，浅色与深色仍由现有主题令牌切换。不要把裸 RGB 通道写入 Tailwind 的 `--color-*` 变量。

主题声明保留 `theme` 层，Preflight、默认值兼容规则和 utilities 则按顺序保持在层外，以延续原生产 CSS 与 Vue scoped 样式的优先级。`tailwind-defaults.css` 保留旧版的灰色边框、占位文字、按钮 `pointer`、禁用控件 `default` 光标，以及对话框居中；它位于 utilities 之前，显式的语义颜色和 `disabled:cursor-*` 仍优先。

`button/input/select/textarea/optgroup` 先用 `font: revert` 恢复浏览器字体默认值，再按旧版逐项继承 family、feature/variation、size、weight 和 line-height；其他字体字段保留原浏览器边界，避免暗色账本的 `tabular-nums` 意外传到控件。文件上传按钮保留原生字间距。兼容规则位于 utilities 前，显式的斜体、字体变体与字重类继续优先。验证应设置祖先字体变体、斜体、字宽和 kerning/optical 等附属字段，不能只比较普通字号；字体 shorthand 的字段关系见 [CSS Fonts 规范](https://www.w3.org/TR/css-fonts-4/#font-prop)。

公共页面 `.site-container` 按旧版展开 components 层后的顺序输出到层外，保留居中、左右安全内边距和 utility 覆盖顺序。不要把现有组件规则放回原生 `components` 层，否则层外 Preflight 的 margin/padding 重置会覆盖它。字号 utility 保留旧版固定行高及断点重置规则：任意字号继承父元素的实际行高，响应字号重置较小断点的 `leading-*`，同断点显式 `leading-*` 仍优先；这同时保护状态徽标、表格文字、控制台标题和首页副标题的布局。

`sibling-space-y-*` 和 `sibling-divide-*` 保留旧版对隐藏节点的过滤及后续兄弟节点规则；圆角、模糊、轮廓和阴影类按旧生产值迁移。旧版未产生可见阴影的未指定类型 `shadow-[var(--…)]` 类已删除，避免升级后意外增加高程；原有 CSS 定义的语义阴影继续按组件规范使用。兼容规则以升级前的实际生产 CSS 为基线，不以新版同名类的默认值替代既有外观。

浏览器最低版本及迁移后的类名说明见[前端说明](../README.md#样式构建)。主题修改仍须覆盖浅色、深色和减少动画三种条件。

独立 transform 属性的兼容取消使用 CSS-wide `initial`。原入场动画覆盖所有 transform utility，深色签到不旋转，`touch-snappy` 按压统一缩放 0.98、减少动态效果时不缩放；不要改回会被生产压缩视作冗余而删除的 `none`。旧版无效的变量阴影、变量颜色透明度及冗余尺寸类已移除，避免迁移新增背景、边框或阴影。

日期浮层的入离场状态取消独立 `translate`，并使它与 `transform` 使用相同过渡时间和缓动，保留旧版动画对水平居中位移的覆盖及插值路径；终态定位不变。减少动态效果时沿用全局关闭过渡的规则。验证必须包含实际压缩后的生产 CSS，开发服务器不能证明这些兼容声明在压缩后仍然存在。

## 夜间视觉规范

夜间核心色为页面 `#1c1f26`、主表面 `#262a34`、表头 `#2e333f`、浮层 `#373d4b`、主文字 `#eef1f7`、次级文字 `#c3cad7`、辅助文字 `#9aa3b4` 和暖金 `#e4c06b`。状态、负载和图表的语义区分继续保留，换底色后重新测量对比度。

金额等强调文字使用 `--accent-text: #f2d386`，与负载关注档保持 OKLab 距离大于 5.5。夜间热力图第三档混合比例为 58%，保证 Token 指标在深色文字翻转档达到 4.5:1；其他档位及状态色不因此改写。验收同时检查实际混合背景和相邻档位区分，不能只测独立的静态色值。

展示级衬线标题保留两端渐隐的 1.5px 金箔细线；正文、表单和表头使用无衬线，金额与指标采用等宽数字。夜间禁止歪斜圆角、铅笔偏移阴影、虚线账本分隔、印章水印和纸纹；页面不再使用丝绒纹理。导航可以使用模糊，卡片、表格、图表、菜单、弹窗和 Toast 使用实底。大表面保留微弱顶部高光，内嵌磁贴不叠加同强度高光。

首页首屏导航保留独立滚动契约：未滚动时透明，滚动后才使用 raised surface 与高程；品牌 Logo 与“RenRen AI”不得使用 tonal 背景、active pill 或金色导轨。

## 夜间表面与高程

- `surface-container-lowest`（`#161920`）：下沉区域。
- `surface-container-low`（`#21252e`）：低层表面；分段器轨道消费专用 `--studio-control-track`。
- `surface-container / surface-solid`（`#262a34`）：普通内容卡片与移动列表。
- `surface-container-high / surface-table-header`（`#2e333f`）：表头和高层内容。
- `surface-container-highest / surface-overlay`（`#373d4b`）：菜单、Modal、Drawer、命令面板和 Toast。
- `nav-surface / nav-border / nav-shadow`：导航专属轻玻璃；无滤镜或减少透明度时使用不透明 `--nav-solid: #232732`。

新组件优先使用 `--surface-container-*`、`--shape-*`、`--state-*` 和 `--elevation-*`，普通内容依靠 tonal surface 与细描边表达层级，只有重点卡片和浮层使用明显高程。

正式样式直接整合进语义令牌与所属组件，不导入设计预览覆盖文件，不使用 `data-concept` 等临时选择器。Vue scoped 样式中的全局祖先应写为完整 `:global(html.dark .component-selector)`，不能使用会丢失后续选择器的 `:global(html.dark) .component-selector`。

## 控件与交互状态约定

- 高度刻度（全控件族统一，对齐 ConsoleButton）：`sm=32 / md=40 / lg=48`；小屏（<1024px）或主指针为粗指针时，主要按钮、页签、分段器、筛选和分页控件的命中区域至少 44×44px。细指针桌面再收缩到对应档位。开关保留紧凑轨道及居中的 44px 扩展命中区域；下划线输入件固定 `h-11`，不得响应式收缩。
- 悬停反馈统一使用 `--state-hover-layer`；骨架屏底色仍用 `--surface-muted`（非交互语义，勿混用）。
- 按压反馈：primary 走 `--accent-active`，secondary/ghost 走 `--state-press-layer`，danger/stamp 用透明度阶梯（hover 0.9 / active 0.8）。
- 大数字一律使用 `.display-number`（日间浓墨 700、夜间雅致 500 由令牌控制），不得手写 font-bold 替代。
- 小节标题使用 common/SectionHeading（`.section-heading`）；统计瓦片使用 common/StatTileGrid；骨架屏使用 common/SkeletonBlock。禁止在视图内重造这三类 markup。
- 空态容器约定：页面级整块空态（替代整页内容）裸用 EmptyState；若空态占据卡片网格中的一个内容位或替代某张卡片的内部，则用与该内容位同宽的表面容器（ConsoleCard `:padded=false` 或等价 bordered 容器）包裹，保证空态不悬浮在页面底色上。
- 卡片栅格断点约定：新增页面的多卡网格默认 `lg` 起两栏、`xl` 起三栏；仪表盘与钱包现存的两栏 `xl` 布局为已知例外，随各自改版收敛，不为对齐而单独返工。
- 夜间页签使用凹槽与浮动滑块，桌面至少 40px，小屏至少 44px。辅助类型标签采用中性色；成功、警告、危险状态保留语义色。手机仪表盘充值按钮为 `#51452c` 底色与 `#f6dca2` 文字。

## 响应式导航

桌面（≥1024px）保留侧栏原生 `13.75rem / 4rem` 展开收起、Studio 浮动外壳和受控的宽屏 rem 缩放。文字放大导致顶栏可用空间不足时，同样使用完整导航抽屉，不能裁掉入口。小屏使用 52px 顶栏，保留菜单、Logo、搜索、主题和账户，语言选择移入抽屉底部并向上展开。

`ConsoleMobileNavDrawer` 复用 `getVisibleTopNavItems` 与 `getAccessibleConsoleNavGroups`，保留权限、禁用提示、工单角标与外链，不复制导航规则。抽屉从左侧展开，宽度为 `min(22rem, 100vw - 24px)`；与业务弹窗共用 `ConsoleModal` / `modalStack`，锁定页面和控制台内部滚动，约束焦点，支持 Escape、焦点归还、路由变化及跨桌面断点关闭。

抽屉中的图标与主标签作为一组，禁用说明在空间不足时换行，不挤压或覆盖主标签；放大文字回归须测量实际文字区域。禁用项和无内部路由的入口不设置 `aria-current`。

仪表盘保持 KPI → 余额/热力图 → 其他分析的 DOM 顺序，正常手机字号下第五项 KPI 跨两列。文字放大或卡片可用宽度不足时，KPI、日历分析区和模型分布通过容器查询自然堆叠，核心数值不省略。标准夹具下 390×844 首屏验收要求余额卡起点不超过 600px，核心数值完整可见。复杂矩阵、日历和长分段选项保留容器内横滚，不裁掉字段或操作。视觉隐藏的绝对定位标签必须有局部定位父节点，避免绕过控制台内部滚动边界。

## 主题切换生命周期

`useTheme` 是唯一入口；`preference` 只读。程序调用 `setThemePreference(value)` 直接更新，人工入口显式调用 `setThemePreference(value, { animate: true })`，返回 `Promise<void>`。顶栏、公开页与设置面板使用同一调度器。

支持 View Transition 时，根快照执行 260ms 透明度交融，图标执行 180ms 轻旋转与淡入；不对全体 DOM 添加颜色过渡。首次加载、系统自动主题变化、跨标签存储同步、实际明暗不变、减少动态效果和不支持 API 时直接应用。快速操作取消前次动画，最后选择生效，不累积队列。

新快照前等待 Vue 更新；Canvas 在 post-flush watcher 中读取新令牌并同步绘制，ECharts 使用主题专用增量更新且关闭该次图表动画，清除切回日间时的光晕残留。主题图片通过 `data-theme-asset` 标记并等待解码，避免冷缓存抓到空白图。数据更新沿用原有图表更新语义。首屏 anti-FOUC、根主题属性和浏览器 `theme-color` 同源，夜间浏览器主题色为 `#1C1F26`。

图片解码期间若被新的人工选择或存储更新取代，只取消版本已过期的快照等待；当前有效请求的真实解码错误仍须通过 Promise 暴露。不能全局吞掉图片错误，也不能因旧图片被取消而让新主题失败。

## 负载色阶

有上限的资源读数（仪表盘系统状态的 CPU、内存、磁盘和 CPU 负载格，以及 Root 运维页的资源仪表）统一用 `--load-{idle,normal,notice,warning,danger}` 五档色阶，档位由 `src/utils/loadTone.ts` 一处计算，同一读数和判定在两个页面同色。空闲、正常、关注三档按占比划分，分界为 30% / 60%；警告、危险两档只来自服务端阈值判定（资源 `state` 为 `warning` / `critical`），与标题胶囊同源，前端不按固定比例告警。当前资源检查超过阈值只产出 `warning`，`critical` 是契约保留值，所以资源格子目前最高到警告档；危险档在卡片上只用于成功率、读数浮层里超过核数的负载，以及标题胶囊的严重和读取失败。宿主机负载格没有健康检查，口径也可能与容器 CPU 不同，所以最高只到关注档，超过核数只在标明宿主机的读数浮层里用危险文字色标出；读不到核数时负载格按未知的虚线样式显示，数值只在浮层里。没有读数时 CPU 弧、磁盘环、负载格和内存十段都画成虚线，不与 0% 混同。管理员把某项阈值设为 0 时，该资源 `state` 为 `unchecked`（Root 的检查状态为 `disabled`），读数照常显示但不进色阶：图形用 `--load-unchecked`，仪表盘数字用 `--load-unchecked-text`。Root 仪表环内数字除严重档用危险文字色外都保持正文色。日间未判定色是冷石板灰，因为暖灰 `--text-tertiary` 离正常档只有 ΔE 4.7；夜间未判定图形固定为原有中性灰 `#b8c1d3`，不随辅助文字灰阶改变。资源监控整体关闭（`state` 为 `disabled`）时格子显示“未启用”，不与未采样的“--”混同。API 成功率复用其中的正常、警告、危险三档。系统状态卡标题胶囊的正常、告警、严重和读取失败也用对应档位的 `--load-*` 颜色，与下方格子同色；加载中仍用 `--status-info`。警告、危险两档不只靠颜色表达：格子的悬停文字和读屏文字带“超过告警阈值”“严重”。每档有图形色和 `-text` 文字色：图形色对磁贴底不低于 3:1，文字色对磁贴底和 14% 徽章底不低于 4.5:1。色相从青经绿、黄、橙单调走到红，彩度逐档升高，危险档在两套主题里都最醒目。正常档与 `--status-success` 同源，日间危险档沿用 `--status-danger`，其余取值见 `tokens.css` 的注释。

负载档位只表达占上限的比例和服务端阈值判定，不要用来给方向、类别或品牌强调着色，各档文字色也要和金额数字用的 `--accent-text` 明显区分（夜间关注档因此避开星辉金）。没有上限的读数（如流量）保持中性文字色，数值与对应折线同色，用箭头和虚实线区分方向。同一档位不叠加透明度阶梯区分时间窗口，透明度会让相邻档位混淆。夜间各主仪表的已填充部分统一带同色 3px 微光，日间不发光。色阶边界按卡片划分：带负载色阶的卡片（仪表盘系统状态卡、Root 资源卡）里的判定颜色跟色阶走；Root 状态区的整体状态标签、检查项圆点和存储卡仍用全局 `--status-*`，因为 `StatusChip` 全站共用。两套颜色在日间警告、夜间警告和危险上不同，统一需要改全局状态色。Root 资源趋势线用 `--trend-{cpu,memory,disk}`，取色阶以外的蓝、紫、玫瑰，离五档和中性灰 ΔE ≥10，折线不会读成某个档位。`tests/visual/system-status.spec.ts` 在两套主题下复核对比度、相邻档位色差、未判定色与各档的色差、胶囊与同档格子同色，以及与 `--accent-text` 的色差；`tests/visual/ops-monitor.spec.ts` 复核趋势线与各档和中性灰的色差。调整色阶须保持这些阈值。

## 品牌与媒体例外

外部供应商 Logo 保留官方品牌色；活动 Banner、游戏和农场插画保留业务强调色。为保证媒体文字可读性，可以使用 `--media-scrim-strong/medium/soft` 黑色遮罩，不得在组件内复制固定暗色渐变。除供应商品牌色外，Canvas 与 ECharts 的结构色必须从 CSS 语义令牌解析。

2FA 二维码容器是唯一的功能性纯白例外：扫码识别要求固定 `#ffffff` 底色，不随主题切换（SecurityAccessView）。

## 持久化

- 主题偏好键：`ren2hub_theme_mode`
- 旧主题键：`renren_theme_mode`，只在迁移和首屏兼容读取时使用
- 语言偏好键：`ren2hub_locale`
- 旧语言键：`renren_locale`，只在首次读取时迁移，随后删除

首屏内联脚本只负责 anti-FOUC：优先读取 `ren2hub_theme_mode`，没有新键时兼容读取旧键，并且只接受 `light`、`dark`、`auto`。

## 字体

字体通过 `src/assets/fonts/` 中的本地 WOFF2 文件自托管，不依赖运行时外部样式加载：

- Inter：正文与界面拉丁字符
- Noto Sans SC：中文无衬线字符
- Noto Serif SC：展示标题与两套主题中的克制衬线表达
- JetBrains Mono：代码、数字和等宽信息

系统字体仅作为本地字体不可用时的回退。

## 可访问性

浅色和深色主题都必须保留可见焦点、状态文本和足够的文本对比度。夜间主卡、表头仍用 `--text-tertiary`；更亮的菜单、弹窗与提示表面继承 `--text-tertiary-overlay`，避免次级说明在高层表面低于 4.5:1。新增界面字号使用等价 rem，保留用户文字放大；图标按钮可以保持 44px 触控尺寸，不能用固定高度裁切文本。`prefers-reduced-motion` 下关闭非必要动画；Canvas 仍绘制稳定静态帧，并继续限制 DPR、在组件卸载时释放资源。

## 视觉回归

视觉回归仅在本地手动运行，不接入 GitHub Actions：

- `bun run test:visual`

该命令审计当前页面的行为与布局；折扣卡的两种主题保留已审阅像素基线，其余页面不做历史截图逐像素比较。默认只在失败时保存截图；设置 `PLAYWRIGHT_CAPTURE_SCREENSHOTS=true` 可保留本次页面截图，输出位于 `output/playwright/`。测试环境使用 `Asia/Shanghai`、符合真实 wire contract 的认证与 API 夹具、本地字体和 `reduced-motion`；需要固定时间的用例由浏览器 fixture 控制。审计覆盖运行错误、DOM 状态、响应式布局、横向溢出、交互遮挡、主题与字体契约。服务启动和 Go 产物验证方式见[路由约定](../ROUTING.md)。

# 主题与样式结构

新前端只维护一套语义令牌和两种视觉主题：浅色 `Desert Ledger`、深色 `One Night`。

## 文件职责

- `src/styles/tokens.css`：颜色、阴影、状态、品牌和 Canvas 共享令牌。
- `src/styles/base.css`：Tailwind 层、文档默认值、焦点和选区规则。
- `src/styles/home.css`：首页专属视觉，选择器限制在 `.landing-shell`；仅首页滚动状态允许作用于 `html.hero-scrollbar-hidden`。
- `src/styles/console.css`：Console 与 Lab 的滚动区域，由两个懒加载布局按需引入。
- `src/styles/index.css`：全局汇总入口，只引入令牌、基础规则和首页样式，不定义新令牌。

组件必须使用 `--page-background`、`--surface-*`、`--text-*`、`--border-*`、`--accent`、`--signal`、`--support` 和 `--status-*` 等语义变量，不得在业务组件中复制主题色板。

## 持久化

- 主题偏好键：`renren_theme_mode`
- 语言偏好键：`ren2hub_locale`
- 旧语言键 `renren_locale` 只在首次读取时迁移，随后删除。

主题脚本只负责首屏 anti-FOUC。字体使用系统字体栈，不通过运行时 `onload` 注入外部样式。

## 可访问性

浅色和深色主题都必须保留可见焦点、状态文本和足够的文本对比度。`prefers-reduced-motion` 下关闭非必要动画；Canvas 仍绘制稳定静态帧，并继续遵守 DPR 上限和完整释放规则。

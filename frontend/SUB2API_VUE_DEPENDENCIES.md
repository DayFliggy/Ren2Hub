# Sub2API Vue 前端依赖清单

来源：`reference-projects/sub2api/frontend/package.json`。

该参考前端使用 Vue 3、TypeScript、Vite 5 和 pnpm。以下版本为 `package.json` 中声明的版本范围，不是锁文件解析后的最终安装版本。

## 1. 运行时依赖

| 依赖 | 声明版本 | 用途 |
| --- | --- | --- |
| `vue` | `^3.4.0` | Vue 3 核心框架 |
| `vue-router` | `^4.2.5` | SPA 路由 |
| `pinia` | `^2.1.7` | 全局状态管理 |
| `vue-i18n` | `^9.14.5` | 国际化 |
| `@vueuse/core` | `^10.7.0` | Vue Composition API 工具集 |
| `@tanstack/vue-virtual` | `^3.13.23` | 大列表虚拟滚动 |
| `vue-chartjs` | `^5.3.0` | Chart.js 的 Vue 组件封装 |
| `vue-draggable-plus` | `^0.6.1` | Vue 拖拽排序 |
| `axios` | `^1.16.0` | HTTP 客户端 |
| `chart.js` | `^4.4.1` | 图表引擎 |
| `dompurify` | `^3.3.1` | HTML 内容净化 |
| `driver.js` | `^1.4.0` | 新手引导与页面聚焦 |
| `file-saver` | `^2.0.5` | 浏览器文件下载 |
| `marked` | `^17.0.1` | Markdown 解析 |
| `qrcode` | `^1.5.4` | 二维码生成 |
| `xlsx` | `^0.18.5` | Excel 文件读取与导出 |
| `@lobehub/icons` | `^4.0.2` | AI/模型品牌图标 |
| `@airwallex/components-sdk` | `^1.30.2` | Airwallex 支付组件 SDK |
| `@stripe/stripe-js` | `^9.0.1` | Stripe 浏览器 SDK |

## 2. 开发与构建依赖

| 依赖 | 声明版本 | 用途 |
| --- | --- | --- |
| `typescript` | `~5.6.0` | TypeScript 编译与类型系统 |
| `vite` | `^5.0.10` | 开发服务器与生产构建 |
| `@vitejs/plugin-vue` | `^5.2.3` | Vite Vue 单文件组件支持 |
| `vue-tsc` | `^2.2.0` | Vue SFC 类型检查 |
| `eslint` | `^8.57.0` | 代码检查 |
| `eslint-plugin-vue` | `^9.25.0` | Vue ESLint 规则 |
| `@typescript-eslint/eslint-plugin` | `^7.18.0` | TypeScript ESLint 规则 |
| `@typescript-eslint/parser` | `^7.18.0` | TypeScript ESLint 解析器 |
| `vite-plugin-checker` | `^0.9.1` | 开发期类型和 Lint 检查 |
| `vitest` | `^2.1.9` | 单元测试框架 |
| `@vitest/coverage-v8` | `^2.1.9` | V8 测试覆盖率 |
| `@vue/test-utils` | `^2.4.6` | Vue 组件测试工具 |
| `jsdom` | `^24.1.3` | 浏览器 DOM 测试环境 |
| `tailwindcss` | `^3.4.0` | CSS 工具类框架 |
| `postcss` | `^8.4.32` | CSS 转换管线 |
| `autoprefixer` | `^10.4.16` | CSS 浏览器前缀 |
| `@types/node` | `^20.10.5` | Node.js 类型声明 |
| `@types/dompurify` | `^3.0.5` | DOMPurify 类型声明 |
| `@types/file-saver` | `^2.0.7` | FileSaver 类型声明 |
| `@types/mdx` | `^2.0.13` | MDX 类型声明 |
| `@types/qrcode` | `^1.5.6` | QRCode 类型声明 |

## 3. pnpm 覆盖项

| 包 | 覆盖版本 |
| --- | --- |
| `js-cookie` | `3.0.7` |
| `form-data@<4.0.6` | `>=4.0.6` |

## 4. 可复用的 Vue 技术组合

若 Ren2Hub 后续采用 Vue 重构，可直接参考的基础组合是：

- 应用框架：`vue` + `typescript`。
- 路由与状态：`vue-router` + `pinia`。
- 数据请求：`axios`；若需要服务端缓存，可另行评估 Vue Query。
- 国际化：`vue-i18n`。
- 工具与交互：`@vueuse/core`、`@tanstack/vue-virtual`、`vue-draggable-plus`。
- 构建与质量：`vite`、`@vitejs/plugin-vue`、`vue-tsc`、ESLint、Vitest。
- 样式：Tailwind CSS 3 + PostCSS + Autoprefixer。

支付、图表、Excel、Markdown、二维码等依赖应按 Ren2Hub 实际功能逐项迁移，不应仅因参考项目已安装而一次性引入。

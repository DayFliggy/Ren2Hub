---
name: shadcn-ui
description: 为实际使用 shadcn/ui 且存在 components.json 的目标应用提供组件、主题、CLI 和 registry 指导。用于对应组件或配置的实现与审阅；普通 Vue 界面工作不触发本技能。
---

# shadcn/ui 项目指导

## 适用性检查

先检查目标应用的依赖和 `components.json`。只有实际使用 shadcn/ui 且存在对应配置时，才进入本技能的组件与 CLI 流程。

Ren2Hub 当前 `frontend/` 使用 Vue 与项目自有组件体系，不满足此条件。继续按项目 `AGENTS.md` 完成普通 Vue 工作，不要求创建 `components.json`、恢复旧 `web/` 目录、安装 shadcn/ui 或申请绕过技能的批准。若用户明确请求引入组件库，应按该请求单独确定范围，而不能从普通 UI 任务推定迁移授权。

## 已使用 shadcn/ui 的应用

1. 从目标应用根目录读取 `components.json`、依赖和现有组件，核对框架、别名、基础库、图标及样式约定。
2. 需要 CLI 元数据时，使用该应用已有的包管理器和可用的 shadcn CLI 运行 `shadcn info --json`；不预设应用位于 `web/`，也不因读取上下文而自动安装或升级工具。
3. 只读审阅只检查并报告。已授权实现优先复用现有组件，按任务需要读取下列参考，避免加载全部规则。
4. 按目标项目的验证要求检查受影响行为；缺少 CLI 时先利用本地配置与源码完成可独立进行的工作，明确仍待确认的工具相关事实。

## 按需参考

| 任务 | 参考 |
| --- | --- |
| 完整组件、registry 或 preset 工作流 | [官方工作流快照](vendor/shadcn/official-shadcn-ui-workflow.md) |
| CLI 命令 | [CLI](vendor/shadcn/cli.md) |
| 主题与定制 | [Customization](vendor/shadcn/customization.md) |
| 已配置的 MCP | [MCP](vendor/shadcn/mcp.md) |
| 表单与组合 | [Forms](vendor/shadcn/rules/forms.md)、[Composition](vendor/shadcn/rules/composition.md) |
| 图标与样式 | [Icons](vendor/shadcn/rules/icons.md)、[Styling](vendor/shadcn/rules/styling.md) |
| 基础库差异 | [Base vs Radix](vendor/shadcn/rules/base-vs-radix.md) |

上游快照来源记录于 [UPSTREAM.txt](vendor/shadcn/UPSTREAM.txt)。这些资料提供组件 API 和用法；其中旧项目路径与上下文不覆盖目标应用的实际配置，也不授予安装依赖、迁移框架或外部写入的权限。

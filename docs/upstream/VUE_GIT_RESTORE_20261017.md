# Vue 前端原样 Git 还原（2026-10-17）

用户在查看恢复页面后明确要求直接还原 NewAPI 核心回归前的前端，并确认“先还原，后端和前端对接的接口先不进行改造”。本次仅恢复历史源码，不再对旧页面重新设计或对接当前后端。

- 历史提交：`9a63062227c31ba54a6346e07ee8b4fd8802f22f`，2026-10-16 13:04:14 UTC，即北京时间 2026-10-16 21:04:14。
- 恢复范围：整个 `frontend/`，包括页面、组件、样式、静态资源、路由、API 调用、测试、构建配置、package.json 与 bun.lock。
- 精确树身份：`71429836231228a05260ecf4b9169f4172834b58`。该目录也与核心回归提交 `d7095730e` 的直接父提交 `ab9fa349c75b39b53cf8f783c5c8be44e8ca3a9f` 完全相同。
- 执行方式：从当前 `d7663846550cd02e6f101252c7580efe6d43badd` 创建新的还原提交，使用 `git restore --source=9a63062227c31ba54a6346e07ee8b4fd8802f22f --staged --worktree -- frontend`；不重写 Git 历史。
- 后端、数据库、部署配置及目录外的 Electron 项目不在恢复范围。

共还原 450 个前端文件变更。检查完成后，工作目录和索引中的 frontend 均与历史树完全一致，没有保留后续重建页面、原生接口适配、字体补集或视觉审计修补。原审计报告仍是其对应版本的历史证据，不适用于本次原样还原。

本地验证通过：定向 21 项、完整 210 文件 / 1,238 项单测、typecheck、lint、format:check、生产 build。构建保留既有大 chunk 提示，未为此修改历史源码。单测使用历史 mock 合同，不能证明与当前后端兼容。

已知接口边界保留待处理：旧登录/用户及 Token 解析依赖 available_quota、reserved_quota、quota_version 等旧字段；首页 `/api/home/metrics`、Key model-manifest/client-config、工单与充值的 `/api/next` 路由均不属于当前后端合同。本次按用户指令不改这些调用，也不新增后端兼容接口。未执行真实登录冒烟、完整浏览器、应用候选或正式部署，不宣称原样前端可以直接搭配当前后端上线。

独立副本 `/home/work/ren2hub-git-frontend-restore-20261017`；证据 `/home/work/ren2hub-git-frontend-evidence-20261017`。`restoration.json` 记录历史树与恢复范围，`validation.json` 记录实际命令及退出码；提交形成前 candidate_sha 保持 null。源码发布与仍运行的正式版本由根工作区分别记录。

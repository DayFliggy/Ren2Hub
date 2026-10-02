# 上游同步空库冒烟

使用已构建的本地可执行文件运行：

```powershell
go build -o "$env:TEMP\ren2hub-upstream-smoke.exe" .
node tests/upstream-sync/smoke.mjs "$env:TEMP\ren2hub-upstream-smoke.exe"
```

脚本在新建临时目录初始化 SQLite，使用随机测试密码和密钥，清除子进程的外部 SQL/Redis 连接配置，只连接本地模拟供应商。结束停止自己启动的进程；保留临时目录中的测试数据库与日志供失败定位，不修改或删除任何已有数据库。它不会读取正式 `.env`，不会调用付费供应商，也不发布。

覆盖空库启动/初始化和浏览器会话登录 API，以及以下真实 HTTP 合同：

- PAT 规范路由与旧别名、一次明文、scope 目录、改名、绑定目标权限的 PATCH Proof、越权拒绝和撤销。
- 模型能力卡 CRUD、同源 manifest/旧名称列表/`/v1/models`、Codex/Pi 导出；可信服务器地址不能被转发头替换，配置不包含真实模型 Token。
- 单凭据 Codex 空缓存返回 unknown、自动刷新默认关闭；GET 不触发上游请求。
- Trace/Thread 诊断持久化、脱敏投影、仅有 `ops:read` 的 PAT 无权读取渠道/凭据内部 ID。
- Chat、Claude 工具和 Responses custom tool 共三次模拟上游发送。

结算断言等候已完成的钱包投影，必须精确扣除三次固定价格且预留清零，不能把 HTTP 200 或尚在结算当作完成。脚本中的登录是浏览器会话 API，真实浏览器交互另由 Playwright 覆盖。

本脚本使用内置 Node 模块，不依赖新 npm 包；真实协议 SDK 验证仍使用 `tests/gateway-sdk/`。三数据库、权限与并发回归见 `docs/upstream/SYNC_LEDGER.md`，不能把本脚本的 SQLite 结果代替 MySQL/PostgreSQL 验证。

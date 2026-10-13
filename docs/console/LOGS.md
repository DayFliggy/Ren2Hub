# 用量日志查询与导出

普通用户的日志列表、统计、仪表盘用量和专用导出共享服务端时间跨度规则。`log_setting.max_range_days` 默认 365，允许 1–36500 的整数；未指定开始时间时取结束时间之前的该跨度。负时间、开始晚于结束和超过跨度均明确报错。未来结束时间仅在跨度校验时按当前时间计算，便于选择当天结束；列表保留请求的结束条件，导出把结束条件固定为打开游标前的当前时间。管理员全局列表不受此普通用户查询限制，但两种专用导出均校验跨度。

`log_setting.user_export_enabled` 默认 true，控制普通用户导出；管理员仍可导出。两个设置由系统设置保存，公开 `/api/status` 的 `log_max_range_days`、`log_export_enabled` 向 Vue 提供页面能力。仪表盘不再把超长范围拆成多个请求绕过服务端规则。

## 接口与权限

| 接口 | 访问与数据边界 |
| --- | --- |
| `GET /api/log/self/export` | 当前用户业务日志；PAT 要求 `logs:read`，导出开关对普通用户生效 |
| `GET /api/log/export` | 管理员全局日志；PAT 要求 `ops:read`，仍需管理员角色 |

查询条件沿用列表的 `type`、`model_name`、`token_name`、`group`、`request_id`、`upstream_request_id`、`keyword`、`start_timestamp`、`end_timestamp`。管理员全局接口另支持 `username`、`channel`。时间为 Unix 秒，关键字最长 128；模型名与历史 group 使用同一实际查询过滤器。用户接口不接受目标用户切换，不返回内部渠道 ID、用户名、路由调试、管理日志或内部证据字段；继续使用列表的 DTO 白名单和脱敏规则。`quota` 保持精确整数字符串，客户端不能先转为 JavaScript Number。

接口返回 `application/x-ndjson`，每行一条日志，最后一条为成功 JSON 封套，其中 `success=true`、`data.count=N`。客户端须确认成功终态、记录数和字段合同之后生成文件。空结果仍有 count=0 的成功终态；业务失败、游标失败、JSON 无效、终态丢失或计数不符均不能下载部分结果。`Cache-Control: no-store` 与 `X-Accel-Buffering: no` 用于避免缓存与代理缓冲；普通 JSON API 错误仍按共享业务错误合同处理。

同一用户同时只允许一个专用导出；当前进程内互斥在请求取消、失败或结束后释放。这依赖项目现行单网关实例合同，不提供多实例分布式锁。普通用户以 ID 降序、管理员以时间和 ID 降序读取；固定结束时间只排除时间戳晚于该条件的记录，不提供数据库事务快照。并发写入的回填记录或同一 Unix 秒内的记录仍可能出现。

## Vue 行为与边界

用量日志页保留 CSV、Excel 和 JSON，导出日期独立于列表日期。导出弹窗使用内联日历，日期面板随弹窗正文滚动，支持小屏幕与键盘选择。列名依据当前语言生成，CSV/Excel 防止单元格公式注入；文件字节保留精确金额字符串与历史分组。CSV 和现有 HTML Excel 文件导入电子表格时仍可能被应用自动转换为数值；尚未验证真实 Excel 对 15 位以上数字的导入精度，需按文本列导入。

页面使用一次专用导出请求，不再循环读取列表分页或截断为 10000 行；关闭弹窗或取消会中止请求，失败保留弹窗和日期以便处理。浏览器请求期限为 120 秒，响应 Blob、文本和解析后的行均占用浏览器内存。服务端使用游标不等于浏览器可以处理无限数据；很大范围仍可能受请求期限、代理期限或设备内存限制，应缩小日期范围。该规则针对用量日志页，审计日志与订单页保留各自的既有导出上限。

## 复验入口

```sh
go test -timeout 60s ./controller ./model ./setting/operation_setting ./service -run 'TestLogExport|TestStreamLogs|TestValidateLogRange|TestGetUserLogs' -count=1
go test -timeout 60s ./router -run '^TestLogExportRoutesRequireOwnerScopeAndAdminRole$' -count=1
go test -timeout 60s ./model -run '^TestLogExportFiltersAcrossDatabases$' -count=1
```

三库用例使用 `TEST_MYSQL_DSN`、`TEST_POSTGRES_DSN`，须指向隔离可丢弃数据库；未提供 DSN 而跳过不算三库通过。前端行为由 `src/api/__tests__/logExport.spec.ts`、共享 HTTP transport 回归以及 `tests/visual/log-export.spec.ts` 验证，按[前端说明](../../frontend/README.md#验证)执行。API fixture 浏览器测试与真实数据库行为分别提供证据，不替代运行镜像和正式部署验证。ClickHouse 导出分支尚无真实环境复验。

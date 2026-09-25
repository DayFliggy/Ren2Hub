# Claude 消息级 output_config 增量吸收

记录日期：2026-09-25。本次在原固定目标 `d04c118c8803f49e0c9bab74dcf5b5efeab9464a` 之上，吸收唯一新增提交 `c2b7a9a9e0b548c2051a949fceabb59029adcb49`：`fix(claude): preserve per-message output_config in Claude messages (#7561)`。原 rc.40 的 260 条提交目录及其历史证据保持原范围，本记录单独覆盖这 1 条增量。

## 实现与回归

`relaykit/dto/claude.go` 直接采用上游 `ClaudeMessage.OutputConfig json.RawMessage` 字段及 `omitempty`。此前消息级配置在 JSON 解析时丢失，现在会随原生 Claude 请求转发；请求级配置继续独立保留。没有新增解析、默认值或 effort 重写逻辑。

上游适配器测试接入本地现有 `relay/channel/claude/relay_conversion_test.go`。除原有断言外，还验证请求级 `medium` 与消息级 `high` 同时保留，system 角色与空 content 数组保持不变。独立模块 `relaykit/dto/claude_message_test.go` 验证字段缺省、effort-only 消息，以及不透明配置中的大整数、零值和 false 不丢失。

## 本地验证

运行环境为 Windows，`GOMAXPROCS=2`；Go 测试均使用 `-p 2 -timeout 60s -count=1`。日志位于忽略目录 `.local-tests/claude-output-config-c2b7a9a9e/`。

| 检查 | 结果 | 日志 |
| --- | --- | --- |
| 修改生产字段前执行新适配器回归 | 按预期失败：消息级配置实际为 nil，预期为 high | `red.log` |
| Claude 整包测试 | 22 个顶层测试、41 个测试/子测试事件通过，无跳过 | `claude-tests.jsonl` |
| RelayKit 独立全包测试（GOWORK=off） | 278 个顶层测试、784 个测试/子测试事件通过，9 个测试包通过，无跳过 | `relaykit-tests.jsonl` |
| DTO 最终断言调整后复验 | 48 个顶层测试、94 个测试/子测试事件通过；不与全包重复累加 | `relaykit-dto-final.jsonl` |
| Claude go vet | 通过 | `claude-vet.log` |
| 根模块 go build ./... | 通过 | `root-build.log` |
| RelayKit GOWORK=off go vet / go build ./... | 均通过 | `relaykit-vet.log`、`relaykit-build.log` |

测试使用本地合成请求，不调用商业供应商。该记录不代表常驻镜像已更新，也不把 DTO/适配器验证表述为实际供应商验收；服务器固定提交复验由根工作区记录。

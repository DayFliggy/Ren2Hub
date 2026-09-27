# 账务 API

金额采用 `common.Quota`（有符号 int64），数据库列为 BIGINT。500000 quota = 1 USD。管理、钱包、Token、奖励、统计、事件中的 quota 字段均为十进制整数字符串，例如 `"9007199254740993"`；禁止先转 JavaScript Number。无限 Token 的 `available_quota` 为 `null`，仍返回预留和消费。价格、精确小计、舍入差额为十进制字符串。时间、数量、版本与记录 ID 沿用整数协议。OpenAI 兼容账单展示接口的协议金额不参与扣款。

响应沿用 `{ "success": true, "data": ... }`。用户端点依据认证用户过滤，`token_id` 仅进一步缩小范围。Root 权限由服务端检查，前端能力开关只控制入口。

| 方法与路径 | 权限 | 请求与结果 |
| --- | --- | --- |
| GET `/api/billing/self/reservations` | 登录用户 | 当前预留，支持 offset、limit、token_id |
| GET `/api/billing/self/events` | 登录用户 | 不可变流水，支持 offset、limit、token_id |
| GET `/api/billing/reconciliations` | Root | 默认待处理列表，支持 status、offset、limit |
| GET `/api/billing/reconciliations/:id` | Root | recovery、quote、usage、charge、events、attempts、audit、upstream_cost |
| POST `/api/billing/reconciliations/:id/cost` | Root | 登记已结束请求的尝试成本；不可变、幂等，不修改钱包 |
| POST `/api/billing/reconciliations/:id/retry` | Root | 重放已保存的处理意图 |
| POST `/api/billing/reconciliations/:id/resolve` | Root | 人工结算或释放 |
| GET `/api/billing/metrics` | Root | 待恢复、对账、预留、等待、冲突、恢复失败、欠费事件、价格版本与报价尝试数、按作用域和来源统计 |
| GET `/api/billing/prices` | Root | 各 `(scope_kind, channel_id, model)` 当前活动价格版本 |
| POST `/api/billing/prices` | Root | 发布新版本，旧版本不可修改 |
| POST `/api/billing/prices/preview` | Root | 冻结草稿与用量的只读计算，不发布、不预留 |
| GET `/api/billing/prices/seed` | Root | 当前合并参考目录、待配置模型与来源哈希 |
| GET `/api/billing/prices/automatic` | Root | 自动配置、Sub2API/AxonHub 两个活动来源状态与当前模型来源 |
| PUT `/api/billing/prices/automatic` | Root | `{enabled,check_interval_minutes}`，周期 5–1440 分钟，默认 30 |
| POST `/api/billing/prices/automatic/sync` | Root | 创建或返回同类正在执行的系统任务；结果包含 checked、changed、published、deactivated、unconfigured_count、unconfigured、unconfigured_details 及 errors |
| POST `/api/billing/prices/automatic/restore` | Root | `{model,expected_version}` 恢复全局目录价；冲突 409，不覆盖人工渠道价格 |
| GET `/api/billing/prices/automatic/snapshots` | Root | `source` 可选、`limit` 默认 20，返回不可变归档元数据数组 |
| GET `/api/billing/prices/automatic/snapshots/:id` | Root | 元数据与 document 原文；`?download=1` 返回经 SHA 校验的 JSON 附件 |
| GET `/api/pricing` | 公开/当前用户权限 | 模型元数据与 offers，含公开版本、条件、调整后价格，不含渠道秘密或证据正文 |

`:id` 为请求 ID，客户端必须 URL 编码。列表返回 `{ items, total }`；limit 默认 20、最大 100。预留项包含 `request_id`、`user_id`、`token_id`、`state`、`funding_amount`、`token_amount`、`version`、时间和 `manual_review_required`。

重试请求：

```json
{ "operation_id": "operator-generated-unique-id", "expected_version": 4 }
```

人工处理请求：

```json
{
  "operation_id": "operator-generated-unique-id",
  "expected_version": 4,
  "action": "settle",
  "quota": "127",
  "reason": "已核对供应商用量，按冻结报价复算"
}
```

`release` 必须提交 quota `"0"`。`settle` 接受 0 至 2147483647 quota。理由必填。同一操作 ID、同一内容重试返回原结果；改内容、旧版本或已结束记录的新操作返回 409。无效输入 400、无权限 403、记录不存在 404；数据库异常 500，指标查询异常 503，不能显示为零。

资金事务成功后，即使日志库不可用也不会再次扣款；记录保留待投递状态，稍后用 retry 完成投递。详情分别保存每次上游尝试的报价、哈希、渠道和时间；不包含请求正文、原始请求头或密钥。

发布价格请求示例：

```json
{
  "expected_version": 0,
  "price": {
    "scope_kind": "global_model",
    "channel_id": 0,
    "model": "example-text-model",
    "mode": "unit_price",
    "provenance": {
      "kind": "retail",
      "retail_reason": "经运营审阅的本站零售价"
    },
    "prices": [
      { "quantity": "p", "unit": "per_million_tokens", "price": "2.5" },
      { "quantity": "c", "unit": "per_million_tokens", "price": "10" },
      { "quantity": "cr", "unit": "per_million_tokens", "price": "0.25" }
    ]
  }
}
```

首次发布某作用域时 expected_version 为 0；后续填写该作用域当前版本 ID。`channel_model` 要求正整数 channel_id，`global_model` 的 channel_id 为 0。请求查价先精确渠道价格，再使用明确发布的全局价格；渠道价格报价系数固定为字符串 `"1"`。发布后已有尝试继续使用原报价。价格可选 `usage_scope: "embedding"`，随价格和 Quote 冻结；编辑、导入与导出必须保留，省略不改变旧哈希。带此限制的价格只接受 embedding 请求与输入用量，不接受输出计费项。结构化阶梯、生效窗口、预览、公开价格与审计成本字段见[计价接口](../../pkg/billing/API.md)。原 ModelRatio、ModelPrice、媒体倍率、ToolPrice、ExposeRatioEnabled、billing_setting 和 auto_pricing 写入入口退休，通用 option 接口拒绝这些配置。QuotaPerUnit 固定为 500000，不可修改。

同步任务通过 `GET /api/system-task/:id` 查询。页面加载或刷新时使用 `GET /api/system-task/list?type=billing_pricing_sync&limit=1` 回载最近的定时或手动任务，并继续轮询运行中任务。`unconfigured_details` 形状为 `[{model,reasons:[{source,reason}]}]`，与 `unconfigured` 一起最多返回 100 个模型，另有总结果体积限制；完整总数为 `unconfigured_count`。来源下载失败保留其有效快照；没有有效快照的单源不阻断其他来源。有效目录无法提供该模型完整价格时停用其全局自动价；全部来源都无有效快照时返回目录错误，不据此批量停用既有价格。自动更新只管理全局价格，人工价格保持优先。

活动来源仅为 `sub2api/axonhub`，对应角色 `primary/fallback`。`snapshot_state` 为 `bundled/current/cached/unavailable`，分别表示离线有效、检查成功、失败后使用有效缓存、没有有效缓存。来源还返回检查与更新时间、哈希、模型与拒绝数量、错误，以及 `snapshot_id/parser_version/snapshot_count`。原文归档与字段证据共同解释发布依据，不保留旧来源归属迁移。

本 API 对应安装格式 4、价格合同 3、`billing-v3`。发布立即生效，拒绝未来开始和已经到期的价格。人工发布必须选择 evidence 完整字段证据或 retail 理由；自动来源专用 source 证据由服务端绑定。物理音频 metering、服务档 support、Decimal 调整明细与公开 offers 的完整形状见[计价接口](../../pkg/billing/API.md)。

管理员设置余额必须携带操作 ID、预期账户版本，通过差额事件记账。支付、兑换、奖励业务继续维护资格和业务凭据，凭据与钱包事件使用同一事务。返利额度仍归返利权益账本管理，转入钱包时建立关联事件。

易支付报价、下单、回调和管理员补单遵循[易支付契约](EPAY.md)。不提供用户验付或后台主动查单；异常由外部网关重试通知或管理员核查补单。预期 quota 冻结于订单快照，实际 `credited_quota` 由钱包事件生成；支付金额以人民币分保存和比较，不能从展示设置反推入账额度。

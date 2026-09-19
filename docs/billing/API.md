# 账务 API

金额采用 `common.Quota`（有符号 int64），数据库列为 BIGINT。500000 quota = 1 USD。管理、钱包、Token、奖励、统计、事件中的 quota 字段均为十进制整数字符串，例如 `"9007199254740993"`；禁止先转 JavaScript Number。无限 Token 的 `available_quota` 为 `null`，仍返回预留和消费。价格、精确小计、舍入差额为十进制字符串。时间、数量、版本与记录 ID 沿用整数协议。OpenAI 兼容账单展示接口的协议金额不参与扣款。

响应沿用 `{ "success": true, "data": ... }`。用户端点依据认证用户过滤，`token_id` 仅进一步缩小范围。Root 权限由服务端检查，前端能力开关只控制入口。

| 方法与路径 | 权限 | 请求与结果 |
| --- | --- | --- |
| GET `/api/billing/self/reservations` | 登录用户 | 当前预留，支持 offset、limit、token_id |
| GET `/api/billing/self/events` | 登录用户 | 不可变流水，支持 offset、limit、token_id |
| GET `/api/billing/reconciliations` | Root | 默认待处理列表，支持 status、offset、limit |
| GET `/api/billing/reconciliations/:id` | Root | recovery、quote、usage、charge、events、attempts |
| POST `/api/billing/reconciliations/:id/retry` | Root | 重放已保存的处理意图 |
| POST `/api/billing/reconciliations/:id/resolve` | Root | 人工结算或释放 |
| GET `/api/billing/metrics` | Root | 待恢复、对账、预留、等待、冲突、恢复失败、欠费事件 |
| GET `/api/billing/prices` | Root | 当前生效价格版本 |
| POST `/api/billing/prices` | Root | 发布新版本，旧版本不可修改 |
| GET `/api/billing/prices/seed` | Root | 仓库内初始目录与待配置模型 |

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
    "model": "example-text-model",
    "mode": "unit_price",
    "prices": [
      { "quantity": "p", "unit": "per_million_tokens", "price": "2.5" },
      { "quantity": "c", "unit": "per_million_tokens", "price": "10" },
      { "quantity": "cr", "unit": "per_million_tokens", "price": "0.25" }
    ]
  }
}
```

首次发布 expected_version 为 0；后续填写当前版本 ID。发布后已有尝试继续使用原报价。原 ModelRatio、ModelPrice、媒体倍率、ToolPrice、billing_setting 和 auto_pricing 写入入口退休，通用 option 接口拒绝这些配置。QuotaPerUnit 固定为 500000，不可修改。

管理员设置余额必须携带操作 ID、预期账户版本，通过差额事件记账。支付、兑换、奖励业务继续维护资格和业务凭据，凭据与钱包事件使用同一事务。返利额度仍归返利权益账本管理，转入钱包时建立关联事件。

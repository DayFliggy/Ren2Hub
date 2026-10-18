# 钱包、定价与充值 API

管理接口通常返回 `{success,data,message?}`；原生金额及下单接口可能使用 `{message:"success",data,...}`。客户端必须按各接口合同检查业务成功，不能只看 HTTP 200。quota 使用原生 JSON number，时间与 ID 使用原生字段，不能发送旧账户版本或资金事件指纹。

| 接口 | 用途 |
| --- | --- |
| `GET /api/user/self` | 当前用户 `quota`、`used_quota`、权限与设置 |
| `GET/POST /api/token/`、`PUT /api/token/` | API Key 原生预算、模型及 IP 限制；路由由 `/routing` 子接口管理 |
| `GET /api/user/topup/info` | 当前启用支付方式、最低金额和折扣；含 GMPay 开关及资产 |
| `POST /api/user/amount` | 原生充值金额计算，结果为格式化金额字符串 |
| `GET /api/user/topup/self` | 本人原生充值记录 |
| `GET /api/user/topup`、`POST /api/user/topup/complete` | 管理员查询与人工补单 |
| `GET /api/user/affiliate`、`GET /api/user/affiliate/invitees` | 返利余额、规则及脱敏邀请名单 |
| `POST /api/user/affiliate/transfer` | 带 `operation_id` 的全额幂等转入；拒绝旧金额参数 |
| `GET /api/pricing` | 原生模型价格投影 |
| `GET/PATCH /api/option/model_pricing` | 模型配置读取、版本校验保存 |
| `POST /api/option/model_pricing/{preview,convert}` | 草稿辅助操作，不创建请求预留 |
| `GET /api/log/self`、`GET /api/log/` | 用户与管理员用量日志 |

模型、充值与管理员操作仍须满足原生角色、scope、业务校验及适用的安全验证。GMPay 与个人工单额外限定浏览器会话；管理员工单使用原生 ticket scopes，见[扩展接口](../routing/GATEWAY_API.md)。

旧 `/api/next`、`/api/billing/self/reservations`、`/api/billing/self/events` 与独立网关账务指标不是现行 API。不能根据历史 OpenAPI 文件恢复这些端点。金额计算见[定价](PRICING.md)，GMPay 响应例外和回调见[支付合同](GMPAY.md)。

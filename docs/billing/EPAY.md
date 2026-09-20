# 易支付契约

本实现使用易支付 MD5 表单协议。仅支持新订单：没有订单回填或旧结算兼容，缺少冻结快照的订单拒绝入账。其他支付渠道、退款和容器部署不属于此次改动。

## 报价与下单

登录用户调用 `POST /api/next/wallet/topup/quote`：

```json
{"amount":10,"amount_unit":"USD","payment_method":"alipay"}
```

成功响应沿用 `{"success":true,"data":...}`。`data` 包括 `pay_amount`（如 `"73.00"`）、`currency:"CNY"`、`amount_minor:"7300"`、`expected_quota:"5000000"`、`minimum_quantity`、`minimum_quota`、`min_topup` 和 `quote_fingerprint`。分值及 quota 的精确字段均为十进制字符串，时间、ID 和输入数量沿用整数协议。

服务端使用 Decimal，计算 `USD 数量 × Price × 用户组倍率 × USD 预设折扣`，最后统一四舍五入至人民币分。USD 输入乘固定 500000 得到 quota；TOKENS 输入直接为 quota，例如 750000 对应 1.5 USD 额度，按单价 7.3 应付 10.95 CNY，实际入账仍是 750000。折扣统一按 USD 预设数量匹配，TOKENS 只有恰好对应整数 USD 预设时才应用该折扣，避免小额 Token 输入误用大额折扣。全局 `MinTopUp` 与方式 `min_topup` 均按 USD 解释，取较大值换算成 quota 后比较。

`POST /api/next/wallet/topup` 和旧 `POST /api/user/pay` 共用创建流程，接收上述字段及 `quote_fingerprint`。Vue 固定提交 USD；旧调用省略单位时按原展示设置归一化，保存后不再受展示设置影响。旧接口保留 `message/data/url` 封装。服务端每次下单重新报价，提交的指纹不一致返回 `QUOTE_CHANGED`，不会保存订单；Vue 刷新报价，等待再次点击付款。输入变化时立即禁用旧报价，取消或过期响应不能恢复付款按钮。

配置接口 `GET /api/next/wallet/config` 为每个方式返回同一规则算出的有效门槛。保存配置拒绝重复、空名称、非法类型及无效门槛；网关必须是 HTTPS。报价一次 SELECT 读取已提交的配置，商户轮换应通过现有批量设置接口一次提交地址、PID、密钥，避免人为逐项保存的中间配置。新订单会固定本次读取的完整版本。

## 冻结快照与原子入账

增量迁移新增 `epay_merchant_versions` 和 `epay_orders`，不删除、重建既有账务数据。商户版本独立保存规范化网关地址、PID 和密钥，所有字段禁止 JSON 序列化，秘密 SQL 禁止输出日志。存储权限与现有数据库秘密配置边界一致，数据库及备份仍需按既有方式保护。

订单快照保存输入单位/数量、应付分值、CNY 币种、预期 quota、单价/组倍率/折扣依据、报价指纹和商户版本引用；返利基数冻结在充值订单原有字段。`credited_quota` 仅由钱包账本事件产生。订单响应不返回商户版本密钥。

`GET/POST /api/user/epay/notify` 只解析一次：总原始参数最多 16 KiB、最多 32 个字段、键最多 64 字节、值最多 4 KiB。重复字段（包括查询串与 POST 体重复）、非法编码、缺少必需字段均拒绝；限额内扩展字段参与验签。签名使用排序和 Builder 构造原文，并恒定时间比较 MD5。回调只使用订单绑定的商户版本验签。

成功付款须核对 PID、商户版本、订单号、金额与状态，并保存网关交易号。地址与 PID 构成商户账户身份，与网关交易号组成唯一凭据，密钥轮换不改变此唯一边界。订单行锁下，凭据、成功状态、钱包事件和返利同一事务提交；网络和验签在资金事务之外。重复回调只有凭据一致才确认成功。

管理员强制补单保持现有权限与入口，不增加查单前置条件或必填理由；按快照中的 quota 入账。之后合法回调可补齐凭据，但不重复增加余额或返利。

清空支付方式或更换当前商户仅影响新单。`payment_setting.epay_settlement_enabled` 默认 true，是独立结算开关；false 暂停回调和查单自动入账，管理员强制补单仍按原权限执行。资金事务先锁定同一设置行，再取得订单和钱包锁；暂停先提交时，在途回调/查单也不能入账，已取得设置锁的结算则先完成提交。

## 漏通知恢复

本人订单可调用 `POST /api/next/wallet/topups/:order_no/verify`，须 URL 编码订单号。服务端校验归属；普通 GET 列表/详情保持只读。查询使用订单版本向 HTTPS `api.php` 提交 `act=order`、`pid`、`key`、`out_trade_no`，密钥放表单体，不放 URL。超时 10 秒、响应上限 1 MiB、禁止重定向。

只接受标准顶层 `code=1`、`status=0/1` 与 `pid/out_trade_no/trade_no/type/money`；不猜测 `data`、`trade_status` 等方言。身份、订单与金额不符拒绝入账。网络错误及无法确认的响应返回明确失败，不视为未付款。合法未付款响应保持 pending，合法成功响应进入与回调相同的结算函数。

`epay_recovery` 复用系统任务数据库租约，每分钟最多处理 20 笔近 7 天待支付订单，并发最多 2。自动与手动查单共享订单级 30 秒租约和至少 60 秒请求间隔，失败/未支付后的退避为 1、5、15、60 分钟。记录查询时间及固定错误分类，不保存原始上游响应。超过 7 天保持 pending，仍接受合法回调及受控手动查单。

## 验证入口

后端测试必须带 `-timeout 60s`：

```powershell
go test ./pkg/epay ./service ./controller ./model ./middleware ./setting/operation_setting -timeout 60s
go test ./model -run TestEpayMigrationsAndConcurrentSettlementAcrossDatabases -v -timeout 60s
go test ./service -run 'TestEpayLocalGateway' -v -timeout 60s
```

三库测试使用独立测试数据库；MySQL/PostgreSQL 分别由 `GATEWAY_STARTUP_MYSQL_DSN`、`GATEWAY_STARTUP_POSTGRES_DSN` 指定。缺失环境时测试显式跳过，不能称为该数据库验收通过。TLS 模拟网关覆盖下单、通知与查单竞争、漏通知批量恢复，全部在本地运行，不触发真实支付。

当次执行结果与未完成项见 [易支付验证记录](EPAY_VALIDATION.md)。

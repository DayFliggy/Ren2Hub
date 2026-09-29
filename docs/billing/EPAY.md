# 易支付契约

本实现使用易支付 MD5 表单协议。订单必须具有冻结快照；不回填旧订单，缺少快照时拒绝入账。本文只描述易支付充值，其他支付渠道与部署分别遵循其对应实现和发布流程。

## 网关配置与浏览器跳转

`PayAddress` 是 HTTPS 网关基础地址，允许路径前缀；服务端正规化后追加 `/submit.php`。`EpayId` 为 PID，`EpayKey` 为商户密钥。`CustomCallbackAddress` 只填本站 Origin，留空使用 `ServerAddress`；异步通知地址固定追加 `/api/user/epay/notify`，回跳地址为 `ServerAddress + /usage-logs`。回跳不证明付款成功。

`PayMethods` 每项含 `name`、`type`，可配置 `icon` 和 `min_topup`。type 只能包含字母、数字、下划线和连字符，且必须被外部网关接受；不能把展示名称直接当协议类型。支付配置还要求管理员已完成现有条款确认。

后端返回签名参数与 URL，Vue 使用浏览器 POST 表单打开支付页，本站服务器不代替用户调用 `submit.php`。外部网关若对下单来源做 IP 限制，看到的是付款浏览器的出口地址；只放行本站服务器 IP 会阻止这种表单流程。

## 报价与下单

登录用户调用 `POST /api/next/wallet/topup/quote`：

```json
{"amount":10,"amount_unit":"USD","payment_method":"alipay"}
```

成功响应沿用 `{"success":true,"data":...}`。`data` 包括 `pay_amount`（如 `"73.00"`）、`currency:"CNY"`、`amount_minor:"7300"`、`expected_quota:"5000000"`、`minimum_quantity`、`minimum_quota`、`min_topup` 和 `quote_fingerprint`。分值及 quota 的精确字段均为十进制字符串，时间、ID 和输入数量沿用整数协议。

服务端使用 Decimal，计算 `USD 数量 × Price × 用户组倍率 × USD 预设折扣`，最后统一四舍五入至人民币分。USD 输入乘固定 500000 得到 quota；TOKENS 输入直接为 quota，例如 750000 对应 1.5 USD 额度，按单价 7.3 应付 10.95 CNY，实际入账仍是 750000。折扣统一按 USD 预设数量匹配，TOKENS 只有恰好对应整数 USD 预设时才应用该折扣，避免小额 Token 输入误用大额折扣。全局 `MinTopUp` 与方式 `min_topup` 均按 USD 解释，取较大值换算成 quota 后比较。

`POST /api/next/wallet/topup` 和旧 `POST /api/user/pay` 共用创建流程，接收上述字段及 `quote_fingerprint`。Vue 固定提交 USD；旧调用省略单位时按原展示设置归一化，保存后不再受展示设置影响。旧接口保留 `message/data/url` 封装。服务端每次下单重新报价，提交的指纹不一致返回 `QUOTE_CHANGED`，不会保存订单；Vue 刷新报价，等待再次点击付款。输入变化时立即禁用旧报价，取消或过期响应不能恢复付款按钮。

配置接口 `GET /api/next/wallet/config` 为每个方式返回同一规则算出的有效门槛。保存配置拒绝重复、空名称、非法类型及无效门槛；网关必须是 HTTPS。报价一次 SELECT 读取已提交的配置，商户轮换应通过现有批量设置接口一次提交地址、PID、密钥，避免人为逐项保存的中间配置。新订单会固定本次读取的完整版本。

充值单价 `Price` 和 `TopupGroupRatio` 的每项倍率必须为有限正数，并能保存在订单的精确金额字段中。单项、批量保存及读取使用同一校验规则；无效保存不覆盖旧配置，已有坏配置明确报错，不静默使用默认价格。

## 冻结快照与原子入账

增量迁移新增 `epay_merchant_versions` 和 `epay_orders`，不删除、重建既有账务数据。商户版本独立保存规范化网关地址、PID 和密钥，所有字段禁止 JSON 序列化，秘密 SQL 禁止输出日志。存储权限与现有数据库秘密配置边界一致，数据库及备份仍需按既有方式保护。

订单快照保存输入单位/数量、应付分值、CNY 币种、预期 quota、单价/组倍率/折扣依据、报价指纹和商户版本引用；返利基数冻结在充值订单原有字段。`credited_quota` 仅由钱包账本事件产生。订单响应不返回商户版本密钥。

`GET/POST /api/user/epay/notify` 只解析一次：总原始参数最多 16 KiB、最多 32 个字段、键最多 64 字节、值最多 4 KiB。重复字段（包括查询串与 POST 体重复）、非法编码、缺少必需字段均拒绝；限额内扩展字段参与验签。签名使用排序和 Builder 构造原文，并恒定时间比较 MD5。回调只使用订单绑定的商户版本验签。

成功付款须核对 PID、商户版本、订单号、金额与状态，并保存网关交易号。金额按 CNY 分精确比较，额外小数位只能为零，例如 `73.0000` 等同 `73.00`，`73.0001` 拒绝。地址与 PID 构成商户账户身份，与网关交易号组成唯一凭据，密钥轮换不改变此唯一边界。订单行锁下，凭据、成功状态、钱包事件和返利同一事务提交；验签在资金事务之外。重复回调只有凭据一致才确认成功。

本站按订单绑定的旧商户版本验签，外部网关也必须让存量订单继续使用原签名密钥。若网关只读取商户当前密钥，应新建商户/PID 接新单并保留旧凭据处理旧单，不能假定原地轮换自动兼容。

管理员强制补单保持现有权限与入口，不增加查单前置条件或必填理由；按快照中的 quota 入账。之后合法回调可补齐凭据，但不重复增加余额或返利。

成功补单的管理日志以 `user.topup_complete` 保存操作者和 `trade_no`，便于按订单追溯；重复补单请求仍留操作记录，但只产生一次资金入账。

清空支付方式或更换当前商户仅影响新单。`payment_setting.epay_settlement_enabled` 默认 true，是独立结算开关；false 暂停回调入账，管理员强制补单仍按原权限执行。资金事务先锁定同一设置行，再取得订单和钱包锁；暂停先提交时，在途回调也不能入账，已取得设置锁的结算则先完成提交。

## 异常处理与补单

Ren2Hub 不主动向支付网关查询订单，不提供用户手动验付和后台查单恢复。原 `POST /api/next/wallet/topups/:order_no/verify` 返回 404，`epay_recovery` 不再注册或调度；订单列表、详情和页面刷新仅查询本站已保存状态。

通知丢失或处理失败时，由外部支付网关重试通知。回调参数、签名或金额无效、结算暂停、资金事务失败时返回 `fail`；合法成功通知完成入账或确认同一凭据已处理后返回 `success`。事务失败不留下部分余额、流水或成功凭据，网关可以再次发送同一通知。

仍未入账的异常订单由管理员在外部网关复查，确认付款后使用现有补单入口，按冻结快照入账。补单后的合法通知可以补齐交易凭据；重复通知、重复补单及两者并发都不能重复充值或返利。没有收到有效通知、也未补单的订单继续保持 pending。

查单客户端、订单检查租约与退避逻辑已移除。已有数据库中的旧检查列和系统任务历史不删除，新代码不再读写这些字段或执行旧恢复任务。

## 验证入口

后端测试必须带 `-timeout 60s`：

```powershell
go test ./pkg/epay ./service ./controller ./model ./router ./middleware ./setting/operation_setting -timeout 60s
go test ./model -run TestEpayMigrationsAndConcurrentSettlementAcrossDatabases -v -timeout 60s
go test ./controller -run TestEpayGatewayRetriesAfterSettlementFailure -v -timeout 60s
go test ./router -run TestRetiredEpayVerificationReturnsNotFound -v -timeout 60s
```

三库测试使用独立测试数据库；MySQL/PostgreSQL 分别由 `GATEWAY_STARTUP_MYSQL_DSN`、`GATEWAY_STARTUP_POSTGRES_DSN` 指定。缺失环境时测试显式跳过，不能称为该数据库验收通过。回归覆盖回调失败后重试、暂停后恢复接收通知、管理员补单与重复通知、事务回滚和旧验付接口退休，不触发真实支付。

这些测试只证明标准 MD5 表单和模拟通知，不证明任意网关的私有协议方言兼容，也不证明真实付款、网关通知重试或公网回调可达。执行结果和部署版本只由所属工作区服务器档案记录，旧的验证记录从 Git 历史读取。

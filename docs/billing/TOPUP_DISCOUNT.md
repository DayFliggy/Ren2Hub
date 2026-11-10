# 充值折扣

充值折扣降低一笔充值的实付金额，到账额度按原价足额计算。折扣率由单笔充值金额阶梯、固定折扣、全站阶梯和个人日/周/月阶梯合成，只在下单时计算一次。模型调用按上游 NewAPI 计价（模型价 × 渠道倍率），不再打折。它不附带会员权益，也不建资金账务；扣费、退款和结算仍走原生钱包与 BillingSession。

管理入口为「系统设置 → 计费 → 充值折扣」`/system-settings/billing/topup-discount`，仅 Root 可访问。编辑器依次是固定优惠与上限、单笔充值阶梯、全站阶梯和个人日/周/月窗口。切换窗口时保留整份草稿，未选中窗口里的错误同样会阻止保存，并在页签上提示。排除账号和让利估算默认收起。所有表的模式和档位一次保存为一个版本。

## 公式

```text
r_amount = 单笔充值阶梯中阈值不大于本单 ExpectedQuota 的最高一档
r_user   = max(r_day, r_week, r_month)
r_total  = min(r_amount + r_fixed + r_site + r_user, cap)
factor   = 1 - r_total
实付     = RoundTopUpPay(原价实付 × factor, 币种)
```

每张表命中“阈值不大于金额”的最高一档；空表或 mode=off 贡献 0，shadow 只记录不生效。ExpectedQuota 取服务端 `validateTopUpQuota` 的结果，与订单冻结的到账额度一致。原价实付是各渠道原生公式去掉上游 `AmountDiscount` 后的金额，全程用 decimal 计算。`RoundTopUpPay` 是唯一的舍入点：half-up，IDR、JPY、KRW、VND 保留 0 位，其他币种保留 2 位。生成的字符串同时用于报价 `data`、`TopUp.PayAmount` 和网关请求，`TopUp.Money` 由它转换而来。

| 情形                           | 处理                                                                                 |
| ------------------------------ | ------------------------------------------------------------------------------------ |
| 易支付、GMPay、Waffo、Pancake  | 应用折扣；每个请求只调用一次 `QuoteTopUp`                                            |
| Stripe、Creem                  | 不打折。实扣由远端 Price 或商品价决定，响应保持上游形状                              |
| 管理员补单、兑换码、管理员加额 | 没有实付金额，不受影响                                                               |
| 排除名单内的用户               | 不计入全站总额，没有全站和个人折扣，固定折扣和单笔充值阶梯照常，不查询小时表         |
| 启用（on）的全站或个人表读失败 | 报价和下单返回 `code=topup_discount_unavailable`，不创建订单，底层原因只写服务端日志 |
| 只有 shadow 的来源读失败       | 其他分量照常，决策 status=`shadow_unavailable`                                       |
| 返利基数                       | 足额 ExpectedQuota，与折扣无关                                                       |
| 并发或囤积                     | 按下单时的窗口定档，多笔并发订单可以拿到同一档，不设累计额度；退款不追溯已用的优惠   |

## 下单与确认

四个应用折扣的渠道的 `.../amount` 接口仍在 `data` 中返回实付字符串，顶层另有 `quote`：`expected_quota`、`currency`、`list_pay_amount`、`pay_amount`、`factor`、`applied_rate`、`amount_rate`、`fixed_rate`、`site_rate`、`personal_rate`、`cap_applied` 和可选的 `winner_window`。

对应的 pay 请求可带 `expected_pay_amount` 和 `expected_currency`。两者都缺失时不比较，这是为了兼容上游客户端。只给一个、`null`、非字符串、格式错误或小数位超过币种精度时，返回参数错误。比较时币种大写后必须相同，金额按 decimal 数值相等，允许尾零差异。不一致时返回 `{"message":"error","code":"topup_quote_changed","data":"<文本>","quote":{...}}`。这个检查在写本地订单和调用网关之前进行，所以不会留下任何订单。本站前端总是携带这两个字段，收到变化后显示新价格，等用户再次确认。

订单与审计记录在同一个事务里写入。扩展表 `topup_discounts`（主键 `top_up_id`）保存配置版本、原价实付和完整决策 JSON，`TopUp.Insert` 和 GMPay `CreateGmpayOrder` 都在各自的事务中写入。决策为 inactive 时不写审计记录，shadow 和 `shadow_unavailable` 照常写入。回调只按冻结的 `PayAmount` 核对，不读这张表，也不重新计价。完整决策只出现在管理员订单详情 `GET /api/user/topup/admin/detail/:trade_no` 的 `discount` 字段里，不进入用户和管理员共用的订单列表。

## 窗口

小时表 `topup_discount_usage_hourly` 按 (user_id, 整点 UTC) 记录 used_quota 的业务增量。写入点在 `model/user.go` 的 `UpdateUserUsedQuotaAndRequestCount` 和 `UpdateUserUsedQuota`，同步写库，不进批量队列；消费、违规费、任务差额和退款都经过这两个函数。小时表是与 used_quota 并行的尽力统计，两边不在同一个事务里。任一边写入失败，或批量模式下进程中断，窗口金额都会偏高或偏低。小时写入失败只记录错误并计数，不影响扣费。

业务日 D 的截止点是当天 00:00（配置时区）。日、周、月窗口分别覆盖截止点前的 1、7、30 个业务日，区间左闭右开，结果按天缓存。截止点前取时、截止点后才提交的写入，以及服务器时钟回拨，都会落进已缓存的过去小时。这类写入要到重启重算后才计入，实现不设封桶水位。窗口净额为负时按 0。全站总额是排除名单以外各用户月窗口金额之和，负值按 0 计。

个人窗口在用户当天首次报价时查询，成功结果缓存到当天结束。全站总额按配置版本缓存，同一时刻只有一个查询。只有固定折扣和单笔充值阶梯的配置不需要任何查询。请求路径上每条小时表语句最长 3 秒，超时按失败处理。全站查询失败后 10 秒内不再重试，失败结果不写入缓存。业务日或时区变化时整体替换缓存，并删除 90 天以前的小时行。

## 配置

配置存于追加表 `topup_discount_configs`（schema_version 1），每次保存插入新版本，不写 `options` 表。表为空时使用内置版本 0：cap 5%，固定折扣 0，单笔充值阶梯与全站阶梯为 off 且没有档位，个人三表为 shadow，排除用户 1。启动时在提供服务前加载配置，读取失败则启动失败；加载成功后快照留在内存。只有 `PATCH /api/option/topup_discount` 能写入，请求带 `expected_version`，与当前版本不一致时返回 409；直接改库需要重启才生效。配置快照、保存互斥和日缓存都在进程内，因此只支持单实例部署。

校验规则如下：

- 请求体不超过 64 KiB，超出返回 413。
- 严格 JSON，未知字段与 JSON 数字一律拒绝。
- 比例为最多 4 位小数的十进制字符串。cap 不超过 0.5，其他比例不超过 cap。
- 每表最多 10 档，阈值为 quota 整数字符串，严格递增，比例不递减。
- 时区在未来 366 天内都必须是整小时偏移。
- 排除用户最多 100 个，不重复。

## 接口

| 方法与路径                                 | 权限 | 作用                                                                                                                  |
| ------------------------------------------ | ---- | --------------------------------------------------------------------------------------------------------------------- |
| `GET /api/option/topup_discount`           | Root | 当前配置与版本                                                                                                        |
| `PATCH /api/option/topup_discount`         | Root | 保存新版本                                                                                                            |
| `GET /api/option/topup_discount/status`    | Root | 业务日、截止点、全站月额与当前档、健康计数                                                                            |
| `POST /api/option/topup_discount/estimate` | Root | 用草稿回放最近 30 个完整业务日的成功充值，结果是额度等值的近似值                                                      |
| `GET /api/user/topup-discount`             | 本人 | 下一笔充值除单笔阶梯外的折扣（零阈值档也只列出不计入）、启用的单笔阶梯档位和个人窗口；不含全站金额、排除名单和 shadow |

估算只回放易支付、GMPay、Waffo 和 Pancake 的成功订单，按配置时区的本地午夜把 `create_time` 归入业务日，夏令时的 23 或 25 小时日同样按日界划分。每笔订单用下单当日的窗口和本单 ExpectedQuota 定档，shadow 按启用计算，排除用户也计入固定折扣和单笔阶梯。输出包括：

- `credited_quota`：到账额度合计。
- `discount_quota`：ExpectedQuota × r_total 的合计。
- `discount_share`：让利占到账额度的比例。
- `capped_quota_share`：触及上限的到账额度占比。
- `orders`：参与回放的订单数。
- `unrated_orders`：ExpectedQuota 为 0、无法评估的订单数。
- `history_covers_start`：最早一条小时记录是否覆盖回放起点。

完整回放需要约 60 天的小时记录。`history_covers_start` 只比较最早记录时刻，发现不了中间的缺口。

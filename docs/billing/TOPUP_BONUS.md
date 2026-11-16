# 充值加赠

充值加赠不改变实付金额，而是按本金额外入账一笔加赠额度。加赠比例由四部分相加后封顶：单笔入账阶梯、固定加赠、全站阶梯，以及个人日/周/月阶梯中最高的一项。适用范围是全部六个在线充值渠道（易支付、GMPay、Waffo、Waffo Pancake、Stripe、Creem）、兑换码和管理员加额，每笔入账只决定一次。模型调用按上游 NewAPI 计价（模型价 × 渠道倍率）。加赠不附带会员权益，也不建资金账务；扣费、退款和结算仍走原生钱包与 BillingSession。

管理入口为「系统设置 → 计费 → 充值加赠」`/system-settings/billing/topup-bonus`，仅 Root 可访问。编辑器依次是固定加赠与上限、单笔入账阶梯、全站阶梯和个人日/周/月窗口。切换窗口时整份草稿保留；未选中窗口里的错误同样阻止保存，并在页签上标出。排除账号和加赠估算默认收起。所有表的模式和档位作为一个版本一次保存。

## 公式

```text
r_amount = 单笔入账阶梯中阈值不大于本笔本金的最高一档
r_user   = max(r_day, r_week, r_month)
r_total  = min(r_amount + r_fixed + r_site + r_user, cap)
加赠     = floor(本金 × r_total)
到账     = 本金 + 加赠
```

每张表取阈值不大于金额的最高一档。空表或 mode=off 的表贡献 0，shadow 只记录、不生效。cap 不超过 50%，所以加赠总小于本金。各来源的本金如下：

- 易支付、GMPay、Waffo、Pancake：服务端 `validateTopUpQuota` 的结果。
- Stripe：`getStripeCreditedQuota` 的结果。
- Creem：商品配置的 quota。
- 兑换码：码面额度。
- 管理员加额：输入的额度。

实付仍按各渠道的原生公式计算，并去掉上游 `AmountDiscount`；`RoundTopUpPay` 是唯一的舍入点。

| 情形                                 | 处理                                                                                                     |
| ------------------------------------ | -------------------------------------------------------------------------------------------------------- |
| 六个在线充值渠道                     | 下单时决定加赠并冻结在订单上，回调和管理员补单只按冻结值入账                                             |
| 兑换码                               | 在兑换事务内、码状态 CAS 成功之后决定，加赠写入兑换码的 `bonus_quota`                                    |
| 管理员加额                           | 在操作号重放与冲突检查之后决定，写入 `admin_quota_operations.bonus_quota`；减额和覆盖不加赠              |
| 签到、注册与邀请奖励、返利划转、退款 | 不加赠                                                                                                   |
| 排除名单内的用户                     | 不计入全站总额，没有全站和个人加赠，固定加赠和单笔阶梯照常，不查询小时表                                 |
| 启用（on）的全站或个人表读失败       | 在线充值返回 `code=topup_bonus_unavailable` 且不建单；兑换返回通用失败，码仍可用；管理员加额失败且不写入 |
| 只有 shadow 的来源读失败             | 其他分量照常，决策 status=`shadow_unavailable`                                                           |
| 返利基数                             | 本金；加赠不产生返利                                                                                     |
| 并发或囤积                           | 按入账时的窗口定档，多笔并发入账可以拿到同一档，不设累计额度；退款不追溯已发放的加赠                     |

加赠决策只读取请求开始前预载的窗口与配置，事务内不再查询；模型层在重放检查或兑换 CAS 之后才调用它，所以重放、冲突和已用兑换码都不会重新决定加赠。

## 在线充值的报价与确认

易支付、GMPay、Waffo 和 Pancake 的 `.../amount` 接口仍在 `data` 中返回实付字符串，实付不受加赠影响。顶层另有 `quote` 对象，字段如下：

- 额度：`principal_quota`、`bonus_quota`、`expected_quota`。
- 价格：`currency`、`pay_amount`。
- 比例：`applied_rate`、`amount_rate`、`fixed_rate`、`site_rate`、`personal_rate`。
- 封顶与窗口：`cap_applied`，以及可选的 `winner_window`。

它们的 pay 请求可带 `expected_pay_amount` 与 `expected_currency`：两者同时出现或同时缺失，只给一个时返回参数错误。另可带独立的 `expected_bonus_quota`。三者全缺时不做比较，用于兼容上游客户端。`expected_bonus_quota` 必须是 JSON 整数，`null`、字符串、负数、小数、指数形式和超过钱包上限的值都按参数错误处理。价格或加赠任一不一致，返回 `{"message":"error","code":"topup_quote_changed","data":"<文本>","quote":{...}}`。

Stripe 和 Creem 的实扣由远端价格决定。前端先用 `GET /api/user/topup-bonus/preview?principal_quota=` 取得加赠，下单时只带 `expected_bonus_quota`。加赠不一致时返回 `code=topup_bonus_changed`，并在 `bonus` 字段中附上新的加赠视图（不含价格）。所有确认检查都在写本地订单、创建 Checkout Session 或调用网关之前完成，因此不会留下订单。本站前端总是携带确认字段，收到变化后展示新的报价或加赠，等用户再次确认。

订单把全部到账额度冻结在 `TopUp.ExpectedQuota`（本金 + 加赠），`BonusQuota` 记录其中的加赠，`AffiliateBaseQuota` 等于本金。结算只读取这些冻结值，最多入账一次，回调不会重新计价。不认识 `BonusQuota` 的旧版本同样按 `ExpectedQuota` 入账，并按 `AffiliateBaseQuota` 计算返利，所以回滚后加赠照常发放、只发一次。Stripe 的 `Money` 仍保持原生数量含义。

## 决策记录与展示

扩展表 `topup_bonus_decisions` 以 (source_type, source_id) 为主键，保存用户、配置版本和完整决策 JSON。source_type 取 `topup`、`redemption` 或 `admin_quota`。决策记录与入账写在同一事务里；inactive 决策不写记录，shadow 和 `shadow_unavailable` 照常写入。它只用于解释，结算、回调和估算都不读取。完整决策只出现在管理员订单详情 `GET /api/user/topup/admin/detail/:trade_no` 的 `bonus` 字段中。用户和管理员的订单列表只带 `bonus_quota`。

各入口的展示：

- 兑换接口：`data` 为到账合计，顶层 `bonus_quota` 为其中的加赠；有加赠时，充值日志写明加赠额度。
- 管理员加额：响应的 `data.bonus_quota` 是加赠额度，重放时返回已提交的值。收款人的充值日志追加 ", including a bonus of N"，审计参数带 `bonus_quota`；`quota` 仍是输入的本金。
- 已使用的兑换码不可修改，状态只能在启用和禁用之间切换，因此已发放加赠的来源记录保持不变。

## 窗口

小时表 `topup_bonus_usage_hourly` 按 (user_id, 整点 UTC) 记录 used_quota 的业务增量，写入点有两个：

- 位置：`model/user.go` 的 `UpdateUserUsedQuotaAndRequestCount` 与 `UpdateUserUsedQuota`。消费、违规费、任务差额和退款都经过这两个函数。
- 写法：同步写库，不进批量队列。

小时表是与 used_quota 并行的尽力统计，两者不在同一个事务里。任一边写入失败，或批量模式下进程中断，都会让窗口金额偏高或偏低。小时写入失败只记录错误并计数，不影响扣费。

业务日 D 的截止点是当天 00:00（配置时区）。日、周、月窗口分别覆盖截止点前的 1、7、30 个业务日，区间左闭右开，结果按天缓存。以下写入会落进已缓存的过去小时：

- 截止点前取时、截止点后才提交的写入。
- 服务器时钟回拨后的写入。

这类写入要到重启重算后才会计入，实现不设封桶水位。窗口净额为负时按 0 计。全站总额是排除名单以外各用户月窗口金额之和，负值同样按 0 计。

个人窗口在用户当天首次报价或入账时查询，成功结果缓存到当天结束。全站总额按配置版本缓存，同一时刻只有一个查询。只有固定加赠和单笔阶梯的配置不需要任何查询。请求路径上每条小时表语句最长 3 秒，超时按失败处理。全站查询失败后 10 秒内不再重试，失败结果不写入缓存。业务日或时区变化时，缓存整体替换，同时删除 90 天以前的小时行。

## 配置

配置存于追加表 `topup_bonus_configs`（schema_version 1），每次保存插入一个新版本，不写 `options` 表。表为空时使用内置版本 0：

- 上限 5%，固定加赠 0。
- 单笔阶梯与全站阶梯为 off 且没有档位，个人三表为 shadow。
- 排除用户 1。

服务在提供服务前加载配置，读取失败则启动失败；加载成功后快照保留在内存中。只有 `PATCH /api/option/topup_bonus` 能写入配置，请求须带 `expected_version`，与当前版本不一致时返回 409。直接改库要重启才生效。配置快照、保存互斥和日缓存都在进程内，所以只支持单实例部署。

校验规则：

- 请求体不超过 64 KiB，超出返回 413。
- 严格 JSON，未知字段与 JSON 数字一律拒绝。
- 比例为最多 4 位小数的十进制字符串。cap 不超过 0.5，其他比例不超过 cap。
- 每表最多 10 档，阈值为 quota 整数字符串，严格递增，比例不递减。
- 时区在未来 366 天内都必须是整小时偏移。
- 排除用户最多 100 个，不重复。

## 从充值折扣迁移

加赠替换了 2026-11-10 的充值折扣，旧配置和旧表不保留。AutoMigrate 之后，首次启动依次执行：

1. 加赠配置表为空时，把最新一版折扣配置原样复制为加赠版本 1，作者和时间不变；比例数值沿用、不做等值换算，5% 折扣即变为 5% 加赠。复制后的配置须通过校验、加载成功，才删除 `topup_discount_configs`。加赠表已有版本时以它为准，只删除旧表。
2. 小时用量按 1000 行一批移入 `topup_bonus_usage_hourly`。每批在一个事务里逐行相加，再按主键删除源行，删除行数必须为 1；全部移完后删除源表。中断后重启会从剩余行继续，不会重复计数。
3. 删除 `topup_discounts`、`usage_discount_configs`、`usage_discount_hourly`。
4. `top_ups`、`admin_quota_operations`、`redemptions` 新增 `bonus_quota` 列，默认值 0，旧行不加赠。

任一步失败都会阻止启动，包括查询旧表是否存在失败，源数据保持不变。回滚到折扣版本时，旧镜像会重建空的折扣表并使用内置默认配置（无折扣）。已建的加赠订单仍按冻结的 `ExpectedQuota` 足额入账。旧版没有兑换码不可变限制，回滚期间已用兑换码可以被改动。再次升级时，回滚期写入的小时用量会相加迁入。

## 接口

| 方法与路径                              | 权限 | 作用                                                                                                                        |
| --------------------------------------- | ---- | --------------------------------------------------------------------------------------------------------------------------- |
| `GET /api/option/topup_bonus`           | Root | 当前配置与版本                                                                                                              |
| `PATCH /api/option/topup_bonus`         | Root | 保存新版本                                                                                                                  |
| `GET /api/option/topup_bonus/status`    | Root | 业务日、截止点、全站月额与当前档、健康计数                                                                                  |
| `POST /api/option/topup_bonus/estimate` | Root | 用草稿回放最近 30 个完整业务日的全部入账，结果是估算                                                                        |
| `GET /api/user/topup-bonus`             | 本人 | 下一笔入账除单笔阶梯外的加赠比例（零阈值档也只列出、不计入）、启用的单笔阶梯档位和个人窗口；不含全站金额、排除名单和 shadow |
| `GET /api/user/topup-bonus/preview`     | 本人 | `principal_quota` 一笔入账的加赠视图，供 Stripe 与 Creem 下单前确认；Access Token 需要 `wallet:read`                        |

估算回放三类来源，均按配置时区的本地午夜划分业务日，夏令时的 23 或 25 小时日同样按日界划分：

- 六个在线渠道的成功订单，按 `create_time` 归日，本金为 `ExpectedQuota − BonusQuota`。
- 已使用的兑换码，包括使用后被删除的，按 `redeemed_time` 归日。
- 管理员加额，按 `created_at` 归日，含旧版空 mode 的加额。

每笔用入账当日的窗口和本笔本金定档，加赠单独向下取整。shadow 按启用计算，排除用户也计入固定加赠和单笔阶梯。输出字段：

- `principal_quota` 与 `bonus_quota`：本金与加赠合计。
- `bonus_share`：加赠占本金的比例。
- `capped_principal_share`：触及上限的本金占比。
- `records`：参与回放的入账笔数。
- `unrated_records`：本金为 0、无法评估的笔数。
- `sources`：按 `topup`、`redemption`、`admin_quota` 顺序给出每类的笔数、本金与加赠。
- `history_covers_start`：最早一条小时记录是否覆盖回放起点。

完整回放需要约 60 天的小时记录。`history_covers_start` 只比较最早一条记录的时刻，发现不了中间的缺口。

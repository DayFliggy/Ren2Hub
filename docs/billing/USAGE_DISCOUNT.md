# 用量折扣

用量折扣是乘在渠道倍率上的一个价格系数，由固定折扣、全站阶梯和个人日/周/月阶梯合成。它不附带会员权益，不复制模型基础价，也不建资金账务；扣费、退款和结算仍走原生钱包与 BillingSession。

## 公式

```text
r_user  = max(r_day, r_week, r_month)
r_total = min(r_fixed + r_site + r_user, cap)
factor  = 1 - r_total
有效渠道倍率 = 基础渠道倍率 × factor
```

每个窗口按自己的阶梯表命中“阈值不大于窗口金额”的最高一档；空表或 mode=off 贡献 0，shadow 只记录不生效。所有原生计价项都在最终取整前乘一次渠道倍率，所以折扣额等于 r_total × 实际调用价格。factor 只在 `relay/helper/price.go` 的 `HandleChannelRatio` 中乘一次，下游读到的 `ChannelRatioInfo.ChannelRatio` 都是有效倍率，不能再乘 factor。

| 情形                                             | 处理                                             |
| ------------------------------------------------ | ------------------------------------------------ |
| 渠道测试                                         | factor = 1，status=not_applicable                |
| 排除名单内的用户                                 | 不计入全站总额，没有全站和个人折扣，固定折扣照常 |
| 小时数据读取失败或超时                           | 只用固定折扣，status=unavailable                 |
| 违规费                                           | 按基础渠道倍率，不打折                           |
| 渠道重试、Responses Compact 二次计价、已受理任务 | 沿用本逻辑请求第一次计价时的决策                 |
| 免费模型                                         | 沿用原生 `isFreeModel`，折扣不改变免费判断       |

## 窗口

小时表 `usage_discount_hourly` 按 (user_id, 整点 UTC) 记录 used_quota 的业务增量。写入点是 `model/user.go` 的 `UpdateUserUsedQuotaAndRequestCount` 和 `UpdateUserUsedQuota`，同步写库，不进批量队列；消费、违规费、任务差额和退款都经过这两个函数。它是与 used_quota 并行的尽力统计，两边不在同一事务：任一边写入失败或批量模式下进程中断，窗口金额会偏高或偏低，方向取决于失败的是消费还是退款。小时写入失败只记录错误并计数，不影响扣费。

业务日 D 的截止点是 D 当天 00:00（配置时区）。日、周、月窗口分别是截止点前 1、7、30 个业务日，左闭右开。小时行按写入时取得的时刻归档，所以截止点之前的行在 D 内通常不再变化，结果按天缓存。例外是在截止点前取时、截止点后才提交的写入，以及服务器时钟回拨：这些写入会落进已缓存的过去小时，当天缓存不含它们，重启后重算会包含，个人窗口与全站总额也可能分别在提交前后读取。本实现不设封桶水位，接受这一差异。窗口净额为负时按 0。全站总额是排除名单以外各用户月窗口金额（负值按 0）之和。

个人窗口在用户当天第一次请求时查询并缓存到当天结束；全站总额按配置版本缓存，同一时间只有一个查询，其余请求等待它。配置未启用、用户被排除或渠道测试时不查询。请求路径上的每条小时表语句最长 3 秒，超时按失败处理；全站查询失败后 10 秒内不再重试，直接按 unavailable 计价。业务日或时区变化时整体替换缓存，由触发替换的请求删除 90 天以前的小时行。

## 配置

配置存独立追加表 `usage_discount_configs`，每次保存插入新版本，不进 `options` 表。表为空时使用内置版本 0：cap 5%，固定 0，全站 off 且无档位，个人三表 shadow，排除用户 1。启动时在提供服务前加载配置，读取失败则启动失败，不会以默认值或原价代替已保存的固定折扣；加载成功后快照留在内存，之后的数据库故障不影响它。只有 `PATCH /api/option/usage_discount` 能写入，请求带 `expected_version`，不一致返回 409。直接改库需要重启才生效。配置快照、保存互斥和日缓存都在进程内，前提是单实例部署；多个实例共用数据库时，其他实例看不到新版本，并发保存可能得到主键冲突而不是 409。

校验规则：请求体不超过 64 KiB，超出返回 413；严格 JSON，未知字段与 JSON 数字一律拒绝；比例为最多 4 位小数的十进制字符串，cap 不超过 0.5，其他比例不超过 cap；每表最多 10 档，阈值为 quota 整数字符串且严格递增，比例不递减；时区在未来 366 天内都必须是整小时偏移；排除用户最多 100 个且不重复。

## 接口

| 方法与路径                                 | 权限 | 作用                                                                 |
| ------------------------------------------ | ---- | -------------------------------------------------------------------- |
| `GET /api/option/usage_discount`           | Root | 当前配置与版本                                                       |
| `PATCH /api/option/usage_discount`         | Root | 保存新版本                                                           |
| `GET /api/option/usage_discount/status`    | Root | 业务日、截止点、全站月额与当前档、健康计数                           |
| `POST /api/option/usage_discount/estimate` | Root | 用最近 30 个完整业务日的小时数据回放草稿配置，结果是近似值           |
| `GET /api/user/usage-discount`             | 本人 | 本人 factor、各分量比例、启用窗口的金额与档位；不含全站金额和 shadow |

估算是近似值，不是账单，误差方向不固定：金额是实际扣费额，启用折扣后已是折后金额；违规费和跨日退款也计入当天净额；排除用户既不计入收入也不计入让利。每个回放日需要此前 30 天数据，完整回放需要约 60 天小时记录；`history_complete` 只比较最早记录时刻，不能发现中间缺失。

## 日志

`channel_ratio` 仍是本次计价使用的有效倍率。有组件启用时，公开 `usage_discount` 对象给出 status、factor、applied_rate、各分量比例、winner_window、cap_applied 和 base_channel_ratio；管理员 `admin_info.usage_discount` 另给 raw_rate、cap_rate、shadow 比例、config_version、business_date 和 reason。纯 shadow 期间只写管理员对象；shadow 组件在公开对象里的比例恒为 0，假设比例只在 `shadow_rates`。日志不写全站金额和排除名单。违规费日志的 `channel_ratio` 是基础倍率，不写 usage_discount。MJ 退款日志（`service/midjourney.go`）不带 usage_discount，退款额是原扣费额。

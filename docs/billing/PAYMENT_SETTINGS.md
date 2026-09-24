# EPay / GMPay 系统设置

2026-09-24 核对。控制台入口为“系统设置 → 计费”，其中“EPay 易支付”与“GMPay 虚拟币支付”独立配置。“通用支付与其他网关”保存共用规则；站点地址沿用“站点设置”的唯一来源。只有 Root 可以读写支付设置，钱包用户接口不返回商户密钥。

本次只调整 EPay、GMPay 及其需要的共用配置。Stripe、Creem、Waffo、Waffo Pancake 保留既有实现和配置入口。

## 配置清单

默认值表示未保存该键时的运行默认，不表示已启用支付。价格、倍率和最低额度必须有效；新单还要求网关、PID、密钥、支付方式或资产齐全，并完成支付合规确认。

| EPay 配置键 | 默认值 | 用途与约束 |
| --- | --- | --- |
| `PayAddress` | 空 | HTTPS 网关基础地址，可含路径前缀；正规化后追加 `/submit.php`，可使用具有可信证书的 IP |
| `EpayId` | 空 | 商户 PID；按字符串保存，保留前导零和长数字，不转换成数值 |
| `EpayKey` | 空 | MD5 签名密钥；仅写入，留空保留原值 |
| `PayMethods` | 支付宝、微信、自定义 1 | JSON 数组，字段为 `name`、`type`、可选 `icon`、`color`、`min_topup`；自定义 1 的默认门槛为 50 USD；空数组停止新单 |
| `Price` | `7.3` | 每购买 1 USD 额度的基础应付 CNY 金额，必须大于 0 |
| `MinTopUp` | `1` | 最低购买额度，正整数 USD；与每方式 `min_topup` 取较大值 |
| `payment_setting.epay_settlement_enabled` | `true` | 是否接受自动回调入账；关闭后返回失败供网关重试，不阻止管理员核实后补单 |

| GMPay 配置键 | 默认值 | 用途与约束 |
| --- | --- | --- |
| `GmpayPayAddress` | 空 | HTTPS 网关基础地址，可含路径前缀；追加原生 `/payments/gmpay/v1/` 接口路径 |
| `GmpayId` | 空 | 商户 PID，字符串 |
| `GmpayKey` | 空 | HMAC-SHA256 签名密钥；仅写入，留空保留原值 |
| `GmpayAssets` | `[]` | JSON 数组；`name`、`token`、`network` 及可选 `icon`、`color`、`min_topup`；币种和网络组合不能重复，空数组停止新单 |
| `GmpayPrice` | `1` | 每购买 1 USD 额度的基础应付 USD 金额，必须大于 0 |
| `GmpayMinTopUp` | `1` | 最低购买额度，正整数 USD；与资产门槛取较大值 |
| `GmpayGroupRatio` | `{"default":1}` | GMPay 专用用户组倍率，JSON 对象，各倍率必须大于 0 |
| `payment_setting.gmpay_settlement_enabled` | `true` | 是否接受自动回调入账；关闭后管理员仍可核实并补单 |

最低额度指购买的额度，不是折扣后的实付金额。金额计算统一使用冻结报价，不受后续设置变化影响。GMPay 资产探测只读取公开目录，不能证明 PID、密钥、钱包、链上到账或成功通知已通过验收。

| 共用配置键 | 默认值 | 入口与作用 |
| --- | --- | --- |
| `ServerAddress` | 运行默认 `http://localhost:3000` | 站点设置；外部可访问的本站 Origin，同时用于支付完成后的 `/usage-logs` 返回地址 |
| `CustomCallbackAddress` | 空 | 通用支付设置；本站通知 Origin，空值使用 `ServerAddress`。只含协议、主机及可选端口，不含路径、查询参数、片段或登录信息 |
| `TopupGroupRatio` | `{"default":1,"vip":1,"svip":1}` | 通用支付设置；EPay 充值分组倍率，部分旧渠道也消费此键，修改前应考虑共用影响 |
| `payment_setting.amount_options` | `[10,20,50,100,200,500]` | 共用 USD 快捷购买额度，正整数数组；只是快捷入口，服务端始终独立校验报价 |
| `payment_setting.amount_discount` | `{}` | 整数 USD 购买档位到折扣倍率的映射，例如 `{"100":0.9}`；倍率范围 `0 < ratio <= 1`，只精确匹配对应档位 |

回调 Origin 支持 HTTP/IP 测试环境，也支持 HTTPS；网关基础地址仍要求 HTTPS。自动追加的通知路径分别是 `/api/user/epay/notify`、`/api/user/gmpay/notify`。已启用渠道遇到非法回调或返回地址会拒绝创建可支付订单，不能把完整通知 URL 填入 Origin 字段。

`general_setting.quota_display_type` 属于额度展示设置，合法值为 `USD`、`CNY`、`TOKENS`、`CUSTOM`；它不改变 500000 quota/USD 的固定换算。现代钱包请求显式传入 `amount_unit`。TOKENS 购买数量恰好对应整数 USD 时，才能匹配上述 USD 折扣档位。

`payment_setting.compliance_confirmed`、`compliance_terms_version`、`compliance_confirmed_at`、`compliance_confirmed_by`、`compliance_confirmed_ip` 是专用合规确认流程保存的证据，不能通过通用配置 API 任意改写，不作为普通输入框开放。

## 保存与运营边界

EPay 和通用项通过 `/api/option/bulk` 原子保存；GMPay 商户及资产通过 `/api/option/gmpay/save` 原子保存，其自动结算开关通过通用配置接口保存。设置读取失败时不允许拿默认值覆盖现有配置；结构化编辑器出现非法草稿时阻止保存。保存后的密钥输入及客户端共享缓存保持空值，仅保留“已配置”状态。

更换商户 PID 或网关地址时，应同步填写对应密钥；空密钥的含义始终是保留原值，公开配置探测不验证这些凭据。停止新单应清空 `PayMethods` 或 `GmpayAssets`，不要用自动结算开关代替。旧订单继续使用下单时冻结的金额、商户版本和密钥；旧通知恢复后只允许入账一次。

Ren2Hub 通过 API 对接未修改的 epusdt 原项目。链、收款钱包、节点/RPC、汇率来源、通知重试次数和网关管理账户属于 epusdt 自身配置，不在 Ren2Hub 系统设置中复制一份。epusdt 原地替换商户密钥可能使旧订单通知无法通过冻结密钥验签，应采用新旧 PID 并存的轮换流程。

协议、金额和恢复行为分别见 [EPay 契约](EPAY.md) 和 [GMPay 契约](GMPAY.md)。测试结果与部署版本分开记录；修改设置源码不表示服务器常驻应用已经更新，也不证明完成了真实链上转账。

# 中英文术语表

当前 Vue 支持 `zh-CN` 和 `en`，后端支持 `zh` 和 `en`。此表约定现行产品用语；历史法语、俄语 UI 术语表不再维护。翻译键与分域加载规则见[项目约定](../AGENTS.md)和[前端说明](../frontend/README.md)。

| 中文 | English | 含义 |
| --- | --- | --- |
| API 令牌 | API token | 调用模型接口的凭据与附加消费限额 |
| 访问令牌 | Access token | 浏览器内存中保存的短期登录 JWT |
| 刷新令牌 | Refresh token | HttpOnly Cookie 中的登录刷新凭据 |
| token | Token | 模型输入/输出计量单位；不要与账户凭据混淆 |
| 渠道 | Channel | 上游端点、凭据和能力配置 |
| 供应商 | Provider / Vendor | 提供模型或 API 的服务方 |
| 模型映射 | Model mapping | 客户端模型名到上游模型名的映射 |
| 路由配置 | Routing profile | Token 的自动/手动路由设置 |
| 路由组 | Route group | 手动 Profile 内的 Entry 和 Policy 集合 |
| 账号分类 | User group | 可用于充值和账号限流，不授予模型权限或决定 Relay 价格 |
| 优先级 | Priority | 自动路由的排序硬边界 |
| 位置 | Position | 手动路由 Entry 的排序硬边界 |
| 权重 | Weight | 同一合格层内的加权选择依据 |
| 并发容量 | Concurrency capacity | 当前用户或渠道同时占用的槽位上限 |
| 等待队列 | Wait queue | 等待准入的请求；不是异步生成任务队列 |
| 钱包 | Wallet | 唯一资金来源 |
| 额度 | Quota | 固定 500000 quota/USD 的整数记账单位 |
| 可用额度 | Available quota | 扣除在途预留后的可用预算 |
| 预留 | Reservation | 发送前持久化保留的预算 |
| 已结算消费 | Settled usage | 已确认并计入账户的费用 |
| 价格版本 | Price version | 从模型配置派生的不可变执行证据，不是独立发布目录 |
| 冻结报价 | Frozen quote | 每次尝试的有效模型配置、任务用量合同、工具价格和渠道系数快照 |
| 渠道倍率 | Channel ratio | 冻结报价中的渠道售价系数 |
| 模型倍率 | Model ratio | 基础输入价格；每百万 token 的 USD 单价为倍率乘 2 |
| 输出倍率 | Completion ratio | 输出相对于基础输入价格的倍率 |
| 按次价格 | Per-call price | 每次请求的 USD 单价；图片适用数量与历史尺寸质量规则 |
| 自动获取 | Fetch prices | 获取五源候选；管理员选单源填入草稿，保存才生效 |
| 对账 | Reconciliation | 对未知消费或失败处理证据作出明确决定 |
| 资金流水 | Billing event | 与钱包/Token 投影同事务保存的不可变事件 |
| 充值 | Top-up | 经业务凭据向钱包入账 |
| 兑换码 | Redemption code | 可兑换钱包额度的代码 |
| 超级管理员 | Root | 最高管理权限；对账和价格配置保存由服务端核验 |
| 两步验证 | Two-factor authentication | 登录或敏感操作的附加验证 |

Model Ratio、Completion Ratio 属于当前 NewAPI 定价界面；订阅套餐不属于当前钱包资金来源。明确零价与未配置价格必须用不同文案，获取候选不能写成已发布。金额展示最多 3 位小数，编辑、提交和导出必须保留原始精度；翻译不得改变单位。

# 术语表

Vue支持简体中文与英语，翻译遵循现行接口单位和权限。后端语言以 `i18n/locales/` 为准。

| 中文         | English               | 含义                                           |
| ------------ | --------------------- | ---------------------------------------------- |
| API Key      | API key               | 调用模型的凭据及附加额度限制                   |
| 登录会话     | Browser session       | 控制台JWT和刷新Cookie对应的服务端会话          |
| 管理访问令牌 | Personal access token | 原生scoped PAT，与模型Key不同                  |
| 钱包额度     | Wallet quota          | 用户原生quota余额                              |
| 剩余额度     | Remaining quota       | Token remain_quota，受unlimited_quota约束      |
| 已用额度     | Used quota            | 原生used_quota统计                             |
| 预扣         | Pre-consumption       | NewAPI请求生命周期内预先扣减额度               |
| 渠道倍率     | Channel ratio         | 渠道配置的价格倍率，费用为模型基础价乘渠道倍率 |
| 私有路由     | Private routing       | 每Token一份有序渠道列表，只缩小授权候选        |
| 自动路由     | Automatic routing     | 按优先级、权重和亲和在可用渠道中自动选择       |
| 工单         | Support ticket        | 用户与支持人员的消息及附件                     |
| 充值         | Top-up                | 通过原生订单向钱包增加额度                     |
| 充值加赠     | Top-up bonus          | 入账时按本金另赠的额度，实付不变               |
| 商户版本     | Merchant version      | GMPay订单绑定的不可变验签凭据版本              |

不用冻结报价、分组倍率、预留账户版本和资金事件这些已退役的术语。明确零价、缺项（未定价）和计算结果应分别表达，不使用“默认价格”描述未配置项；前端不得通过翻译或展示精度改变提交金额。

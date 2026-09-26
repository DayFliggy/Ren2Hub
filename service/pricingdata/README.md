# 自动定价离线快照

`sub2api.json.gz` 和 `axonhub.json.gz` 保存两个默认源的原始 UTF-8 JSON，使用 gzip 压缩后嵌入服务。下载日期为 2026-09-25；哈希针对解压后的原始字节。

| 来源 | 原始地址 | SHA-256 |
| --- | --- | --- |
| Sub2API 使用的 model-price-repo | `https://raw.githubusercontent.com/Wei-Shaw/model-price-repo/main/model_prices_and_context_window.json` | `b356bffe7cb91540dfd236e0eaeacb6088c4db2d81c9b3477ae43a0bebcbc306` |
| AxonHub 使用的 PublicProviderConf | `https://raw.githubusercontent.com/ThinkInAIXYZ/PublicProviderConf/refs/heads/dev/dist/all.json` | `c916528e25127693f05b4b6124abe6f0d8f753e6406b7881c08b02f2cbdad5da` |

Sub2API 快照已与同目录 `model_prices_and_context_window.sha256` 核对一致。AxonHub 未提供独立校验文件，记录下载正文的 SHA-256。

启动仅在主库尚无该来源记录时写入快照，不覆盖持久化目录。随后通过统一系统任务检查远端；无网络也可使用已验证的目录。Sub2API 为主源，AxonHub 按整模型补缺；models.dev 与 OpenRouter 已退出自动定价链路。快照数据不是人工价，后续有效自动更新会发布新的不可变价格版本。

更新快照时，从 `service/billing_auto_pricing.go` 的固定来源下载、校验 Sub2API 哈希，保留原始字节再 gzip 压缩，并更新本表。运行 `go test ./pkg/autopricing ./service -run 'Test.*Pricing|Test.*Catalog' -timeout 60s` 验证解析与离线启动；不能用旧倍率导出器覆盖历史 `model/billing_pricing_seed.json`，后者仅用于辨认旧安装价格归属。

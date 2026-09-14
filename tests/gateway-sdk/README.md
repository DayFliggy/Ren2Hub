# 官方 SDK 隔离验收

测试通过真实本地网关连接模拟供应商，不使用生产 Key，也不认证真实模型兼容性。Python 版本固定在 `requirements.txt`；TypeScript SDK 固定在 `package.json` 和 `bun.lock`。Chat、Responses、Messages 使用 SDK 自带响应解析和流累积器；Gemini 使用官方 SDK 的内容事件对象，Realtime 使用官方类型化迭代器/事件接口，不把测试中的事件收集称作 SDK 内置累积器。

在主模块目录设置 `GATEWAY_SDK_PYTHON`、`GATEWAY_SDK_NODE` 为已安装固定依赖的运行时路径；必要时设置 `PYTHONPATH` 指向隔离依赖目录。缺少运行时变量时 Go 测试会跳过，不能将 SKIP 记为通过。

每套 SDK 验收单独启动一个 `go test`，统一使用 60 秒超时。不要将所有套件合并进一个 `-run` 表达式，尤其三套云测试应分别执行：

```powershell
$sdkSuites = @(
  'TestGatewayOfficialSDK',
  'TestGatewayOfficialMediaSDK',
  'TestGatewayOfficialAudioSDK',
  'TestGatewayOfficialRealtimeSDK',
  'TestGatewayOfficialCloudSDK',
  'TestGatewayOfficialCloudToolAndInterruptionSDK',
  'TestGatewayOfficialCloudParallelToolSDK',
  'TestGatewayOfficialLegacySDK',
  'TestGatewayOfficialCompatibleSDK',
  'TestGatewayOfficialSpecialSDK',
  'TestGatewayOfficialCodexSDK',
  'TestGatewayOfficialVertexOpenSourceSDK',
  'TestGatewayOfficialCustomSDK',
  'TestGatewayOfficialSemanticPythonSDK',
  'TestGatewayOfficialSemanticNodeSDK'
)
foreach ($suite in $sdkSuites) {
  go test -p 4 -timeout 60s ./router -run "^${suite}$" -count=1 -v
  if ($LASTEXITCODE -ne 0) { throw "$suite failed" }
}
```

| 脚本组 | 对应 Go 套件 | 已通过的本地验收 |
|---|---|---|
| `verify.py` / `verify.mjs` | `TestGatewayOfficialSDK` | 每语言 32 条基础文本路径、16 轮工具往返、16 条中断流；四入口 × OpenAI/Claude/Gemini/Ollama，含状态归属和前导失败切换 |
| `verify_media.py` / `verify_media.mjs` | `TestGatewayOfficialMediaSDK` | 每语言 7 条正常路径和 1 条图片中断；Embedding float/base64、图片 JSON/SSE、TTS PCM、STT JSON/multipart 流 |
| `verify_audio.py` / `verify_audio.mjs` | `TestGatewayOfficialAudioSDK` | 两语言 16 场景；MiniMax、火山 HTTP/WS、Cloudflare 转写，不可执行路径零发送及二进制截断异常 |
| `verify_realtime.py` / `verify_realtime.mjs` | `TestGatewayOfficialRealtimeSDK` | 两语言 8 场景；并发响应、工具、上游断开、供应商错误、客户端关闭和正常关闭码 |
| `verify_cloud.py` / `verify_cloud.mjs`，基础阶段 | `TestGatewayOfficialCloudSDK` | 两语言 80 条路径；Azure、AWS Claude Bearer/SigV4、Vertex Claude/Gemini 五配置 × 四入口 × JSON/SSE |
| 同上，工具与中断阶段 | `TestGatewayOfficialCloudToolAndInterruptionSDK` | 两语言 40 轮单工具往返和 40 条已交付中断，覆盖相同五配置与四入口 |
| 同上，双工具阶段 | `TestGatewayOfficialCloudParallelToolSDK` | 两语言 40 轮双工具往返；首帧两个调用、分片参数 `0/false`、逆序工具结果、最终响应及唯一终止事件 |
| `verify_legacy.py` / `verify_legacy.mjs` | `TestGatewayOfficialLegacySDK` | 两语言 108 场景；Baidu/Cohere/Tencent TC3/旧 Zhipu/Dify/PaLM，24 条 Chat JSON/SSE、72 条发送前拒绝、10 条已交付中断、2 条缓冲失败 |
| `verify_compatible.py` / `verify_compatible.mjs` | `TestGatewayOfficialCompatibleSDK` | 28 配置，每语言 170 条 JSON/SSE 成功、54 条发送前拒绝；两语言合计 448 场景，验证实际适配器、模型/URL 映射、请求体、认证、输出和 usage |
| `verify_special.py` / `verify_special.mjs` | `TestGatewayOfficialSpecialSDK` | Nova/Coze/讯飞，两语言 54 场景；10 条成功、38 条发送前拒绝、4 条已交付中断、2 条 Coze 已接受后查询失败不重放 |
| `verify_codex.py` / `verify_codex.mjs` | `TestGatewayOfficialCodexSDK` | 两语言 32 场景；4 条 Responses JSON/SSE、12 条其他协议拒绝、4 条未知/跨 Token 拒绝、4 条凭据调序续轮、4 条停用/替换拒绝、2 条上游错误、2 条已交付中断 |
| 复用 `verify_compatible.py` / `verify_compatible.mjs` | `TestGatewayOfficialVertexOpenSourceSDK` | llama 与非 llama `-maas`，服务账号/API Key 四配置；两语言 64 场景，24 条 JSON/SSE 成功、40 条发送前拒绝；实际适配器、本地 JWT 换取、账号、模型、URL 和输出上限 |
| `verify_custom.py` / `verify_custom.mjs` | `TestGatewayOfficialCustomSDK` | 每语言 22 条成功路径和 10 条发送前拒绝；AdvancedCustom 四协议、模型映射、路径及传输配置 |
| `verify_semantics.py` / `verify_semantics.mjs` | `TestGatewayOfficialSemanticPythonSDK` / `TestGatewayOfficialSemanticNodeSDK` | 每语言 58 场景：Chat 12、Responses 16、Anthropic 13、Gemini 17；34 条成功与 24 条拒绝，图片 JSON/SSE、结构输出、配置型搜索、custom tools、签名与私有文件 |

兼容适配器的当前记录分别运行 Python 与 TypeScript 子测试，两者均通过；日志为 `.local-tests/gateway/compatible-sdk-python.log`、`compatible-sdk-node.log`。需要定向复验时，可使用 `-run '^TestGatewayOfficialCompatibleSDK$/verify_compatible.py$'` 或对应 `.mjs` 子测试；仍保持单次 60 秒超时。

语义套件记录为 `semantic-sdk-python.log`、`semantic-sdk-node.log`。每个请求由观察端点核对上游次数、预扣会话、钱包、Token 和消费日志；拒绝路径均无变化。图片按解码字节检查，Google Python SDK 的 URL-safe base64 在跨协议 data URI 中正规化，原生请求保留。固定 Python SDK 的搜索时间区间序列化为 `start_time/end_time` 和 `+00:00`，该原生保留测试仅证明 SDK 到网关的传输契约，不证明真实供应商接受该字段形态。Chat 文件引用由测试库预先建立绑定，未新增 Files API。

云夹具保留实际适配器和 AWS SigV4/事件流处理，只隔离网络 transport；原有五配置中的 Vertex 使用 API Key。独立开源模型套件通过现场生成的 RSA 私钥和本地服务账号 JSON 验证 JWT 签名、audience 与 Token 换取，所有网络均被 transport 拦截；不读取真实账号或请求真实 OAuth。API Key 开源模型四协议均在预扣和认证前拒绝，服务账号的 Anthropic 入口同样拒绝；另有四次实际 HTTP 请求验证 Key 调序及替换不会串用账号缓存。通过记录为 `sdk-vertex-open-source.log` 和 `vertex-auth-identity.log`。

Vertex Gemini 无 ID 投影仅放行按名称仍可唯一关联的工具历史，`safe/allow` 记录损失，`strict` 拒绝 ID 丢失；必须依赖 ID 消歧的历史在所有策略下拒绝。原生 Chat/Gemini 多候选可以保留，转 Claude/Responses 在规划阶段以 `multiple_candidates_unsupported` 拒绝。思考控制的已识别不等价转换由独立计划和 HTTP 回归验证，`safe/strict` 拒绝、`allow` 诊断，Chat/Responses 可保真的 effort 保留。云 SDK 场景本身未涵盖搜索/custom tools、媒体或供应商专用推理签名。

特殊文本夹具验证 Nova 的流式请求在发送前返回 400，其他未登记协议返回 503；Coze 创建已接受后不因查询失败重新创建。Codex 仅使用本地 OAuth 格式测试 Key，所有上游及授权网络均被拦截，不会发起真实登录或刷新；账号头、后端 Responses 路径和稳定凭据续轮经过实际适配器验证，compact/alpha search 不计入本套场景。当前通过日志为 `.local-tests/gateway/sdk-special.log` 与 `sdk-codex.log`。

Realtime 的本地 TLS 测试证书只加入当前夹具与 SDK 子进程信任，不修改系统根证书，也不全局关闭 TLS 校验。Suno 不使用这些文本 SDK；续作、公开/私有任务 ID、同 Token 歌曲归属、凭据调序和失效资源边界由 `TestGatewaySunoContinuation*` 的 HTTP 回归验证。

每种语言使用独立夹具，避免前一语言产生的健康冷却影响后一语言。Go 测试会启动网关并传入随机本地地址和测试凭据；不应手动将这些脚本指向真实服务。更改 SDK 版本或流式契约后重新运行对应验收，具体支持边界见 [协议支持矩阵](../../docs/routing/GATEWAY_SUPPORT_MATRIX.md)。

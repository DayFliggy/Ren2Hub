# constant 包

该包集中维护渠道、协议、任务与上下文标识，以及宿主启动时注入的公共配置变量。它不是业务服务层。`endpoint_type.go` 通过别名复用独立 RelayKit 的协议类型，避免宿主另建一套协议枚举；其余文件不应引入数据库、HTTP handler 或业务服务依赖。

| 文件 | 用途 |
| --- | --- |
| `api_type.go` | 适配器 API 类型 |
| `channel.go` | 渠道类型及名称、默认地址等元数据 |
| `endpoint_type.go` | RelayKit EndpointType 别名和宿主专用端点常量 |
| `context_key.go` | 统一请求上下文键 |
| `cache_key.go` | 缓存命名与 Token 字段标识 |
| `env.go` | 启动阶段注入的公共配置变量 |
| `azure.go` | Azure 版本兼容边界 |
| `finish_reason.go` | 生成结束原因常量 |
| `midjourney.go` | Midjourney 动作与错误标识 |
| `task.go` | 异步任务平台、动作与模型映射 |
| `multi_key_mode.go` | 多凭据选择模式 |
| `setup.go` | 初始化状态 |
| `waffo_pay_method.go` | Waffo 支付方式结构与默认展示/参数映射 |

新增公共标识时先检查宿主与 RelayKit 是否已有定义。需要外部 I/O、数据库访问或状态协调的行为应放入相应业务包；不要为了共享名称把业务流程放进本包。新增或迁移文件时同步维护本表。

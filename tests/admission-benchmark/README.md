# 准入核心离线比较

Windows/PowerShell、Go 和独占的回环 Redis 环境下，在主项目执行：

```powershell
powershell -NoProfile -File tests/admission-benchmark/compare.ps1 -RedisAddress 127.0.0.1:16379
```

默认基线固定为 `a2d7371b2bf0a3ee50e7a69b895caa08730460b6` 的 Redis 准入核心。脚本只从本地 Git 读取 `route_lease.go`、`route_lease_queue.go`，放入独立临时包，与当前 `service.AdmissionManager` 比较；运行结束删除三个临时 Go 文件，保留 JSON 与基线 SHA。不恢复生产文件，不修改 Git，不保留旧后端配置入口。旧队列指标回调为空，以免比较依赖旧应用的全局指标；旧获取、排队、轮询和释放代码未经修改。

每种方案执行三轮：低负载 80 次/1 worker/持槽 5ms，突发 80 次/16 workers/持槽 5ms，拥塞 160 次/64 workers/持槽 20ms。容量均为 8、队列 20、等待 100ms，新方案总队列 1024。相同输入、封闭式 worker 持续提交，非恒定外部 QPS。先执行一次获取/释放预热；构建时间不计入测量。请求数守恒、新方案授槽/释放守恒、活动与等待归零，以及新方案 Redis 命令为零均有断言。只清理本次 PID 下的四个 benchmark key，不使用 FLUSHDB。

JSON 记录成功/拒绝/超时、成功获取的 p50/p95、墙钟时间、进程 CPU、Redis CPU、分配与堆内存及 Redis 客户端命令数。CPU 使用 Windows GetProcessTimes 和 Redis INFO CPU，短批次受到约 15.625ms 计时粒度影响，零值不是零成本。SQL、首个有效输出和重试不属于这个核心基准，不能从其结果推导完整网关的吞吐提升。

完整 HTTP 场景另见 `router/gateway_single_instance_load_test.go` 与 [验收记录](../../docs/routing/SINGLE_INSTANCE_ACCEPTANCE.md)。旧完整应用工作树未保存为可重建基线；本工具不能补造那份基线。

# 依赖维护

主模块、RelayKit和TokenKit分别维护Go module；Vue使用Bun与冻结锁文件。Go工作区不能替代独立模块验证。React和Electron不属于本轮交付，不保留其构建或依赖验收要求。

实际版本以 `go.mod`、各独立模块、`frontend/package.json` / `bun.lock` 和Dockerfile为准。不得把历史扫描结论当作当前依赖已安全。升级先核对真实引用、平台用途及传递链，保留必要功能并记录未消除告警，不靠忽略项或删除仍使用的依赖制造通过。

Go验证使用 `GOWORK=off`、`go mod verify`、适用的 `go vet` / `govulncheck` 和 `go test -timeout 60s`。Vue按Vitest、typecheck、lint、format:check、build及受影响浏览器场景验证。已有结果只有源码和环境相同才可复用。

项目保留AGPL及上游归属。`LICENSE`、`NOTICE`、`THIRD-PARTY-LICENSES.md` 应随实际分发维护；Docker镜像携带三份文件。依赖清单以实际构建解析图和包内许可为准，不能因上游名称或版本相近省略检查。本页不宣称本轮依赖扫描或产物许可核验已完成。

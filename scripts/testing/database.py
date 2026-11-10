#!/usr/bin/env python3
"""在本轮创建的空数据库中执行 Go 原生数据库合同；不接受外部 DSN。"""
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import signal
import select
import socket
import socketserver
import subprocess
import sys
import threading
import time

DIALECTS = ("sqlite", "mysql", "postgres")
LABEL = "io.ren2hub.local-test.run-id"
IMAGES = {"mysql": "mysql:8.4", "postgres": "postgres:15.19"}
# 显式清单使测试改名、被删除或未运行都成为失败，而非自动缩减覆盖范围。
MATRIX_TESTS = {
    "middleware": ("TestRouteTaskPluginEndpointUsesSelectedChannel",),
    "model": (
        "TestTokenRoutingDatabases", "TestTokenRouteChannelMutationLockOrderDatabases",
        "TestAuthCacheIntegrityDatabaseMatrix", "TestGmpayAndTicketDatabaseContracts",
        "TestCoreEmptyDatabaseInitialization", "TestAffiliateLedgerMigrationAndIdempotencyDatabases",
        "TestSystemTaskHistoryDatabaseMatrix", "TestAffiliateDeletedInviterDatabases",
        "TestAffiliateMonthlySeriesDatabases", "TestDeleteDisabledChannelIDsDatabases",
        "TestChannelMetricsDatabases", "TestChannelPriorityDatabaseMatrix",
        "TestInferencePresetSettingsAndDatabaseRoundTrip", "TestAuditLogSortIndexDatabaseMatrix",
        "TestMigrationSchemaStability", "TestRedemptionSortingDatabases",
        "TestRequestPolicyDatabaseMatrix", "TestAdminTicketsKeepDeletedOwnersDatabases",
        "TestTopUpQueryDatabases", "TestTopUpDiscountDatabaseMatrix", "TestChannelUpstreamRatioDatabases",
    ),
    "controller": (
        "TestModelPricingConversionDatabaseMatrix", "TestModelManagementDatabaseMatrix",
        "TestVendorManagementDatabaseMatrix", "TestModelDeletionDatabaseMatrix",
        "TestSharedModelPluginPricingDatabaseMatrix", "TestDeleteRedemptionBatch",
        "TestAuditDatabaseMatrix", "TestPreConsumePolicyDatabaseMatrix",
        "TestRequestPolicyRoutingDatabaseMatrix", "TestLogExportDatabaseMatrix",
        "TestAPITokenAuditDatabaseMatrix",
    ),
    "pkg/perf_metrics": ("TestPerformanceAggregationAndFlush",),
    "service": ("TestFixedPriceBillingDatabaseMatrix", "TestTaskPollingRefundDatabaseMatrix"),
}
SINGLE_DIALECT_TESTS = (
    ("model", "TestMigratePrefillGroupUniquenessMySQL", "mysql"),
    ("model", "TestMigratePrefillGroupUniquenessPostgreSQL", "postgres"),
    ("model", "TestMigrateTokenKeyUniquenessMySQL", "mysql"),
    ("model", "TestMigrateTokenKeyUniquenessPostgreSQL", "postgres"),
    ("controller", "TestTokenMigrationFromChar48ToVarchar128MySQL", "mysql"),
    ("controller", "TestTokenMigrationFromChar48ToVarchar128Postgres", "postgres"),
)
MANAGE_TESTS = (
    "TestManageUserQuotaRecordsTopupAndAudit", "TestManageUserQuotaFailuresDoNotRecordTopup",
    "TestManageUserQuotaLogFailureKeepsSuccessfulAdjustment", "TestManageUserQuotaTargetsAndWalletBounds",
    "TestManageUserQuotaMiddlewareKeepsOneOperationPerRequest", "TestManageUserQuotaConcurrentSnapshots",
    "TestManageUserQuotaCacheUsesCommittedIntegerDifference",
)
SECURITY_TESTS = (
    "TestSecurityEnrollmentAccessTokenLifecycleConsumesProofs",
    "TestSecurityEnrollmentProofConcurrentConsumption",
    "TestSecurityEnrollmentProofIsBurnedAfterBusinessFailure",
    "TestSecurityEnrollmentPendingPasskeyRejectsChangedAuthorization",
)
CHANNEL_TESTS = (
    "TestAddChannelReturnsCreatedIDs", "TestAddChannelInvalidVertexBatchDoesNotReturnResults",
    "TestAddChannelRollbackDoesNotReturnIDs", "TestAddChannelPanicRollsBackWithoutSuccess",
    "TestMultiKeyEnableRestoresOnlyExhaustedChannels",
)
TASK_TESTS = (
    "TestDoubaoRelayedImageStreamSettlement", "TestImmediateTaskSettlementDatabase",
    "TestExecuteTaskSubmissionRefundsWhenInsertFails", "TestExecuteTaskSubmissionRefundsWhenFinalReserveFails",
    "TestExecuteTaskSubmissionCallerCancellationDuringSubmitRefundsBeforeDurableBarrier",
    "TestProxyTaskMediaReadsArtifactFromJSONContentPath",
    "TestTaskPluginChangesReportPricingIssuesOfServingPlugin", "TestSwitchedOffPluginPricesAreNotPricingIssues",
    "TestTaskPluginListAndDetailReportSkippedRoutes", "TestTaskPluginSwitchOnPathsRejectRoutingConflicts",
)
PAIRED_TESTS = (
    ("model", "TestUserSessionPreviousRefreshHashMigrationConfiguredDatabases"),
    ("controller", "TestIndependentAuditLogStores"),
)
REQUIRED_TESTS = tuple(name for tests in MATRIX_TESTS.values() for name in tests) + tuple(
    name for _, name, _ in SINGLE_DIALECT_TESTS
) + MANAGE_TESTS + SECURITY_TESTS + CHANNEL_TESTS + TASK_TESTS + tuple(name for _, name in PAIRED_TESTS)


# 这些 helper 的现有单测默认使用临时 SQLite；逐项报告其外部引擎变体未覆盖。
DEFERRED_DATABASE_HELPERS = {
    "setupResponsesWSRequestTest": "WebSocket 集成原生单测使用临时 SQLite；MySQL/PostgreSQL 变体未纳入本矩阵",
    "setupSecurityEnrollmentTest": "安全注册原生单测使用临时 SQLite；MySQL/PostgreSQL 变体未纳入本矩阵",
    "openTaskDialectDatabase": "任务插件原生单测使用临时 SQLite；MySQL/PostgreSQL 变体未纳入本矩阵",
    "openTaskDialectDatabaseNamed": "任务插件原生单测使用临时 SQLite；MySQL/PostgreSQL 变体未纳入本矩阵",
    "setupManageUserTestDB": "非额度管理原生单测使用临时 SQLite；MySQL/PostgreSQL 变体未纳入本矩阵",
}
DEFERRED_DATABASE_TESTS = {}


def database_inventory(source: Path, tracked_files: list[str]) -> dict:
    """发现新增数据库测试和 helper 调用，显式报告未归类的覆盖范围。"""
    functions = {}
    for relative in tracked_files:
        if not relative.endswith("_test.go"):
            continue
        path = Path(relative)
        if "developer-notes" in path.parts:
            continue
        content = (source / path).read_text()
        package = str(path.parent)
        # Go 测试遵循 gofmt；顶层闭括号位于列首，内层块均有缩进。
        for match in re.finditer(r"^func (\w+)\([^\n]*\{(.*?)^}", content, re.M | re.S):
            functions[(package, match[1])] = match[2]
    external = {key for key, body in functions.items()
                if "os.Getenv" in body and ("DSN" in body or "DIALECT" in body)}
    calls = {key: {(key[0], name) for name in re.findall(r"\b(\w+)\s*\(", body)
                   if (key[0], name) in functions} for key, body in functions.items()}
    result = {"covered": [], "deferred": [], "unclassified": []}
    for key in sorted(functions):
        if not key[1].startswith("Test"):
            continue
        visited, pending = set(), [key]
        while pending:
            current = pending.pop()
            if current not in visited:
                visited.add(current)
                pending.extend(calls[current] - visited)
        roots = sorted(name for package, name in external & visited)
        if not roots:
            continue
        item = {"package": key[0], "test": key[1], "environment_functions": roots}
        if key[1] in REQUIRED_TESTS:
            result["covered"].append(item)
        elif key[1] in DEFERRED_DATABASE_TESTS:
            item["reason"] = DEFERRED_DATABASE_TESTS[key[1]]
            result["deferred"].append(item)
        elif all(name in DEFERRED_DATABASE_HELPERS for name in roots):
            item["reason"] = "; ".join(DEFERRED_DATABASE_HELPERS[name] for name in roots)
            result["deferred"].append(item)
        else:
            result["unclassified"].append(item)
    existing = {name for _, name in functions}
    result["missing_required"] = sorted(set(REQUIRED_TESTS) - existing)
    return result


def database_test_files(source: Path) -> list[str]:
    """Include tracked and non-ignored working-tree tests for dirty candidates."""
    result = subprocess.run(
        ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z",
         "--", "*_test.go", ":(exclude)docs/developer-notes/**"],
        cwd=source, check=True, stdout=subprocess.PIPE, text=False,
    )
    return [path for path in result.stdout.decode().split("\0") if path]


class Failure(RuntimeError):
    pass


def clean_environment(environ: dict[str, str]) -> dict[str, str]:
    """拒绝外部连接配置，再用白名单构造子进程环境。错误只列键名。"""
    forbidden = sorted(key for key, value in environ.items() if value and (
        "DSN" in key or key.startswith(("TEST_", "DOCKER_", "MYSQL_", "POSTGRES_", "PG", "REDIS_"))
        or "_TEST_" in key or key in ("SQLITE_PATH", "SESSION_SECRET", "CRYPTO_SECRET")
    ))
    if forbidden:
        raise Failure("拒绝继承外部连接或测试配置: " + ", ".join(forbidden))
    allowed = ("PATH", "HOME", "USER", "LOGNAME", "LANG", "LC_ALL", "TZ", "GOPATH", "GOCACHE", "GOMODCACHE")
    env = {key: environ[key] for key in allowed if key in environ}
    env.update(GOENV="off", GOWORK="off", GOPROXY="off", GOTOOLCHAIN="local", GOSUMDB="off", CGO_ENABLED="0", GOMAXPROCS="2", GOFLAGS="")
    return env


def private_directory(path: Path) -> Path:
    path = path.absolute()
    if path.is_symlink() or any(parent.is_symlink() for parent in path.parents):
        raise Failure("报告目录及其父级不可为符号链接")
    path.mkdir(mode=0o700, parents=True, exist_ok=True)
    if path.stat().st_uid != os.getuid():
        raise Failure("报告目录必须属于当前用户")
    path.chmod(0o700)
    if (path / "database-report.json").exists():
        raise Failure("报告目录已包含数据库报告，请为新一轮选择空目录")
    return path


def verify_events(content: str, test: str, dialects: tuple[str, ...], nested: bool) -> dict:
    events = []
    for line in content.splitlines():
        try:
            event = json.loads(line)
        except json.JSONDecodeError:
            continue  # Go 在构建失败时可能输出普通文本；缺少 pass 仍然失败。
        if isinstance(event, dict):
            events.append(event)
    selected = [event for event in events if event.get("Test", "").split("/")[0] == test]
    skipped = sorted({event["Test"] for event in selected if event.get("Action") == "skip"})
    failed = sorted({event["Test"] for event in selected if event.get("Action") == "fail"})
    ran = {event["Test"] for event in selected if event.get("Action") == "run"}
    passed = {event["Test"] for event in selected if event.get("Action") == "pass"}
    missing = []
    if test not in ran or test not in passed:
        missing.append(test)
    if nested:
        for dialect in dialects:
            prefix = test + "/" + dialect
            if not any(name in ran and (name == prefix or name.startswith(prefix + "/")) for name in passed):
                missing.append(prefix)
    return {"success": not (skipped or failed or missing), "passed_tests": len(passed),
            "skipped": skipped, "failed": failed, "missing": missing, "dialects": list(dialects)}


class LoopbackProxy(socketserver.ThreadingTCPServer):
    """Docker internal 网络不发布端口；宿主仅经本轮 loopback 转发访问。"""
    daemon_threads = True
    allow_reuse_address = False

    def __init__(self, target: tuple[str, int]):
        self.target = target
        self.stopping = threading.Event()
        self.errors: list[str] = []
        super().__init__(("127.0.0.1", 0), ProxyConnection)
        self.thread = threading.Thread(target=self.serve_forever, kwargs={"poll_interval": 0.1}, daemon=True)
        self.thread.start()

    def close(self) -> None:
        self.stopping.set()
        self.shutdown()
        self.server_close()
        self.thread.join(timeout=2)


class ProxyConnection(socketserver.BaseRequestHandler):
    def handle(self) -> None:
        try:
            with socket.create_connection(self.server.target, timeout=5) as upstream:
                # 转发会把一条 SQL 报文拆成多次小写入；Nagle 遇上对端延迟 ACK，
                # 每次往返多等约 40ms，200 行锁序用例因此超过 15 秒预算。
                for endpoint in (self.request, upstream):
                    endpoint.setsockopt(socket.IPPROTO_TCP, socket.TCP_NODELAY, 1)
                upstream.settimeout(5)
                self.request.settimeout(5)
                while not self.server.stopping.is_set():
                    readable, _, _ = select.select([self.request, upstream], [], [], 0.5)
                    for origin in readable:
                        data = origin.recv(65536)
                        if not data:
                            return
                        destination = upstream if origin is self.request else self.request
                        destination.sendall(data)
        except (ConnectionResetError, BrokenPipeError):
            # SQL 客户端取消/关闭连接会终止代理流，不代表运行器错误。
            return
        except OSError as error:
            self.server.errors.append(type(error).__name__ + ": " + str(error))


class Runner:
    def __init__(self, source: Path, output: Path, env: dict[str, str]):
        self.source, self.output, self.env = source, output, env
        self.run_id = secrets.token_hex(8)
        self.prefix = "ren2hub-local-test-" + self.run_id
        self.password = secrets.token_hex(24)
        self.docker: list[str] = []
        self.resources: list[tuple[str, str]] = []
        self.pending_resources: list[tuple[str, str]] = []
        self.containers: dict[str, str] = {}
        self.ports: dict[str, int] = {}
        self.proxies: list[LoopbackProxy] = []
        self.sequence = 0
        self.report = {"success": False, "run_id": self.run_id, "suites": [], "cleanup_errors": [],
                       "scope": "全新 SQLite/MySQL/PostgreSQL；不含 ClickHouse、外部供应商或生产验证"}

    def redact(self, text: str) -> str:
        text = text.replace(self.password, "[REDACTED]")
        return re.sub(r"(?i)(postgres(?:ql)?://[^:\s]+:)[^@\s]+(@)", r"\1[REDACTED]\2", text)

    def write(self, name: str, content: str) -> None:
        path = self.output / name
        # 不追随已有文件，避免 CLI 输出路径写入调用者未指定的位置。
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, "w") as handle:
            os.fchmod(handle.fileno(), 0o600)
            handle.write(self.redact(content))

    def command(self, args: list[str], *, env: dict[str, str] | None = None,
                timeout: int = 30, check: bool = True, input_text: str | None = None) -> subprocess.CompletedProcess:
        process = subprocess.Popen(args, cwd=self.source, env=env or self.env, text=True,
                                   stdin=subprocess.PIPE if input_text is not None else subprocess.DEVNULL,
                                   stdout=subprocess.PIPE, stderr=subprocess.STDOUT, start_new_session=True)
        try:
            output, _ = process.communicate(input_text, timeout=timeout)
        except (subprocess.TimeoutExpired, KeyboardInterrupt) as error:
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass  # 进程可能已退出；仍收集输出并重新抛出原中断/超时。
            output, _ = process.communicate()
            self.write("interrupted-command.log", output)
            if isinstance(error, subprocess.TimeoutExpired):
                error.output = output
            raise
        result = subprocess.CompletedProcess(args, process.returncode, output)
        if check and result.returncode:
            raise Failure(self.redact(f"命令失败（exit={result.returncode}）: {args[0]}\n{output[-4000:]}"))
        return result

    def docker_call(self, *args: str, **kwargs) -> subprocess.CompletedProcess:
        return self.command(self.docker + list(args), **kwargs)

    def prepare(self) -> None:
        if not (self.source / "go.mod").is_file():
            raise Failure("--source 必须指向 Ren2Hub Go 模块")
        for tool in ("docker", "go"):
            if not shutil.which(tool, path=self.env.get("PATH")):
                raise Failure("缺少工具: " + tool)
        version = self.command(["go", "version"]).stdout.strip()
        self.report["go_version"] = version
        tests = database_test_files(self.source)
        self.report["discovery"] = database_inventory(self.source, tests)
        if self.report["discovery"]["unclassified"] or self.report["discovery"]["missing_required"]:
            raise Failure("发现未归类或缺失的数据库测试；见报告 discovery，拒绝缩减覆盖范围")
        # 固定本机 socket，绕开 Docker context 和 DOCKER_HOST 的远程目标。
        self.docker = ["docker", "--host", "unix:///var/run/docker.sock"]
        check = self.docker_call("info", "--format", "{{json .ServerVersion}}", check=False)
        if "permission denied" in check.stdout.lower():
            self.report["docker_access"] = "普通用户 socket 权限不足；使用 sudo -n docker"
            self.docker = ["sudo", "-n"] + self.docker
            check = self.docker_call("info", "--format", "{{json .ServerVersion}}")
        try:
            server_version = json.loads(check.stdout.strip())
        except json.JSONDecodeError as error:
            raise Failure("Docker info 未返回有效版本: " + check.stdout[-1000:]) from error
        if check.returncode or not isinstance(server_version, str) or not server_version:
            raise Failure("本机 Docker 服务不可用")
        self.report["docker_version"] = server_version
        self.report["images"] = {}
        for kind, image in IMAGES.items():
            detail = json.loads(self.docker_call("image", "inspect", image).stdout)[0]
            self.report["images"][kind] = {"tag": image, "id": detail["Id"], "repo_digests": detail.get("RepoDigests", [])}
        self.pending_resources.append(("network", self.prefix))
        network = self.docker_call("network", "create", "--internal", "--label", LABEL + "=" + self.run_id, self.prefix).stdout.strip()
        self.track("network", network)
        self.pending_resources.remove(("network", self.prefix))
        detail = json.loads(self.docker_call("network", "inspect", network).stdout)[0]
        if detail.get("Internal") is not True:
            raise Failure("数据库网络未开启 internal 隔离")
        self.report["database_transport"] = "internal Docker 网络；宿主 127.0.0.1 随机端口转发；无 Docker 发布端口"
        for kind in IMAGES:
            self.create_server(kind, self.report["images"][kind]["id"], network)
        for kind in IMAGES:
            self.wait_ready(kind)
            self.report["images"][kind]["database_version"] = self.sql(kind, "SELECT version();").stdout.strip()

    def track(self, kind: str, resource_id: str) -> None:
        if not re.fullmatch(r"[0-9a-f]{64}", resource_id):
            raise Failure("Docker 未返回完整资源 ID，拒绝使用名称清理")
        self.resources.append((kind, resource_id))
        self.write("resources.json", json.dumps({"run_id": self.run_id, "resources": self.resources}, indent=2) + "\n")

    def create_server(self, kind: str, image: str, network: str) -> None:
        port, data, size = (3306, "/var/lib/mysql", "768m") if kind == "mysql" else (5432, "/var/lib/postgresql/data", "512m")
        env = dict(self.env)
        if kind == "mysql":
            env.update(MYSQL_ROOT_PASSWORD=self.password, MYSQL_ROOT_HOST="%")
            variables = ("MYSQL_ROOT_PASSWORD", "MYSQL_ROOT_HOST")
        else:
            env.update(POSTGRES_PASSWORD=self.password, POSTGRES_USER="ren2hub", POSTGRES_DB="postgres")
            variables = ("POSTGRES_PASSWORD", "POSTGRES_USER", "POSTGRES_DB")
        args = ["create", "--pull=never", "--name", self.prefix + "-" + kind,
                "--network", network, "--label", LABEL + "=" + self.run_id,
                "--cpus", "2", "--memory", "1g",
                "--tmpfs", f"{data}:rw,nosuid,size={size}"]
        # sudo 会过滤进程环境；经 stdin 交给 Docker，不把密码放入 argv 或磁盘。
        args += ["--env-file", "/dev/stdin", image]
        self.pending_resources.append(("container", self.prefix + "-" + kind))
        container = self.docker_call(*args, input_text="".join(key + "=" + env[key] + "\n" for key in variables)).stdout.strip()
        self.track("container", container)
        self.pending_resources.remove(("container", self.prefix + "-" + kind))
        self.containers[kind] = container
        self.docker_call("start", container)
        detail = json.loads(self.docker_call("container", "inspect", container).stdout)[0]
        networks = detail["NetworkSettings"]["Networks"]
        if len(networks) != 1 or next(iter(networks.values()))["NetworkID"] != network:
            raise Failure("数据库容器连接了本轮以外的网络")
        if detail["HostConfig"].get("PortBindings"):
            raise Failure("数据库容器存在意外发布的端口")
        address = next(iter(networks.values()))["IPAddress"]
        if not address:
            raise Failure("数据库容器没有内部 IP")
        proxy = LoopbackProxy((address, port))
        self.proxies.append(proxy)
        self.ports[kind] = proxy.server_address[1]

    def sql(self, kind: str, statement: str, *, check: bool = True) -> subprocess.CompletedProcess:
        if kind == "mysql":
            # 密码只存在容器初始化环境中，shell 展开不进入宿主命令行。
            args = ("exec", "-i", self.containers[kind], "sh", "-c",
                    'export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"; exec mysql --host=127.0.0.1 --protocol=TCP --user=root --batch --skip-column-names')
        else:
            args = ("exec", "-i", self.containers[kind], "sh", "-c",
                    'export PGPASSWORD="$POSTGRES_PASSWORD"; exec psql --host=127.0.0.1 --username=ren2hub --dbname=postgres --no-psqlrc --set=ON_ERROR_STOP=1 --tuples-only --no-align')
        return self.docker_call(*args, input_text=statement + "\n", check=check, timeout=10)

    def wait_ready(self, kind: str) -> None:
        deadline = time.monotonic() + 60
        consecutive = 0
        while time.monotonic() < deadline:
            result = self.sql(kind, "SELECT 1;", check=False)
            consecutive = consecutive + 1 if result.returncode == 0 and result.stdout.strip() == "1" else 0
            if consecutive == 2:
                return
            time.sleep(1)
        self.write(kind + "-readiness.log", result.stdout)
        raise Failure(kind + " 未在 60 秒内就绪，见 readiness 日志")

    def database_environment(self) -> dict[str, str]:
        self.sequence += 1
        env = dict(self.env)
        for kind in IMAGES:
            for suffix in ("main", "log"):
                name = f"rtest_{self.sequence}_{suffix}"
                clause = " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci" if kind == "mysql" else ""
                self.sql(kind, "CREATE DATABASE " + name + clause + ";")
                if kind == "mysql":
                    dsn = f"root:{self.password}@tcp(127.0.0.1:{self.ports[kind]})/{name}?charset=utf8mb4&parseTime=True&loc=UTC"
                else:
                    dsn = f"postgres://ren2hub:{self.password}@127.0.0.1:{self.ports[kind]}/{name}?sslmode=disable"
                if suffix == "main":
                    for prefix in ("TEST_", "ROUTING_TEST_", "REN2HUB_EXTENSION_TEST_", "CORE_STARTUP_", "TEST_PERF_", "TEST_FIXED_", "AUDIT_", "TEST_AFFILIATE_"):
                        env[prefix + kind.upper() + "_DSN"] = dsn
                else:
                    env["TEST_" + kind.upper() + "_LOG_DSN"] = dsn
                    env["TEST_FIXED_" + kind.upper() + "_LOG_DSN"] = dsn
        return env

    def database_names(self, kind: str) -> set[str]:
        query = "SHOW DATABASES;" if kind == "mysql" else "SELECT datname FROM pg_database;"
        return set(self.sql(kind, query).stdout.split())

    def drop_suite_databases(self, before: dict[str, set[str]]) -> None:
        # 仅本轮容器内的名称差集；原生 controller helper 也会创建独立数据库。
        for kind, original in before.items():
            container = self.containers[kind]
            data = json.loads(self.docker_call("container", "inspect", container).stdout)[0]
            if data.get("Id") != container or data.get("Config", {}).get("Labels", {}).get(LABEL) != self.run_id:
                raise Failure("容器身份变化，拒绝清理测试库")
            for name in self.database_names(kind) - original:
                if not re.fullmatch(r"(?:rtest_[0-9]+_(?:main|log)|newapi_audit_[0-9]+)", name):
                    raise Failure("拒绝清理非本地测试命名的数据库: " + name)
                suffix = " WITH (FORCE)" if kind == "postgres" else ""
                self.sql(kind, "DROP DATABASE " + name + suffix + ";")

    def suite(self, package: str, test: str, dialects: tuple[str, ...] = DIALECTS,
              *, nested: bool = True, manage: bool = False, security: bool = False,
              channel: bool = False, task: bool = False) -> None:
        before = {kind: self.database_names(kind) for kind in IMAGES}
        env = self.database_environment()
        if manage:
            env["TEST_MANAGE_USER_DIALECT"] = dialects[0]
        if security:
            env["TEST_SECURITY_DIALECT"] = dialects[0]
        if channel:
            env["TEST_CHANNEL_SQL_DSN"] = '' if dialects[0] == 'sqlite' else env['TEST_' + dialects[0].upper() + '_DSN']
        if task:
            env["TEST_TASK_DB_DIALECT"] = dialects[0]
        pattern = "^" + test + "$"
        if nested:
            pattern += "/^(" + "|".join(dialects) + ")$"
        args = ["go", "test", "-json", "-buildvcs=false", "-p", "2", "-count=1", "-timeout=60s", "./" + package, "-run", pattern]
        name = f"{self.sequence:02d}-{test}" + ("-" + dialects[0] if manage or security or channel or task else "")
        started = time.monotonic()
        entry = {"package": package, "test": test, "dialects": list(dialects), "log": name + ".jsonl", "success": False}
        self.report["suites"].append(entry)
        try:
            result = self.command(args, env=env, timeout=60, check=False)
            self.write(name + ".jsonl", result.stdout)
            entry.update(verify_events(result.stdout, test, dialects, nested))
            entry["exit_code"] = result.returncode
            entry["success"] = entry["success"] and result.returncode == 0
        except subprocess.TimeoutExpired as error:
            self.write(name + ".jsonl", error.output or "")
            entry["error"] = "Go 测试超过 60 秒，已终止进程组"
        finally:
            try:
                self.drop_suite_databases(before)
            except (Failure, OSError, subprocess.TimeoutExpired, ValueError, KeyError) as error:
                entry["success"] = False
                entry["database_cleanup_error"] = self.redact(str(error))
                raise
            finally:
                entry["elapsed_seconds"] = round(time.monotonic() - started, 3)
            self.write("database-progress.json", json.dumps(self.report, ensure_ascii=False, indent=2) + "\n")

    def execute(self) -> None:
        for package, tests in MATRIX_TESTS.items():
            for test in tests:
                # The lock matrix includes 23 scenarios per engine. Keep each
                # backend invocation bounded at 60 seconds without aggregating
                # three independent engines into one timeout budget.
                if test == "TestTokenRouteChannelMutationLockOrderDatabases":
                    for dialect in DIALECTS:
                        self.suite(package, test, (dialect,))
                else:
                    self.suite(package, test)
        for package, test in PAIRED_TESTS:
            self.suite(package, test, ("mysql", "postgres"))
        for package, test, dialect in SINGLE_DIALECT_TESTS:
            self.suite(package, test, (dialect,), nested=False)
        for test in MANAGE_TESTS:
            for dialect in DIALECTS:
                self.suite("controller", test, (dialect,), nested=False, manage=True)
        for test in SECURITY_TESTS:
            for dialect in DIALECTS:
                self.suite("controller", test, (dialect,), nested=False, security=True)
        for test in CHANNEL_TESTS:
            for dialect in DIALECTS:
                self.suite("controller", test, (dialect,), nested=False, channel=True)
        for test in TASK_TESTS:
            for dialect in DIALECTS:
                self.suite("controller", test, (dialect,), nested=False, task=True)
        self.report["success"] = all(entry["success"] for entry in self.report["suites"])

    def cleanup(self) -> None:
        # create 的客户端被中断时，daemon 可能已经成功创建；用本轮随机名称
        # 找回完整 ID 并核对 label，后续删除仍只使用 ID。
        for kind, name in self.pending_resources:
            try:
                result = self.docker_call(kind, "inspect", name, check=False)
                if result.returncode and ("no such" in result.stdout.lower() or "not found" in result.stdout.lower()):
                    continue
                if result.returncode:
                    raise Failure("无法确认中断创建的资源: " + name)
                detail = json.loads(result.stdout)[0]
                labels = detail.get("Config", {}).get("Labels", {}) if kind == "container" else detail.get("Labels", {})
                if labels.get(LABEL) != self.run_id:
                    raise Failure("中断资源的 run-id 不匹配，拒绝清理 " + name)
                resource = (kind, detail["Id"])
                if resource not in self.resources:
                    self.track(*resource)
            except (Failure, OSError, subprocess.TimeoutExpired, ValueError, IndexError, KeyError) as error:
                self.report["cleanup_errors"].append(self.redact(str(error)))
        for proxy in self.proxies:
            proxy.close()
            if proxy.errors:
                self.report["cleanup_errors"].extend(proxy.errors)
        for kind, resource_id in reversed(self.resources):
            try:
                data = json.loads(self.docker_call(kind, "inspect", resource_id).stdout)[0]
                labels = data.get("Config", {}).get("Labels", {}) if kind == "container" else data.get("Labels", {})
                if data.get("Id") != resource_id or labels.get(LABEL) != self.run_id:
                    raise Failure("资源 ID 或 run-id 不匹配，拒绝清理 " + resource_id)
                if kind == "container":
                    logs = self.docker_call("logs", resource_id, check=False)
                    self.write(resource_id[:12] + "-container.log", logs.stdout)
                    if logs.returncode:
                        self.report["cleanup_errors"].append("读取容器日志失败: " + resource_id)
                    self.docker_call("container", "rm", "--force", "--volumes", resource_id)
                else:
                    self.docker_call("network", "rm", resource_id)
            except (Failure, OSError, subprocess.TimeoutExpired, ValueError, IndexError, KeyError) as error:
                self.report["cleanup_errors"].append(self.redact(str(error)))
        if self.report["cleanup_errors"]:
            self.report["success"] = False


def interrupt_run(signum, frame) -> None:
    raise KeyboardInterrupt


def run(source: Path, output: Path) -> dict:
    output = private_directory(output)
    runner = Runner(source.resolve(), output, {})
    started = time.monotonic()
    previous_sigterm = None
    if threading.current_thread() is threading.main_thread():
        previous_sigterm = signal.signal(signal.SIGTERM, interrupt_run)
    try:
        runner.env = clean_environment(dict(os.environ))
        runner.prepare()
        runner.execute()
    except KeyboardInterrupt:
        runner.report["error"] = "执行被中断；资源清理结果见 cleanup_errors"
        runner.report["interrupted"] = True
    except (Failure, OSError, subprocess.TimeoutExpired, ValueError, KeyError) as error:
        runner.report["error"] = runner.redact(str(error))
    finally:
        runner.cleanup()
        runner.report["elapsed_seconds"] = round(time.monotonic() - started, 3)
        runner.report["passed_suites"] = sum(item["success"] for item in runner.report["suites"])
        runner.report["total_suites"] = len(runner.report["suites"])
        runner.write("database-report.json", json.dumps(runner.report, ensure_ascii=False, indent=2) + "\n")
        if previous_sigterm is not None:
            signal.signal(signal.SIGTERM, previous_sigterm)
    return runner.report


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", type=Path, default=Path(__file__).resolve().parents[2])
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args(argv)
    try:
        report = run(args.source, args.output)
    except (Failure, OSError) as error:
        print(str(error), file=sys.stderr)
        return 1
    print(json.dumps({"success": report["success"], "passed_suites": report["passed_suites"],
                      "total_suites": report["total_suites"], "report": str(args.output / "database-report.json")}, ensure_ascii=False))
    return 0 if report["success"] else 1


if __name__ == "__main__":
    raise SystemExit(main())

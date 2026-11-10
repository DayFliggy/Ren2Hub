"""本地数据库执行器的安全边界；不依赖 Docker。"""
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import socket
import threading
import tempfile
import time
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("database", Path(__file__).with_name("database.py"))
database = importlib.util.module_from_spec(spec)
spec.loader.exec_module(database)


def events(test, actions):
    return "\n".join(json.dumps({"Test": test + suffix, "Action": action}) for suffix, action in actions)


class DatabaseRunnerTests(unittest.TestCase):
    def test_lock_matrix_runs_every_engine_even_after_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            runner = database.Runner(Path(directory), Path(directory), {})
            observed = []
            def suite(package, test, dialects=database.DIALECTS, **kwargs):
                if test == "TestTokenRouteChannelMutationLockOrderDatabases":
                    observed.append(dialects)
                runner.report["suites"].append({"success": dialects != ("mysql",)})
            with patch.object(runner, "suite", side_effect=suite):
                runner.execute()
            self.assertEqual(observed, [("sqlite",), ("mysql",), ("postgres",)])
            self.assertFalse(runner.report["success"])

    def test_shared_endpoint_routing_runs_in_the_database_matrix(self):
        self.assertIn(
            "TestRouteTaskPluginEndpointUsesSelectedChannel",
            database.REQUIRED_TESTS,
        )

    def test_database_inventory_includes_untracked_nonignored_tests(self):
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory)
            subprocess.run(["git", "init", "-q"], cwd=source, check=True)
            (source / "tracked_test.go").write_text("package test\n")
            subprocess.run(["git", "add", "tracked_test.go"], cwd=source, check=True)
            untracked = source / "new_test.go"
            untracked.write_text("package test\n")
            ignored = source / "ignored_test.go"
            ignored.write_text("package test\n")
            (source / ".gitignore").write_text("ignored_test.go\n")
            actual = set(database.database_test_files(source))
            self.assertEqual(actual, {"tracked_test.go", "new_test.go"})

    def test_external_environment_rejected_without_secret_in_error(self):
        for name in ("SQL_DSN", "LOG_SQL_DSN", "TEST_MYSQL_DSN", "AUDIT_POSTGRES_DSN", "DOCKER_HOST", "TEST_MANAGE_USER_DIALECT", "NEW_API_VLLM_TEST_URL"):
            with self.subTest(name=name), self.assertRaises(database.Failure) as caught:
                database.clean_environment({"PATH": "/bin", name: "private-production-value"})
            self.assertIn(name, str(caught.exception))
            self.assertNotIn("private-production-value", str(caught.exception))

    def test_child_environment_is_allowlisted_and_offline(self):
        env = database.clean_environment({"PATH": "/bin", "HOME": "/tmp/home", "API_KEY": "secret", "HTTP_PROXY": "http://elsewhere", "GOFLAGS": "-exec remote", "GOPROXY": "https://remote"})
        self.assertNotIn("API_KEY", env)
        self.assertNotIn("HTTP_PROXY", env)
        self.assertEqual(env["GOFLAGS"], "")
        self.assertEqual(env["GOPROXY"], "off")
        self.assertEqual(env["GOTOOLCHAIN"], "local")
        self.assertEqual(env["GOENV"], "off")

    def test_missing_or_skipped_dialect_never_passes(self):
        name = "TestDatabase"
        partial = events(name, [("", "run"), ("/sqlite", "run"), ("/sqlite", "pass"), ("/mysql", "run"), ("/mysql", "skip"), ("", "pass")])
        result = database.verify_events(partial, name, database.DIALECTS, True)
        self.assertFalse(result["success"])
        self.assertIn(name + "/mysql", result["skipped"])
        self.assertIn(name + "/postgres", result["missing"])
        self.assertFalse(database.verify_events("", name, (), False)["success"])

    def test_nested_dialect_test_events_are_required(self):
        name = "TestDatabase"
        actions = [("", "run"), ("", "pass")]
        for kind in database.DIALECTS:
            actions += [("/" + kind + "/UTC", "run"), ("/" + kind + "/UTC", "pass")]
        self.assertTrue(database.verify_events(events(name, actions), name, database.DIALECTS, True)["success"])
        self.assertFalse(database.verify_events(events(name, actions + [("/mysql/failure", "fail")]), name, database.DIALECTS, True)["success"])

    def test_cleanup_requires_full_matching_id_and_label(self):
        with tempfile.TemporaryDirectory() as directory:
            runner = database.Runner(Path(directory), Path(directory), {})
            identity = "a" * 64
            runner.resources = [("container", identity)]
            for observed_id, label in (("b" * 64, runner.run_id), (identity, "another-run")):
                runner.report["cleanup_errors"] = []
                detail = [{"Id": observed_id, "Config": {"Labels": {database.LABEL: label}}}]
                with patch.object(runner, "docker_call", return_value=subprocess.CompletedProcess([], 0, json.dumps(detail))) as call:
                    runner.cleanup()
                self.assertEqual(call.call_count, 1)
                self.assertTrue(runner.report["cleanup_errors"])
                self.assertFalse(runner.report["success"])

    def test_cleanup_removes_only_registered_ids_in_reverse_order(self):
        with tempfile.TemporaryDirectory() as directory:
            runner = database.Runner(Path(directory), Path(directory), {})
            network, container = "a" * 64, "b" * 64
            runner.resources = [("network", network), ("container", container)]
            calls = []
            def fake(*args, **kwargs):
                calls.append(args)
                if "inspect" in args:
                    detail = {"Id": args[-1], "Labels": {database.LABEL: runner.run_id}, "Config": {"Labels": {database.LABEL: runner.run_id}}}
                    return subprocess.CompletedProcess([], 0, json.dumps([detail]))
                return subprocess.CompletedProcess([], 0, "")
            with patch.object(runner, "docker_call", side_effect=fake):
                runner.cleanup()
            self.assertEqual([call for call in calls if "rm" in call], [("container", "rm", "--force", "--volumes", container), ("network", "rm", network)])
            self.assertFalse(runner.report["cleanup_errors"])

    def test_partial_prepare_failure_and_interrupt_always_cleanup(self):
        for error in (database.Failure("creation failed"), KeyboardInterrupt()):
            with self.subTest(error=type(error).__name__), tempfile.TemporaryDirectory() as directory:
                output = Path(directory) / "result"
                def partial(runner):
                    runner.resources.append(("network", "a" * 64))
                    raise error
                with patch.dict(os.environ, {"PATH": "/bin"}, clear=True), patch.object(database.Runner, "prepare", partial), patch.object(database.Runner, "cleanup") as cleanup:
                    report = database.run(Path(directory), output)
                cleanup.assert_called_once()
                self.assertFalse(report["success"])
                self.assertTrue((output / "database-report.json").exists())
                self.assertEqual(output.stat().st_mode & 0o777, 0o700)
                self.assertEqual((output / "database-report.json").stat().st_mode & 0o777, 0o600)

    def test_external_environment_stops_before_docker(self):
        with tempfile.TemporaryDirectory() as directory, patch.dict(os.environ, {"SQL_DSN": "private"}, clear=True), patch.object(database.Runner, "prepare") as prepare:
            report = database.run(Path(directory), Path(directory) / "out")
            self.assertFalse(report["success"])
            prepare.assert_not_called()
            self.assertNotIn("private", json.dumps(report))

    def test_cli_exits_nonzero_on_failure(self):
        with patch.object(database, "run", return_value={"success": False, "passed_suites": 0, "total_suites": 1}), patch("builtins.print"):
            self.assertEqual(database.main(["--output", "/tmp/test-output"]), 1)

    def test_interrupted_create_recovers_id_before_cleanup(self):
        with tempfile.TemporaryDirectory() as directory:
            runner = database.Runner(Path(directory), Path(directory), {})
            runner.pending_resources = [("network", runner.prefix)]
            identity = "a" * 64
            detail = [{"Id": identity, "Labels": {database.LABEL: runner.run_id}}]
            def fake(*args, **kwargs):
                return subprocess.CompletedProcess([], 0, json.dumps(detail) if "inspect" in args else "")
            with patch.object(runner, "docker_call", side_effect=fake) as call:
                runner.cleanup()
            self.assertIn(unittest.mock.call("network", "rm", identity), call.call_args_list)
            self.assertNotIn(unittest.mock.call("network", "rm", runner.prefix), call.call_args_list)
            self.assertFalse(runner.report["cleanup_errors"])

    def test_suite_cleanup_drops_only_new_local_names(self):
        with tempfile.TemporaryDirectory() as directory:
            runner = database.Runner(Path(directory), Path(directory), {})
            identity = "a" * 64
            runner.containers = {"postgres": identity}
            detail = [{"Id": identity, "Config": {"Labels": {database.LABEL: runner.run_id}}}]
            with patch.object(runner, "docker_call", return_value=subprocess.CompletedProcess([], 0, json.dumps(detail))), patch.object(runner, "database_names", return_value={"postgres", "rtest_1_main", "newapi_audit_123"}), patch.object(runner, "sql") as sql:
                runner.drop_suite_databases({"postgres": {"postgres"}})
            self.assertEqual({call.args[1] for call in sql.call_args_list}, {"DROP DATABASE rtest_1_main WITH (FORCE);", "DROP DATABASE newapi_audit_123 WITH (FORCE);"})
            with patch.object(runner, "docker_call", return_value=subprocess.CompletedProcess([], 0, json.dumps(detail))), patch.object(runner, "database_names", return_value={"postgres", "foreign_database"}), patch.object(runner, "sql") as sql:
                with self.assertRaises(database.Failure):
                    runner.drop_suite_databases({"postgres": {"postgres"}})
                sql.assert_not_called()

    def test_timeout_preserves_error_when_child_already_exited(self):
        with tempfile.TemporaryDirectory() as directory:
            runner = database.Runner(Path(directory), Path(directory), {})
            with patch.object(database.subprocess, "Popen") as popen, patch.object(database.os, "killpg", side_effect=ProcessLookupError):
                popen.return_value.communicate.side_effect = [subprocess.TimeoutExpired(["go"], 1), ("partial log", None)]
                with self.assertRaises(subprocess.TimeoutExpired):
                    runner.command(["go"], timeout=1)
            self.assertEqual((Path(directory) / "interrupted-command.log").read_text(), "partial log")

    def test_inventory_detects_new_database_test_and_helper_callers(self):
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory)
            path = source / "sample_test.go"
            path.write_text("""package example
func newDatabase(t *testing.T) {
    dsn := os.Getenv("TEST_NEW_DSN")
}
func TestFreshDatabase(t *testing.T) {
    newDatabase(t)
}
""")
            result = database.database_inventory(source, ["sample_test.go"])
            self.assertEqual(result["unclassified"][0]["test"], "TestFreshDatabase")
            self.assertEqual(result["unclassified"][0]["environment_functions"], ["newDatabase"])
            self.assertTrue(result["missing_required"])

    def test_repository_database_tests_are_all_classified(self):
        # A renamed fixture changes the environment roots of every caller;
        # the real repository must stay fully classified.
        source = Path(__file__).resolve().parents[2]
        result = database.database_inventory(source, database.database_test_files(source))
        self.assertEqual(result["unclassified"], [])
        self.assertEqual(result["missing_required"], [])

    def test_readiness_requires_two_consecutive_tcp_successes(self):
        with tempfile.TemporaryDirectory() as directory:
            runner = database.Runner(Path(directory), Path(directory), {})
            results = [subprocess.CompletedProcess([], code, output) for code, output in [(0, "1"), (1, "starting"), (0, "1"), (0, "1")]]
            with patch.object(runner, "sql", side_effect=results) as sql, patch.object(database.time, "sleep"):
                runner.wait_ready("mysql")
            self.assertEqual(sql.call_count, 4)

    def test_sigterm_uses_interrupt_cleanup_path(self):
        with self.assertRaises(KeyboardInterrupt):
            database.interrupt_run(15, None)

    def test_proxy_is_loopback_only_and_closes_socket(self):
        upstream = socket.socket()
        upstream.bind(("127.0.0.1", 0))
        upstream.listen(1)
        def echo():
            connection, _ = upstream.accept()
            with connection:
                connection.sendall(connection.recv(32))
        thread = threading.Thread(target=echo, daemon=True)
        thread.start()
        proxy = database.LoopbackProxy(upstream.getsockname())
        address = proxy.server_address
        try:
            self.assertEqual(address[0], "127.0.0.1")
            with socket.create_connection(address, timeout=2) as client:
                client.sendall(b"local-test-data")
                self.assertEqual(client.recv(32), b"local-test-data")
        finally:
            proxy.close()
            upstream.close()
            thread.join(timeout=2)
        with self.assertRaises(OSError):
            socket.create_connection(address, timeout=1)
        self.assertFalse(proxy.errors)

    def test_proxy_forwards_split_writes_without_ack_delay(self):
        # SQL 客户端常把一条报文分两次写出；代理不得因 Nagle 等待对端延迟 ACK。
        rounds = 10
        upstream = socket.socket()
        upstream.bind(("127.0.0.1", 0))
        upstream.listen(1)
        def reply():
            connection, _ = upstream.accept()
            with connection:
                connection.setsockopt(socket.IPPROTO_TCP, socket.TCP_NODELAY, 1)
                for _ in range(rounds):
                    received = b""
                    while len(received) < 2:
                        received += connection.recv(2 - len(received))
                    connection.sendall(b"x")
        thread = threading.Thread(target=reply, daemon=True)
        thread.start()
        proxy = database.LoopbackProxy(upstream.getsockname())
        try:
            with socket.create_connection(proxy.server_address, timeout=2) as client:
                client.setsockopt(socket.IPPROTO_TCP, socket.TCP_NODELAY, 1)
                started = time.monotonic()
                for _ in range(rounds):
                    client.sendall(b"a")
                    time.sleep(0.001)
                    client.sendall(b"b")
                    self.assertEqual(client.recv(1), b"x")
                elapsed = time.monotonic() - started
        finally:
            proxy.close()
            upstream.close()
            thread.join(timeout=2)
        # 未关闭 Nagle 时每轮约 42ms（实测 10 轮约 0.42 秒）。
        self.assertLess(elapsed, 0.25)
        self.assertFalse(proxy.errors)

    def test_output_symlink_and_existing_report_are_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            link = base / "link"
            link.symlink_to(base, target_is_directory=True)
            with self.assertRaises(database.Failure):
                database.private_directory(link / "out")
            output = database.private_directory(base / "out")
            (output / "database-report.json").write_text("{}")
            with self.assertRaises(database.Failure):
                database.private_directory(output)


if __name__ == "__main__":
    unittest.main()

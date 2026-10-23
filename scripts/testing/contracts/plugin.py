#!/usr/bin/env python3
"""隔离测试实例的插件两阶段 HTTP 验收；应用重启由统一候选 runner 完成。

before-restart 保存私有状态并撤销会话/Token、停用渠道；after-restart 在
同一端口恢复模拟上游并完成旧任务。缺少完成用量的成功任务按提交时估算
结算。只读数据库核对原生任务来源、注册源码 SHA、提交时价格快照和扣费日志。
任务终态后激活兼容版本，验证 producerVersion 旧产物分支。不会调用真实
供应商或重启服务；不声称历史源码固定执行或旧版本删除保护。
"""

import base64
import hashlib
import http.server
import json
import re
import secrets
import threading
import time
import urllib.parse
import urllib.request

from .common import (RequirementError, acceptance_target, atomic_private, expression_pricing, candidate_query, regular_file,
                                native_balances, require_charge, save_model_pricing, verify_candidate_database)
from .client import Client as AcceptanceClient

# Native task submission charges 30 seconds of 1080p at 0.4 USD, then adjusts usage.
TASK_ESTIMATE_USD = 12


class AcceptanceError(RuntimeError):
    """只包含预先定义的非敏感错误码。"""


def require(condition, code):
    if not condition:
        raise AcceptanceError(code)


def loopback_base(value):
    parsed = urllib.parse.urlsplit(value)
    require(parsed.scheme == "http" and parsed.hostname == "127.0.0.1"
            and parsed.port is not None and parsed.username is None
            and not parsed.query and not parsed.fragment and parsed.path in ("", "/"),
            "explicit_ipv4_loopback_base_required")
    return value.rstrip("/")


def private_json(path):
    regular_file(path, private=True)
    require(path.stat().st_size <= 65536, "private_file_too_large")
    with path.open(encoding="utf-8") as stream:
        result = json.load(stream)
    require(isinstance(result, dict), "private_json_object_required")
    return result


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *_args, **_kwargs):
        return None


class Client(AcceptanceClient):
    def __init__(self, base):
        super().__init__(loopback_base(base), redirect_handler=NoRedirect())

    def api(self, method, path, body=None, proof=None):
        status, raw = self.request(method, path, body, proof=proof)
        value = json.loads(raw)
        if status != 200 or not isinstance(value, dict) or value.get("success") is not True:
            # Method and path contain no secrets for our management calls.
            failure = AcceptanceError("management_api_rejected")
            failure.details = {"method": method, "path": path.split("?")[0], "http_status": status}
            raise failure
        return value.get("data")

    def local_path(self, value):
        parsed = urllib.parse.urlsplit(value)
        if parsed.scheme or parsed.netloc:
            target = urllib.parse.urlsplit(self.base)
            require((parsed.scheme, parsed.netloc) == (target.scheme, target.netloc),
                    "artifact_origin_mismatch")
        require(parsed.path.startswith("/v1/tasks/") and not parsed.fragment,
                "artifact_path_mismatch")
        return urllib.parse.urlunsplit(("", "", parsed.path, parsed.query, ""))


def plugin_source(key, model, version, quantity="seconds"):
    require(quantity in ("seconds", "p"), "unsupported_usage_quantity")
    require(version in ("1.0.0", "2.0.0"), "unsupported_fixture_plugin_version")
    meta = {"apiVersion": 1, "key": key, "name": "Ren2Hub local acceptance",
        "version": version, "author": {"name": "Ren2Hub acceptance"},
        "models": [model], "fetchMode": "per_task",
        "usageSchema": {quantity: {"type": "number", "unit": "second"},
                        "resolution": {"enum": ["720p", "1080p"]}}}
    route = "/v1" if version == "1.0.0" else "/v2"
    return "export const meta = " + json.dumps(meta, separators=(",", ":")) + ";\n" + r'''
export function buildSubmitRequest(ctx) {
  if (!["complete", "missing"].includes(ctx.requestBody.case)) throw new Error("invalid acceptance case");
  return {url: ctx.baseUrl + "/submit", method: "POST", headers: {"Authorization": "Bearer " + ctx.apiKey},
    body: {case: ctx.requestBody.case}, action: "acceptance"};
}
export function parseSubmitResponse(ctx, response) {
  return {taskId: response.body.id, taskData: response.body};
}
export function extractUsage() { return {__QUANTITY__: 30, resolution: "1080p"}; }
export function buildQueryRequest(ctx) {
  return {url: ctx.baseUrl + "__ROUTE__/jobs/" + ctx.taskId, method: "GET",
    headers: {"Authorization": "Bearer " + ctx.apiKey}};
}
export function parseTaskResult(ctx, body) {
  return {taskId: ctx.taskId, status: body.status, progress: body.status === "SUCCESS" ? "100%" : "50%",
    finishTime: body.finish_time || 0};
}
export function extractUsageOnComplete(ctx, result, body) {
  return typeof body.seconds === "number" ? {__QUANTITY__: body.seconds} : null;
}
export function listArtifacts(task) {
  return task.status === "SUCCESS" ? [{key: "output", type: "file", mimeType: "text/plain"}] : [];
}
export function buildContentRequest(ctx) {
  if (ctx.artifactKey !== "output") throw new Error("artifact_not_found");
  const route = __ARTIFACT_ROUTE__;
  return {url: ctx.baseUrl + route + "/artifacts/" + ctx.upstreamTaskId,
    method: ctx.clientRequest.method, headers: {"Authorization": "Bearer " + ctx.apiKey,
      "X-Acceptance-Plugin-Version": "__VERSION__", "X-Acceptance-Producer-Version": ctx.producerVersion}};
}
'''.replace("__ROUTE__", route).replace("__QUANTITY__", quantity).replace("__VERSION__", version).replace(
        "__ARTIFACT_ROUTE__", '"/v1"' if version == "1.0.0" else '(ctx.producerVersion === "1.0.0" ? "/v1" : "/v2")')


def task_price(model, rate="0.4", quantity="seconds"):
    require(quantity in ("seconds", "p"), "unsupported_usage_quantity")
    return expression_pricing('tier("1080p", u("' + quantity + '") * ' + rate + ')')

class Mock(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_args):
        pass

    def respond(self, status, body, mime="application/json"):
        raw = json.dumps(body).encode() if mime == "application/json" else body
        self.send_response(status)
        self.send_header("Content-Type", mime)
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        if self.command != "HEAD":
            self.wfile.write(raw)

    def authenticated(self):
        if self.headers.get("Authorization") == "Bearer " + self.server.mock_key:
            return True
        self.respond(401, {"error": "mock_credential_mismatch"})
        return False

    def do_POST(self):
        try:
            size = int(self.headers.get("Content-Length", "0"))
        except ValueError:
            self.respond(400, {"error": "invalid_submit"})
            return
        if not 0 < size <= 4096:
            self.respond(400, {"error": "invalid_submit"})
            return
        # Drain bounded requests before early responses: closing a socket with
        # unread POST data can reset it before the client receives the 401/400.
        body = self.rfile.read(size)
        if len(body) != size:
            self.respond(400, {"error": "invalid_submit"})
            return
        if not self.authenticated():
            return
        if self.path != "/submit":
            self.respond(400, {"error": "invalid_submit"})
            return
        try:
            case = json.loads(body).get("case")
        except (ValueError, AttributeError):
            case = None
        if case not in ("complete", "missing"):
            self.respond(400, {"error": "invalid_case"})
            return
        with self.server.lock:
            self.server.counts["submits"] += 1
            duplicate = case in self.server.submitted or self.server.phase == "after-restart"
            self.server.submitted.add(case)
            if duplicate:
                self.server.counts["replays"] += 1
        if duplicate:
            self.respond(409, {"error": "replay_forbidden"})
            return
        self.respond(202, {"id": case, "status": "IN_PROGRESS"})

    def do_GET(self):
        if not self.authenticated():
            return
        match = re.fullmatch(r"/v1/(jobs|artifacts)/(complete|missing)", self.path)
        if not match:
            with self.server.lock:
                self.server.counts["wrong_version"] += 1
            self.respond(409, {"error": "active_version_fallback_forbidden"})
            return
        kind, case = match.groups()
        with self.server.lock:
            self.server.counts[kind] += 1
        if kind == "artifacts":
            if self.headers.get("X-Acceptance-Plugin-Version") == "2.0.0":
                if self.headers.get("X-Acceptance-Producer-Version") != "1.0.0":
                    with self.server.lock:
                        self.server.counts["wrong_version"] += 1
                    self.respond(409, {"error": "producer_version_mismatch"})
                    return
                with self.server.lock:
                    self.server.counts["compat_artifacts"] += 1
            self.respond(200, ("producer-v1-" + case).encode(), "text/plain")
            return
        done = case == "missing" or self.server.phase == "after-restart"
        body = {"id": case, "status": "SUCCESS" if done else "IN_PROGRESS"}
        if done:
            body["finish_time"] = self.server.finished_at
        if done and case == "complete":
            body["seconds"] = 10
        self.respond(200, body)

    do_HEAD = do_GET


class Snapshot:
    """仅暴露本实验渠道的固定只读查询，不接受调用者提供 SQL。"""
    def __init__(self, revision):
        verify_candidate_database(revision)

    def _rows(self, query):
        return candidate_query(query, timeout=20)

    def read(self, channel_id):
        require(type(channel_id) is int and channel_id > 0, "positive_channel_id_required")
        channel = str(channel_id)
        return {
            "tasks": self._rows("SELECT id,task_id,status,quota,finish_time,private_data FROM tasks WHERE channel_id=" + channel),
            "plugins": self._rows("SELECT key,version,source_hash,source,active,enabled FROM task_plugins WHERE key IN (SELECT DISTINCT platform FROM tasks WHERE channel_id=" + channel + ")"),
            "logs": self._rows("SELECT id,request_id,type,quota,other FROM logs WHERE channel_id=" + channel + " ORDER BY id"),
            "requests": self._rows("SELECT request_count FROM users WHERE id IN (SELECT DISTINCT user_id FROM tasks WHERE channel_id=" + channel + ")"),
        }


class Experiment:
    def __init__(self, args, report):
        self.args, self.report = args, report
        self.state = {} if args.phase == "before-restart" else private_json(args.state)
        self.fixture_controls_active = True
        if args.phase == "after-restart":
            # Reject foreign state before login or cleanup can act on its IDs.
            require(self.state.get("format") == 4 and self.state.get("acceptance_target") == acceptance_target()
                    and self.state.get("phase") == "before-restart-complete"
                    and self.state.get("base") == args.base
                    and self.state.get("revision") == args.revision,
                    "before_restart_state_mismatch")
        self.root, self.user = Client(args.base), Client(args.base)
        self.snapshot = Snapshot(args.revision) if args.snapshot else None
        self.server = None

    def save(self):
        atomic_private(self.args.state, json.dumps(self.state, ensure_ascii=False, indent=2))

    def check(self, name, **details):
        self.report["checks"].append({"name": name, **details})

    def wait(self, predicate, code, timeout=75):
        deadline = time.monotonic() + timeout
        while True:
            if predicate():
                return
            require(time.monotonic() < deadline, code)
            time.sleep(1)

    def token_status(self, status):
        self.user.api("PUT", "/api/token/?status_only=true", {"id": self.state["token_id"], "status": status})

    def channel_status(self, status):
        self.root.api("POST", "/api/channel/status/batch", {"ids": [self.state["channel_id"]], "status": status})

    def task(self, case):
        status, raw = self.user.request("GET", "/v1/tasks/" + self.state["tasks"][case],
                                        bearer=self.state["token_key"])
        require(status == 200, "task_query_failed")
        return json.loads(raw)

    def wallet(self):
        return native_balances(self.state["user_id"], self.state["token_id"])

    def assert_wallet(self, charges_usd):
        require_charge(self.state["initial_wallet"], self.wallet(), sum(charges_usd) * self.state["quota_per_usd"])

    def verify_task_settlements(self):
        """Two native submit charges and one completion refund, with the original expression."""
        require(self.snapshot is not None, "native_snapshot_required")
        unit = self.state["quota_per_usd"]
        expected = {"complete": 4 * unit, "missing": TASK_ESTIMATE_USD * unit}
        saved = self.snapshot.read(self.state["channel_id"])
        tasks = {row["task_id"]: int(row["quota"]) for row in saved["tasks"]}
        require(tasks == {self.state["tasks"][case]: quota for case, quota in expected.items()},
                "native_task_charge_mismatch")
        logs = {self.state["tasks"][case]: [] for case in expected}
        expr = self.state["original_pricing"]["billing_setting.billing_expr"]
        quantity = self.state.get("usage_quantity", "seconds")
        for row in saved["logs"]:
            require(row["type"] in (2, 6), "unexpected_native_task_log")
            other = json.loads(row["other"]) if isinstance(row["other"], str) else row["other"]
            require(other.get("task_id") in logs, "unknown_task_billing_log")
            require(other.get("billing_mode") == "tiered_expr"
                    and other.get("expr_b64") == base64.b64encode(expr.encode()).decode(),
                    "native_task_price_snapshot_changed")
            facts = other.get("usage_facts", {})
            require(facts.get(quantity) == (10 if row["type"] == 6 else 30)
                    and facts.get("resolution") == "1080p", "native_task_usage_basis_mismatch")
            logs[other["task_id"]].append((row["type"], int(row["quota"])))
            if row["type"] == 6:
                require(other.get("pre_consumed_quota") == 12 * unit
                        and other.get("actual_quota") == 4 * unit, "native_refund_delta_mismatch")
        require(sorted(logs[self.state["tasks"]["complete"]]) == [(2, 12 * unit), (6, 8 * unit)]
                and logs[self.state["tasks"]["missing"]] == [(2, 12 * unit)],
                "native_task_billing_not_exactly_once")
        self.assert_wallet([12, 4])

    def db_check(self, terminal, require_enabled=True):
        require(self.snapshot is not None, "native_snapshot_required")
        data = self.snapshot.read(self.state["channel_id"])
        require(len(data["tasks"]) == 2, "native_task_count_mismatch")
        require({row["task_id"] for row in data["tasks"]} == set(self.state["tasks"].values()),
                "native_task_identity_changed")
        version = self.state.get("active_version", "1.0.0")
        for expected_version, expected_hash in (("1.0.0", self.state["v1_hash"]),
                                                ("2.0.0", self.state.get("v2_hash"))):
            if expected_hash is None:
                continue
            plugins = [row for row in data["plugins"] if row["key"] == self.state["plugin"] and row["version"] == expected_version]
            require(len(plugins) == 1 and plugins[0]["source_hash"] == expected_hash
                    and hashlib.sha256(plugins[0]["source"].encode()).hexdigest() == expected_hash,
                    "plugin_registry_source_changed")
        require([row["version"] for row in data["plugins"] if row["active"] and (row["enabled"] or not require_enabled)] == [version],
                "plugin_active_version_mismatch")
        provenance, prices, completed = {}, {}, 0
        terminal_times = dict(self.state.get("terminal_times", {}))
        quantity = self.state.get("usage_quantity", "seconds")
        expr = self.state["original_pricing"]["billing_setting.billing_expr"]
        for row in data["tasks"]:
            private = json.loads(row["private_data"]) if isinstance(row["private_data"], str) else row["private_data"]
            execution = private.get("execution", {})
            producer = execution.get("task_plugin", {})
            require(producer.get("key") == self.state["plugin"] and producer.get("version") == "1.0.0"
                    and producer.get("api_version") == 1 and producer.get("generation", 0) > 0
                    and execution.get("request_id")
                    and execution.get("request_path") == "/v1/tasks/" + self.state["plugin"],
                    "native_task_provenance_changed")
            snapshot = private.get("billing_context", {}).get("tiered_snapshot", {})
            require(snapshot.get("expr_string") == expr and snapshot.get("channel_ratio") == 1
                    and snapshot.get("task_usage_billing") is True, "native_task_price_snapshot_changed")
            success = row["status"] == "SUCCESS"
            require(row["status"] in ("SUBMITTED", "IN_PROGRESS", "SUCCESS"), "unexpected_native_task_status")
            if success:
                # Per-task polling uses the host's terminal time. It does not
                # copy parseTaskResult.finishTime from the mock provider.
                require(type(row["finish_time"]) is int and row["finish_time"] >= self.state["finished_at"],
                        "native_task_finish_time_invalid")
                require(terminal_times.get(row["task_id"], row["finish_time"]) == row["finish_time"],
                        "native_task_finish_time_changed")
                terminal_times[row["task_id"]] = row["finish_time"]
                completed += 1
            # Native persistence retains the submitted facts; the completion
            # adjustment log carries merged actual facts (verified separately).
            require(snapshot.get("usage_facts") == {quantity: 30, "resolution": "1080p"},
                    "native_task_usage_snapshot_mismatch")
            provenance[row["task_id"]] = execution
            prices[row["task_id"]] = snapshot
        require(completed <= terminal, "native_task_completed_before_fixture_release")
        if "submission_provenance" in self.state:
            require(provenance == self.state["submission_provenance"], "native_task_provenance_changed")
            require(prices == self.state["submission_prices"], "native_task_price_snapshot_changed")
        else:
            self.state["submission_provenance"], self.state["submission_prices"] = provenance, prices
            self.save()
        if terminal_times != self.state.get("terminal_times"):
            self.state["terminal_times"] = terminal_times
            self.save()
        require(data["requests"] == [{"request_count": 2}], "native_task_request_count_changed")
        return completed == terminal

    def activate_compatible_v2(self):
        # Native NewAPI executes the active plugin. Activate only after both
        # tasks settle, then explicitly exercise its historical artifact branch.
        source = plugin_source(self.state["plugin"], self.state["model"], "2.0.0", self.state.get("usage_quantity", "seconds"))
        self.state["v2_hash"] = hashlib.sha256(source.encode()).hexdigest()
        self.save()
        self.root.api("POST", "/api/plugin/task", {"source": source, "enabled": True,
            "sourceSha256": self.state["v2_hash"], "remark": "producerVersion artifact compatibility"})
        self.root.api("POST", "/api/plugin/task/" + self.state["plugin"] + "/activate", {"version": "2.0.0"})
        self.state["active_version"] = "2.0.0"
        self.save()
        self.artifact("missing", saved=True)
        self.artifact("complete", saved=True)
        require(self.server.counts["compat_artifacts"] == 2, "active_v2_producer_compatibility_not_observed")
        require(self.db_check(2), "native_provenance_changed_on_activation")
        self.check("active_v2_explicitly_serves_v1_artifacts_using_producer_version")

    def artifact(self, case, saved=False):
        status, raw = self.user.request("GET", "/v1/tasks/" + self.state["tasks"][case] + "/artifacts",
                                        bearer=self.state["token_key"])
        require(status == 200, "artifact_projection_failed")
        items = json.loads(raw)["artifacts"]
        require(len(items) == 1 and items[0]["key"] == "output", "artifact_projection_mismatch")
        if not saved:
            self.state.setdefault("artifact_urls", {})[case] = items[0]["content_url"]
        signed = urllib.parse.urlsplit(self.state["artifact_urls"][case])
        expected_path = "/v1/tasks/" + self.state["tasks"][case] + "/artifacts/output/content"
        require(signed.scheme in ("http", "https") and signed.hostname and signed.username is None
                and signed.path == expected_path and not signed.fragment, "signed_artifact_identity_mismatch")
        query = urllib.parse.parse_qs(signed.query, strict_parsing=True)
        require(set(query) == {"access"} and len(query["access"]) == 1
                and re.fullmatch(r"[A-Za-z0-9_-]{43}", query["access"][0]), "signed_artifact_query_mismatch")
        # The configured public hostname may be external. Exercise its exact
        # signed path on this app's loopback; never connect to that hostname.
        path = self.user.local_path(urllib.parse.urlunsplit(("", "", signed.path, signed.query, "")))
        status, raw = self.user.request("GET", path, bearer="")
        require(status == 200 and raw == ("producer-v1-" + case).encode(), "signed_artifact_content_mismatch")

    def restrict_artifact_fetch(self):
        # Set restrictive filters before permitting the one private address.
        # Save the exact effective values before the first settings write.
        values = {"fetch_setting.enable_ssrf_protection": "true", "fetch_setting.domain_filter_mode": "true",
            "fetch_setting.domain_list": "[]", "fetch_setting.ip_filter_mode": "true",
            "fetch_setting.ip_list": '["127.0.0.1/32"]',
            "fetch_setting.allowed_ports": json.dumps([str(self.state["mock_port"])]),
            "fetch_setting.apply_ip_filter_for_domain": "true", "fetch_setting.allow_private_ip": "true"}
        current = {row["key"]: row["value"] for row in self.root.api("GET", "/api/option/")}
        require(all(key in current for key in values), "fetch_configuration_snapshot_incomplete")
        require(not self.state.get("fetch_restore_pending"), "previous_fetch_restore_pending")
        self.state["fetch_original"] = {key: current[key] for key in values}
        self.state["fetch_restore_pending"] = True
        self.save()
        for key, value in values.items():
            self.root.api("PUT", "/api/option/", {"key": key, "value": value})
        actual = {row["key"]: row["value"] for row in self.root.api("GET", "/api/option/")}
        require(all(actual.get(key) == value for key, value in values.items()), "fetch_test_allowlist_not_applied")
        self.check("artifact_fetch_allowlist_is_exact_loopback_and_mock_port")

    def start_mock(self):
        self.server = http.server.ThreadingHTTPServer(("127.0.0.1", self.state.get("mock_port", 0)), Mock)
        self.server.phase, self.server.mock_key = self.args.phase, self.state["mock_key"]
        self.server.finished_at = self.state["finished_at"]
        self.server.lock = threading.Lock()
        self.server.submitted = set()
        self.server.counts = {"submits": 0, "replays": 0, "jobs": 0, "artifacts": 0, "wrong_version": 0, "compat_artifacts": 0}
        self.state["mock_port"] = self.server.server_port
        self.save()
        threading.Thread(target=self.server.serve_forever, daemon=True).start()

    def run(self):
        verify_candidate_database(self.args.revision)
        status, raw = self.root.request("GET", "/api/status")
        require(status == 200 and json.loads(raw).get("data", {}).get("version") == self.args.revision,
                "running_revision_mismatch")
        self.check("running_revision_matches", version=self.args.revision)
        secret = private_json(self.args.credentials)
        root = self.root.login(secret["ren2hub_username"], secret["ren2hub_password"])
        require(root["role"] == 100, "root_acceptance_account_required")
        # This is a persistent native option, not an environment override.
        # Keep it across the two phases so absolute artifact URLs survive restart.
        options = {row["key"]: row["value"] for row in self.root.api("GET", "/api/option/")}
        require(options.get("TaskPublicAddress", "") in ("", self.args.base), "candidate_task_public_address_mismatch")
        if self.args.phase == "before-restart":
            self.root.api("PUT", "/api/option/", {"key": "ServerAddress", "value": self.args.base})
            options = {row["key"]: row["value"] for row in self.root.api("GET", "/api/option/")}
        require(options.get("ServerAddress") == self.args.base, "candidate_server_address_not_persisted")
        self.check("native_candidate_artifact_address_persisted")
        if self.args.phase == "before-restart":
            self.before()
        else:
            self.after()

    def before(self):
        suffix = secrets.token_hex(5)
        self.state = {"format": 4, "acceptance_target": acceptance_target(), "phase": "preparing", "base": self.args.base,
            "revision": self.args.revision, "plugin": "core-" + suffix,
            "model": "core-task-" + suffix, "username": "plug" + suffix,
            "password": secrets.token_urlsafe(24), "mock_key": secrets.token_hex(24),
            "finished_at": int(time.time()), "tasks": {}, "usage_quantity": getattr(self.args, 'usage_quantity', 'seconds')}
        self.save()
        self.start_mock()
        self.state["started_at_before"] = self.root.api("GET", "/api/status").get("start_time")
        require(isinstance(self.state["started_at_before"], int) and self.state["started_at_before"] > 0,
                "running_instance_identity_unavailable")
        source = plugin_source(self.state["plugin"], self.state["model"], "1.0.0", self.state.get("usage_quantity", "seconds"))
        self.state["v1_hash"] = hashlib.sha256(source.encode()).hexdigest()
        self.root.api("POST", "/api/plugin/task", {"source": source, "enabled": True,
            "sourceSha256": self.state["v1_hash"], "remark": "isolated two-phase acceptance"})
        self.state["plugin_created"] = True
        self.save()
        self.state["original_pricing"] = task_price(self.state["model"], quantity=self.state.get("usage_quantity", "seconds"))
        price = save_model_pricing(self.root, self.state["model"], self.state["original_pricing"])
        self.state["pricing_config_version"] = price["version"]
        self.root.api("POST", "/api/user/", {"username": self.state["username"],
            "password": self.state["password"], "display_name": "Plugin acceptance", "role": 1, "status": 1})
        user = self.user.login(self.state["username"], self.state["password"])
        self.state["user_id"] = user["id"]
        self.save()
        status = self.root.api("GET", "/api/status")
        self.state["quota_per_usd"] = int(status["quota_per_unit"])
        require(self.state["quota_per_usd"] == 500000, "unexpected_usd_quota_scale")
        self.root.api("POST", "/api/user/manage", {"id": self.state["user_id"],
            "action": "add_quota", "mode": "override", "value": 100 * self.state["quota_per_usd"],
            "operation_id": "plugin-init-" + suffix})
        self.state["token_quota"] = 100 * self.state["quota_per_usd"]
        self.user.api("POST", "/api/token/", {"name": self.state["plugin"],
            "remain_quota": self.state["token_quota"], "unlimited_quota": False,
            "expired_time": -1, "type": "auto", "model_limits_enabled": True, "model_limits": self.state["model"]})
        tokens = self.user.api("GET", "/api/token/search?keyword=" + self.state["plugin"] + "&page_size=100")["items"]
        tokens = [row for row in tokens if row["name"] == self.state["plugin"]]
        require(len(tokens) == 1, "temporary_token_not_unique")
        self.state["token_id"] = tokens[0]["id"]
        self.state["token_key"] = self.user.api("POST", f"/api/token/{tokens[0]['id']}/key")["key"]
        self.save()
        channel = {
            "name": self.state["plugin"], "type": 61, "key": self.state["mock_key"],
            "base_url": "http://127.0.0.1:" + str(self.state["mock_port"]), "models": self.state["model"],
            "status": 1, "priority": 0, "weight": 100,
            "setting": json.dumps({"task_plugin_key": self.state["plugin"]})}
        self.root.api("POST", "/api/channel/", {"mode": "single", "channel": channel})
        rows = self.root.api("GET", "/api/channel/search?keyword=" + self.state["plugin"] + "&page_size=100")["items"]
        rows = [row for row in rows if row["name"] == self.state["plugin"]]
        require(len(rows) == 1, "temporary_channel_not_unique")
        self.state["channel_id"] = rows[0]["id"]
        self.save()
        # Native creation persists rows without immediately refreshing the
        # channel cache. Full UpdateChannel refreshes it even for unchanged status.
        self.root.api("PUT", "/api/channel/", {**{key: value for key, value in channel.items() if key != "status"}, "id": self.state["channel_id"]})
        self.state["initial_wallet"] = self.wallet()
        self.save()
        for case in ("complete", "missing"):
            status, raw = self.user.request("POST", "/v1/tasks/" + self.state["plugin"],
                {"model": self.state["model"], "case": case}, bearer=self.state["token_key"])
            require(status == 200, "plugin_task_submit_failed")
            self.state["tasks"][case] = json.loads(raw)["task_id"]
            self.save()
        self.finish_before()

    def finish_before(self):
        self.wait(lambda: self.task("missing")["status"] == "SUCCESS", "missing_usage_task_not_terminal")
        require(self.task("complete")["status"] in ("SUBMITTED", "IN_PROGRESS"), "pending_task_not_preserved")
        # Native submission has already charged both estimates. The missing
        # completion usage leaves the original submit facts and 12 USD charge.
        self.assert_wallet([TASK_ESTIMATE_USD, TASK_ESTIMATE_USD])
        self.wait(lambda: self.db_check(1), "native_task_snapshot_not_ready")
        self.restrict_artifact_fetch()
        self.artifact("missing")
        self.check("native_submit_charges_both_estimates_and_missing_usage_retains_submit_facts",
                   charged_usd="24", missing_final_usd="12", terminal_count=1)
        save_model_pricing(self.root, self.state["model"],
            task_price(self.state["model"], "0.9", self.state.get("usage_quantity", "seconds")),
            self.state["pricing_config_version"])
        require(self.db_check(1), "native_submission_snapshot_changed_on_reprice")
        self.check("registered_v1_source_and_submission_price_snapshot_recorded", source_sha256=self.state["v1_hash"])
        self.state["phase"] = "before-restart-complete"
        self.save()

    def after(self):
        current = self.root.api("GET", "/api/status").get("start_time")
        require(isinstance(current, int) and current > self.state["started_at_before"], "application_restart_not_observed")
        self.check("application_restart_observed", previous_revision=self.state["revision"], current_revision=self.args.revision)
        self.start_mock()
        self.user.login(self.state["username"], self.state["password"])
        self.token_status(1)
        self.channel_status(1)
        self.wait(lambda: self.task("complete")["status"] == "SUCCESS", "native_task_not_recovered")
        settled_usd = TASK_ESTIMATE_USD + 4
        self.wait(lambda: self.wallet()["wallet_used"] == self.state["initial_wallet"]["wallet_used"] + settled_usd * self.state["quota_per_usd"],
                  "native_completion_adjustment_not_delivered")
        self.assert_wallet([TASK_ESTIMATE_USD, 4])
        self.wait(lambda: self.db_check(2), "native_completed_task_snapshot_not_ready")
        self.restrict_artifact_fetch()
        self.artifact("missing", saved=True)
        self.artifact("complete")
        self.verify_task_settlements()
        for _ in range(3):
            require(all(self.task(case)["status"] == "SUCCESS" for case in ("missing", "complete")), "terminal_status_regressed")
        require(self.db_check(2), "native_terminal_snapshot_changed_on_requery")
        self.verify_task_settlements()
        self.check("native_v1_registry_and_provenance_survive_restart_with_original_price",
                   source_sha256=self.state["v1_hash"], usage_quantity=self.state.get("usage_quantity", "seconds"),
                   actual_seconds=10, unit_usd="0.4", completed_final_usd="4")
        self.check("native_submit_and_completion_adjustment_are_not_repeated", total_usd=str(settled_usd),
                   submit_logs=2, completion_refund_logs=1, request_count=2)
        self.check("signed_artifact_survives_restart")
        self.activate_compatible_v2()
        self.verify_task_settlements()
        self.state["phase"] = "after-restart-complete"
        self.state["resumed_revision"] = self.args.revision
        self.save()

    def cleanup(self):
        errors = []
        if self.root.bearer and self.state.get("fetch_restore_pending"):
            try:
                # Block private targets before restoring the original filters.
                self.root.api("PUT", "/api/option/", {"key": "fetch_setting.allow_private_ip", "value": "false"})
                original = self.state["fetch_original"]
                keys = [key for key in original if key != "fetch_setting.allow_private_ip"] + ["fetch_setting.allow_private_ip"]
                for key in keys:
                    self.root.api("PUT", "/api/option/", {"key": key, "value": original[key]})
                actual = {row["key"]: row["value"] for row in self.root.api("GET", "/api/option/")}
                require(all(actual.get(key) == value for key, value in original.items()), "fetch_restore_readback_mismatch")
                self.state["fetch_restore_pending"] = False
                self.save()
            except Exception:
                errors.append("fetch_configuration_restore_failed")
        controls = getattr(self, "fixture_controls_active", True)
        if controls and self.user.bearer and self.state.get("token_id"):
            try:
                self.token_status(2)
                require(self.user.request("GET", "/v1/models", bearer=self.state["token_key"])[0] in (401, 403), "revoked_token_accepted")
            except Exception:
                errors.append("token_cleanup_failed")
        if controls and self.root.bearer and self.state.get("channel_id"):
            try:
                self.channel_status(2)
            except Exception:
                errors.append("channel_cleanup_failed")
        if controls and self.root.bearer and self.state.get("plugin_created") and (self.args.phase == "after-restart"
                or self.state.get("phase") != "before-restart-complete"):
            try:
                self.root.api("POST", "/api/plugin/task/" + self.state["plugin"] + "/status?cascade=true&force=true", {"enabled": False})
            except Exception:
                errors.append("plugin_cleanup_failed")
        for client in (self.user, self.root):
            if not client.bearer:
                continue
            try:
                old = client.bearer
                logout_error = None
                try:
                    client.api("POST", "/api/user/auth/logout", {})
                except Exception as error:
                    # Revocation can commit before refresh-cookie cleanup
                    # fails. Verify the old access token independently.
                    logout_error = type(error).__name__
                status, _ = client.request("GET", "/api/user/self", bearer=old)
                require(status in (401, 403), "logout_access_still_valid")
                client.bearer = ""
                if logout_error:
                    self.report.setdefault("logout_response_errors_after_verified_revocation", []).append(logout_error)
            except Exception as error:
                errors.append("session_cleanup_failed")
                self.report.setdefault("cleanup_error_types", []).append(type(error).__name__)
        if self.server:
            self.server.shutdown()
            self.server.server_close()
            self.report["mock_counts"] = self.server.counts
            expected = 2 if self.args.phase == "before-restart" else 0
            if self.server.counts["submits"] != expected or self.server.counts["replays"] or self.server.counts["wrong_version"]:
                errors.append("mock_send_or_version_count_failed")
        self.report["cleanup_errors"] = errors
        require(not errors, "cleanup_or_mock_contract_failed")


def run_plugin(args):
    report = {"success": False, "phase": args.phase, "revision": args.revision, "checks": [],
        "external_provider_requests": 0, "uncovered": ["真实供应商", "未知接受/发送崩溃窗口", "原生差额结算中途崩溃", "公开产物域名的网络可达性"]}
    experiment = None
    try:
        require(re.fullmatch(r"[0-9a-f]{40}", args.revision), "full_revision_required")
        require(not args.report.exists() and not args.report.is_symlink(), "new_report_path_required")
        require(args.report.resolve() != args.state.resolve() and args.state.resolve() != args.credentials.resolve(),
                "state_and_credentials_paths_must_differ")
        if args.phase == "before-restart":
            require(not args.state.exists() and not args.state.is_symlink(), "new_state_path_required")
        require(args.snapshot, "native_snapshot_required")
        experiment = Experiment(args, report)
        experiment.run()
        experiment.check("readonly_native_source_provenance_price_and_charge_checks")
        report["retired_assertions"] = ["历史 executable 固定执行", "旧源码删除保护", "schema5 预留账本", "结果 outbox 与独立统计投递"]
        report["success"] = True
    except Exception as error:
        report["error_code"] = str(error) if isinstance(error, (AcceptanceError, RequirementError)) else "acceptance_api_or_transport_failure"
        report["error_type"] = type(error).__name__
        if isinstance(error, AcceptanceError) and hasattr(error, "details"):
            report["failed_api"] = error.details
    finally:
        if experiment:
            try:
                experiment.cleanup()
            except AcceptanceError as error:
                report["success"] = False
                report.setdefault("error_code", str(error))
        # Reports contain no credentials, private state, response bodies or signed URLs.
        if not args.report.exists() and not args.report.is_symlink():
            atomic_private(args.report, json.dumps(report, ensure_ascii=False, indent=2))
    return report

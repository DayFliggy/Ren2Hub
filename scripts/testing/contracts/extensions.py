"""候选实例的 GMPay / 工单扩展验收；只使用本地 TLS 假网关。

调用者先验证隔离候选身份并建立 root/user/other 会话。数据库仅经
experiment.rows 只读核对；业务写入全部走实际 API。运行结束停用假商户。
"""
from concurrent.futures import ThreadPoolExecutor
from contextlib import ExitStack
from decimal import Decimal
import base64
import hashlib
import hmac
import http.server
import json
from pathlib import Path
import re
import secrets
import ssl
import threading
import time
import urllib.parse

from .common import RequirementError, native_balances, operation_proof, require, require_charge
from .client import Client
from .websocket import WebSocket, logout_verified


PNG = base64.b64decode(
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a0p8AAAAASUVORK5CYII="
)
PAY_METHOD = "gmpay:usdt.tron"
NOTIFY_PATH = "/api/user/gmpay/notify"


def sign(values, key):
    canonical = "&".join(sorted(f"{name}={value}" for name, value in values.items()
                                if name != "signature" and value != ""))
    return hmac.new(key.encode(), canonical.encode(), hashlib.sha256).hexdigest()


def signed(values, key):
    result = dict(values)
    result["signature"] = sign(result, key)
    return result


def payload(response, code):
    status, raw = response
    require(status == 200, code + "_http")
    try:
        value = json.loads(raw)
    except ValueError:
        require(False, code + "_json")
    require(isinstance(value, dict), code + "_object")
    return value


def business(response, code):
    value = payload(response, code)
    require(value.get("success") is True, code)
    return value.get("data")


def rejected(response, code, expected_code=None):
    value = payload(response, code)
    require(value.get("success") is False, code)
    if expected_code:
        require(value.get("code") == expected_code, code + "_code")


def multipart(fields, files=()):
    boundary = "Ren2HubAcceptance" + secrets.token_hex(12)
    chunks = []
    for name, value in fields.items():
        chunks.append(f'--{boundary}\r\nContent-Disposition: form-data; name="{name}"\r\n\r\n{value}\r\n'.encode())
    for name, mime, raw in files:
        chunks.append(f'--{boundary}\r\nContent-Disposition: form-data; name="attachments"; filename="{name}"\r\nContent-Type: {mime}\r\n\r\n'.encode())
        chunks.extend((raw, b"\r\n"))
    chunks.append(f"--{boundary}--\r\n".encode())
    return b"".join(chunks), {"Content-Type": "multipart/form-data; boundary=" + boundary}


def submit_form(client, path, fields, files=()):
    body, headers = multipart(fields, files)
    return client.request("POST", path, body, headers=headers)


class Gateway(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_args):
        pass

    def respond(self, status, body):
        raw = json.dumps(body, separators=(",", ":")).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def do_GET(self):
        if self.path != "/payments/gmpay/v1/config":
            self.respond(404, {})
            return
        self.respond(200, {"status_code": 200, "data": {"supported_assets": [
            {"network": "tron", "display_name": "Local TRON", "tokens": ["usdt"]}
        ]}})

    def do_POST(self):
        try:
            size = int(self.headers.get("Content-Length", "0"))
        except ValueError:
            size = 0
        if self.path != "/payments/gmpay/v1/order/create-transaction" or not 0 < size <= 16384:
            self.respond(400, {})
            return
        raw = self.rfile.read(size)
        try:
            pairs = urllib.parse.parse_qs(raw.decode(), strict_parsing=True)
            if any(len(values) != 1 for values in pairs.values()):
                raise ValueError("duplicate")
            values = {name: items[0] for name, items in pairs.items()}
        except (ValueError, UnicodeError):
            self.respond(400, {})
            return
        with self.server.lock:
            valid = (hmac.compare_digest(values.get("signature", ""), sign(values, self.server.key))
                     and values.get("pid") == self.server.merchant
                     and values.get("currency") == "cny"
                     and values.get("token") == "usdt" and values.get("network") == "tron"
                     and re.fullmatch(r"[0-9]+\.[0-9]{2}", values.get("amount", "")))
            if not valid:
                self.server.failures.append("invalid_create_contract")
                self.respond(401, {"status_code": 401})
                return
            order_id = values["order_id"]
            if order_id in self.server.orders:
                self.server.failures.append("create_retried")
                self.respond(409, {})
                return
            values["trade_id"] = "local-" + secrets.token_hex(12)
            self.server.orders[order_id] = dict(values)
            unknown = self.server.next_unknown
            self.server.next_unknown = False
        if unknown:
            self.respond(503, {"status_code": 503})
            return
        self.respond(200, {"status_code": 200, "data": {
            "order_id": order_id, "trade_id": values["trade_id"], "amount": values["amount"],
            "currency": "CNY", "token": "usdt", "actual_amount": "1.250001",
            "receive_address": "local-address", "status": 1,
            "expiration_time": int(time.time()) + 600,
            "payment_url": self.server.address + "/cashier/" + order_id,
        }})


def notification(order, key):
    return signed({"pid": order["pid"], "trade_id": order["trade_id"],
                   "order_id": order["order_id"], "amount": order["amount"],
                   "actual_amount": "1.250001", "token": "usdt", "status": "2",
                   "receive_address": "local-address", "block_transaction_id": "local-transaction-" + order["trade_id"]}, key)


def wallet(experiment):
    rows = experiment.rows(f"SELECT quota FROM users WHERE id={int(experiment.user_id)}")
    require(len(rows) == 1, "extension_wallet_missing")
    return int(rows[0]["quota"])


def topup_logs(experiment):
    return int(experiment.rows(f"SELECT count(*) AS n FROM logs WHERE user_id={int(experiment.user_id)} AND type=1")[0]["n"])


def snapshot(experiment, trade_no):
    require(re.fullmatch(r"GP[a-zA-Z0-9]{30}", trade_no) is not None, "unexpected_local_order_id")
    rows = experiment.rows("SELECT t.status AS order_status,g.status,g.expected_quota,g.amount_minor,g.currency "
                           "FROM top_ups t JOIN gmpay_orders g ON t.id=g.top_up_id "
                           f"WHERE t.trade_no='{trade_no}' AND t.user_id={int(experiment.user_id)}")
    require(len(rows) == 1, "gmpay_snapshot_missing")
    return rows[0]


def check_settled(experiment, trade_no, before, log_count, expected_quota):
    value = snapshot(experiment, trade_no)
    require(value["status"] == "settled" and value["order_status"] == "success", "gmpay_not_settled")
    require(wallet(experiment) == before + expected_quota, "gmpay_wallet_not_exactly_once")
    require(topup_logs(experiment) == log_count + 1, "gmpay_topup_log_not_exactly_once")


def run_gmpay(experiment, server):
    root, user, other = experiment.root, experiment.user, experiment.other
    config = {"pay_address": server.address, "merchant_id": server.merchant,
              "secret_key": server.key, "enabled": True,
              "assets": [{"name": "Local USDT", "token": "usdt", "network": "tron"}]}
    root.api("POST", "/api/option/payment_compliance", {"confirmed": True})
    root.api("PUT", "/api/option/", {"key": "ServerAddress", "value": user.base})
    root.api("PUT", "/api/option/", {"key": "CustomCallbackAddress", "value": ""})
    try:
        saved = root.api("POST", "/api/option/gmpay/save", config)
        require(saved["has_secret"] is True and "secret_key" not in saved, "gmpay_secret_exposed")
        probe = root.api("POST", "/api/option/gmpay/probe", {"pay_address": server.address})
        require(probe["supported_assets"][0]["network"] == "tron", "gmpay_probe_mismatch")
        public = user.api("GET", "/api/user/topup/info")
        require(public["enable_gmpay_topup"] is True and any(item["type"] == PAY_METHOD for item in public["pay_methods"]), "gmpay_method_missing")
        amount = max(1, int(public["min_topup"]))
        status = user.api("GET", "/api/status")
        if status["quota_display_type"] == "TOKENS":
            amount *= int(status["quota_per_unit"])
        native = payload(user.request("POST", "/api/user/amount", {"amount": amount}), "native_amount")
        require(native.get("message") == "success", "native_amount_failed")
        money = str(native["data"])

        def create():
            result = payload(user.request("POST", "/api/user/gmpay/pay", {"amount": amount, "payment_method": PAY_METHOD}), "gmpay_create")
            require(result.get("message") == "success", "gmpay_create_rejected")
            data = result["data"]
            require(data["currency"] == "CNY" and Decimal(data["pay_amount"]) == Decimal(money), "gmpay_native_amount_mismatch")
            require(isinstance(data["expected_quota"], int) and data["expected_quota"] > 0, "gmpay_native_quota_invalid")
            value = snapshot(experiment, data["trade_no"])
            require(value["expected_quota"] == data["expected_quota"] and value["currency"] == "CNY"
                    and int(value["amount_minor"]) == int(Decimal(money) * 100), "gmpay_creation_snapshot_mismatch")
            require(value["order_status"] == "pending" and value["status"] == "ready", "gmpay_creation_prematurely_settled")
            return data

        before, count = wallet(experiment), topup_logs(experiment)
        data = create()
        trade_no = data["trade_no"]
        require(wallet(experiment) == before, "gmpay_creation_credited_wallet")
        rejected(other.request("GET", "/api/user/gmpay/orders/" + trade_no), "gmpay_cross_user_order_exposed")
        detail = user.api("GET", "/api/user/gmpay/orders/" + trade_no)
        require("merchant_version_id" not in detail["payment"] and "key" not in detail["payment"], "gmpay_order_secret_exposed")
        order = server.orders[trade_no]
        callback = notification(order, server.key)
        invalid = dict(callback, signature="0" * 64)
        require(user.request("POST", NOTIFY_PATH, invalid) == (200, b"fail"), "gmpay_invalid_signature_accepted")
        mismatch = signed(dict(callback, amount=str(Decimal(money) + 1)), server.key)
        require(user.request("POST", NOTIFY_PATH, mismatch) == (200, b"fail"), "gmpay_mismatched_amount_accepted")
        require(wallet(experiment) == before and topup_logs(experiment) == count, "gmpay_rejection_changed_wallet")
        # A configuration rotation cannot invalidate the merchant version bound to this order.
        server.key = secrets.token_hex(24)
        config["secret_key"] = server.key
        root.api("POST", "/api/option/gmpay/save", config)
        gate = threading.Barrier(3)
        def callback_call():
            gate.wait(timeout=10)
            require(user.request("POST", NOTIFY_PATH, callback) == (200, b"success"), "gmpay_parallel_callback_failed")
        def manual_call():
            gate.wait(timeout=10)
            root.api("POST", "/api/user/topup/complete", {"trade_no": trade_no})
        with ThreadPoolExecutor(max_workers=3) as pool:
            jobs = [pool.submit(callback_call), pool.submit(callback_call), pool.submit(manual_call)]
            for job in jobs:
                job.result(timeout=30)
        require(user.request("POST", NOTIFY_PATH, callback) == (200, b"success"), "gmpay_repeat_callback_failed")
        root.api("POST", "/api/user/topup/complete", {"trade_no": trade_no})
        check_settled(experiment, trade_no, before, count, data["expected_quota"])
        experiment.record("gmpay_native_cny_snapshot_rotation_parallel_callback_manual_once")

        before, count = wallet(experiment), topup_logs(experiment)
        data = create()
        root.api("POST", "/api/user/topup/complete", {"trade_no": data["trade_no"]})
        callback = notification(server.orders[data["trade_no"]], server.key)
        require(user.request("POST", NOTIFY_PATH, callback) == (200, b"success"), "gmpay_manual_then_callback_failed")
        check_settled(experiment, data["trade_no"], before, count, data["expected_quota"])
        experiment.record("gmpay_manual_first_callback_remains_once")

        before, count = wallet(experiment), topup_logs(experiment)
        previous = set(server.orders)
        server.next_unknown = True
        result = payload(user.request("POST", "/api/user/gmpay/pay", {"amount": amount, "payment_method": PAY_METHOD}), "gmpay_unknown")
        require(result.get("message") != "success", "gmpay_unknown_falsely_succeeded")
        created = set(server.orders) - previous
        require(len(created) == 1, "gmpay_unknown_create_replayed")
        trade_no = created.pop()
        value = snapshot(experiment, trade_no)
        require(value["status"] == "unknown" and value["order_status"] == "pending" and wallet(experiment) == before, "gmpay_unknown_state_lost")
        require(user.request("POST", NOTIFY_PATH, notification(server.orders[trade_no], server.key)) == (200, b"success"), "gmpay_unknown_callback_failed")
        check_settled(experiment, trade_no, before, count, int(value["expected_quota"]))
        require(not server.failures and len(server.orders) == 3, "gmpay_mock_contract_or_retry_failure")
        experiment.record("gmpay_unknown_outcome_no_retry_late_callback_settles")
    finally:
        config["enabled"] = False
        config["secret_key"] = ""
        root.api("POST", "/api/option/gmpay/save", config)


def run_ticket_pat(experiment, ticket_id):
    root = experiment.root
    context = {"scopes": ["ticket:read", "wallet:write"], "expires_at": int(time.time()) + 7200}
    proof = operation_proof(root, "access_token.generate", experiment.root_password, context)
    token = root.api("POST", "/api/user/access_tokens", {"name": "extension-" + experiment.suffix, **context}, proof=proof)
    try:
        path = f"/api/admin/tickets/{ticket_id}"
        detail = business(root.request("GET", path, bearer=token["token"]), "ticket_pat_read")
        require(detail["ticket"]["id"] == ticket_id, "ticket_pat_read_target")
        for method, target, body, code in (
            ("PATCH", path + "/status", {"status": "closed"}, "ACCESS_TOKEN_SCOPE_DENIED"),
            ("GET", f"/api/tickets/{ticket_id}", None, "AUTH_SESSION_REQUIRED"),
            ("POST", "/api/user/gmpay/pay", {"amount": 1, "payment_method": PAY_METHOD}, "AUTH_SESSION_REQUIRED"),
        ):
            status, raw = root.request(method, target, body, bearer=token["token"])
            require(status == 403 and json.loads(raw).get("code") == code, "extension_pat_scope_or_session_boundary")
        experiment.record("ticket_native_pat_read_scope_and_personal_payment_session_only")
    finally:
        token_id = int(token["item"]["id"])
        proof = operation_proof(root, "access_token.revoke", experiment.root_password, {"token_id": token_id})
        root.api("DELETE", f"/api/user/access_tokens/{token_id}", proof=proof)


def run_tickets(experiment):
    root, user, other = experiment.root, experiment.user, experiment.other
    fields = {"title": "Candidate ticket " + experiment.suffix, "content": "Local acceptance request", "category": "api", "priority": "normal", "model_id": "local-model", "request_id": "local-request"}
    created = business(submit_form(user, "/api/tickets", fields, [("proof.png", "image/png", PNG)]), "ticket_create")
    ticket_id = int(created["ticket"]["id"])
    path = f"/api/tickets/{ticket_id}"
    detail = user.api("GET", path)
    require(detail["ticket"]["status"] == "open" and len(detail["messages"]) == 1, "ticket_create_contract")
    image = detail["messages"][0]["images"][0]
    require(image.startswith("/api/tickets/attachments/"), "ticket_attachment_route")
    require(user.request("GET", image) == (200, PNG), "ticket_attachment_content")
    rejected(other.request("GET", path), "ticket_cross_user_read", "NOT_FOUND")
    rejected(other.request("GET", image), "ticket_cross_user_attachment", "NOT_FOUND")
    rejected(submit_form(other, path + "/messages", {"content": "unauthorized"}), "ticket_cross_user_reply", "NOT_FOUND")
    rejected(other.request("PATCH", path + "/status", {"status": "closed"}), "ticket_cross_user_status", "NOT_FOUND")
    experiment.record("ticket_owner_multipart_attachment_and_cross_user_boundaries")
    run_ticket_pat(experiment, ticket_id)

    admin_path = f"/api/admin/tickets/{ticket_id}"
    admin = root.api("GET", "/api/user/self")
    root.api("PATCH", admin_path + "/assignee", {"assignee_id": admin["id"]})
    reply = business(submit_form(root, admin_path + "/messages", {"content": "Local support reply"}), "ticket_support_reply")
    require(reply["message"]["role"] == "support" and reply["ticket"]["status"] == "replied", "ticket_support_contract")
    root.api("PATCH", admin_path + "/status", {"status": "closed"})
    rejected(submit_form(user, path + "/messages", {"content": "closed reply"}), "ticket_closed_reply", "TICKET_CLOSED")
    user.api("PATCH", path + "/status", {"status": "open"})
    business(submit_form(user, path + "/messages", {"content": "Reopened follow up"}), "ticket_reopened_reply")
    detail = root.api("GET", admin_path)
    require(detail["ticket"]["status"] == "open" and detail["ticket"]["message_count"] == 3
            and detail["ticket"]["assignee_id"] == admin["id"], "ticket_state_or_assignment_lost")
    experiment.record("ticket_native_admin_assignment_reply_close_reopen")

    before = experiment.rows(f"SELECT count(*) AS n FROM tickets WHERE user_id={int(experiment.user_id)}")[0]["n"]
    rejected(submit_form(user, "/api/tickets", fields, [("bad.png", "image/png", b"not an image")]), "ticket_invalid_mime", "ATTACHMENT_TYPE_UNSUPPORTED")
    rejected(submit_form(user, "/api/tickets", fields, [(f"proof-{i}.png", "image/png", PNG) for i in range(6)]), "ticket_attachment_count", "ATTACHMENT_TOO_MANY")
    after = experiment.rows(f"SELECT count(*) AS n FROM tickets WHERE user_id={int(experiment.user_id)}")[0]["n"]
    require(after == before, "ticket_invalid_attachment_created_ticket")
    experiment.record("ticket_attachment_type_and_count_rejected_without_creation")


def route_token(experiment, client, name, model, cleanup):
    client.api("POST", "/api/token/", {"name": name, "remain_quota": 3000,
        "expired_time": -1, "unlimited_quota": False, "type": "manual",
        "model_limits_enabled": True, "model_limits": model})
    items = client.api("GET", "/api/token/?p=1&page_size=100")["items"]
    matches = [item for item in items if item["name"] == name]
    require(len(matches) == 1, "routing_fixture_token_missing")
    token_id = int(matches[0]["id"])

    def remove():
        if experiment.rows(f"SELECT id FROM tokens WHERE id={token_id} AND deleted_at IS NULL"):
            client.api("DELETE", f"/api/token/{token_id}")
        require(not experiment.rows(f"SELECT id FROM tokens WHERE id={token_id} AND deleted_at IS NULL"),
                "routing_fixture_token_cleanup_failed")
    cleanup.callback(remove)
    return token_id


def route_channel(experiment, model, suffix, cleanup):
    original = experiment.root.api("GET", f"/api/channel/{experiment.channel_id}")
    name = "route-" + experiment.suffix + "-" + suffix
    config = {"name": name, "type": 1, "key": experiment.mock_key,
              "base_url": original["base_url"], "models": model,
              "status": 1, "priority": 0, "weight": 100}
    experiment.root.api("POST", "/api/channel/", {"mode": "single", "channel": config})
    rows = experiment.rows(f"SELECT id FROM channels WHERE name='{name}'")
    require(len(rows) == 1, "routing_fixture_channel_missing")
    channel_id = int(rows[0]["id"])

    def remove():
        if experiment.rows(f"SELECT id FROM channels WHERE id={channel_id}"):
            experiment.root.api("DELETE", f"/api/channel/{channel_id}")
        require(not experiment.rows(f"SELECT id FROM channels WHERE id={channel_id}"),
                "routing_fixture_channel_cleanup_failed")
    cleanup.callback(remove)
    experiment.root.api("PUT", "/api/channel/", {"id": channel_id,
        **{key: value for key, value in config.items() if key != "status"}})
    return channel_id


def run_routing_pat(experiment, token_id, foreign_id, channel_id):
    user = experiment.user
    catalog = user.api("GET", "/api/user/access_tokens/catalog")
    personal = [resource for group in catalog["groups"] if group["group"] == "personal"
                for resource in group["resources"] if resource["resource"] == "routing"]
    require(len(personal) == 1 and {action["action"] for action in personal[0]["actions"]} == {"read", "write"},
            "ordinary_user_routing_scope_catalog_missing")
    require(not any(resource["resource"] in {"channel", "ticket", "user", "option"}
                    for group in catalog["groups"] for resource in group["resources"]),
            "ordinary_user_administrator_scope_exposed")
    context = {"scopes": ["routing:read"], "expires_at": int(time.time()) + 7200}
    proof = operation_proof(user, "access_token.generate", experiment.user_password, context)
    token = user.api("POST", "/api/user/access_tokens", {"name": "routing-" + experiment.suffix, **context}, proof=proof)
    identifier, pat = int(token["item"]["id"]), token["token"]
    path = f"/api/token/{token_id}/routing"
    foreign_path = f"/api/token/{foreign_id}/routing"
    try:
        view = business(user.request("GET", path, bearer=pat), "routing_pat_read")
        require(user.request("GET", foreign_path, bearer=pat)[0] == 404, "routing_pat_foreign_owner_read")
        body = {"mode": "manual", "version": view["version"], "entries": [{"channel_id": channel_id, "enabled": True}]}
        require(user.request("PUT", path, body, bearer=pat)[0] == 403, "routing_read_pat_wrote")
        update = {"name": "routing-" + experiment.suffix, "scopes": ["routing:write"]}
        require(user.request("PATCH", f"/api/user/access_tokens/{identifier}", update)[0] == 403, "routing_pat_update_without_step_up")
        proof = operation_proof(user, "access_token.update", experiment.user_password,
                                {"token_id": identifier, "scopes": update["scopes"]})
        user.api("PATCH", f"/api/user/access_tokens/{identifier}", update, proof=proof)
        require(user.request("GET", path, bearer=pat)[0] == 403, "routing_write_pat_read")
        business(user.request("PUT", path, body, bearer=pat), "routing_pat_write")
        require(user.request("PUT", foreign_path, body, bearer=pat)[0] == 404, "routing_pat_foreign_owner_write")
        experiment.record("ordinary_user_routing_pat_catalog_issue_update_scope_ownership")
    finally:
        items = user.api("GET", "/api/user/access_tokens")["items"]
        if any(item["id"] == identifier for item in items):
            proof = operation_proof(user, "access_token.revoke", experiment.user_password, {"token_id": identifier})
            user.api("DELETE", f"/api/user/access_tokens/{identifier}", proof=proof)
        require(user.request("GET", path, bearer=pat)[0] == 401, "routing_pat_cleanup_failed")


def run_routing_candidates(experiment):
    concrete = "gpt-4-gizmo-acceptance-" + experiment.suffix
    with ExitStack() as cleanup:
        channel_id = route_channel(experiment, "gpt-4-gizmo-*", "normalized", cleanup)
        sibling_id = route_channel(experiment, concrete + "-other", "sibling", cleanup)
        token_id = route_token(experiment, experiment.user, "route-" + experiment.suffix, concrete, cleanup)
        foreign_id = route_token(experiment, experiment.other, "route-other-" + experiment.suffix, concrete, cleanup)
        path = f"/api/token/{token_id}/routing"
        before, sends = native_balances(experiment.user_id, token_id), experiment.sends
        candidates = experiment.user.api("GET", path + "/channels")
        require({item["id"] for item in candidates} == {channel_id}, "routing_model_candidate_intersection")
        require(candidates[0]["models"] == [concrete], "routing_candidates_widened_model_names")
        view = experiment.user.api("GET", path)
        body = {"mode": "manual", "version": view["version"], "entries": [{"channel_id": channel_id, "enabled": True}]}
        view = experiment.user.api("PUT", path, body)
        query = urllib.parse.urlencode({"model": concrete, "path": "/v1/chat/completions"})
        preview = experiment.user.api("GET", path + "/preview?" + query)
        require(len(preview["entries"]) == 1 and preview["entries"][0]["eligible"] is True,
                "routing_normalized_preview_unavailable")
        body.update(version=view["version"], entries=[{"channel_id": sibling_id, "enabled": True}])
        status, raw = experiment.user.request("PUT", path, body)
        require(status == 403 and json.loads(raw).get("code") == "ROUTING_CHANNEL_FORBIDDEN",
                "routing_concrete_sibling_accepted")
        run_routing_pat(experiment, token_id, foreign_id, channel_id)
        require_charge(before, native_balances(experiment.user_id, token_id), 0)
        require(experiment.sends == sends, "routing_editor_contacted_upstream")
        experiment.record("routing_concrete_model_candidate_put_preview_no_charge_or_send")


def run_routing_pinned_websocket(experiment):
    root = experiment.root
    with ExitStack() as cleanup:
        username, password = "wsadmin" + experiment.suffix, secrets.token_urlsafe(32)
        proof = operation_proof(root, "admin.user.create", experiment.root_password, {"role": 10})
        root.api("POST", "/api/user/", {"username": username, "password": password,
                 "display_name": username, "role": 10}, proof=proof)
        rows = experiment.rows(f"SELECT id FROM users WHERE username='{username}' AND deleted_at IS NULL")
        require(len(rows) == 1, "routing_admin_fixture_missing")
        user_id = int(rows[0]["id"])

        def remove_admin():
            if experiment.rows(f"SELECT id FROM users WHERE id={user_id} AND deleted_at IS NULL"):
                proof = operation_proof(root, "admin.user.delete", experiment.root_password, {"user_id": user_id})
                root.api("DELETE", f"/api/user/{user_id}", proof=proof)
            require(not experiment.rows(f"SELECT id FROM users WHERE id={user_id} AND deleted_at IS NULL"),
                    "routing_admin_cleanup_failed")
        cleanup.callback(remove_admin)
        admin = Client(experiment.base)
        admin.login(username, password)
        cleanup.callback(logout_verified, admin)
        root.api("POST", "/api/user/manage", {"id": user_id, "action": "add_quota", "mode": "override",
            "value": 10000, "operation_id": "routing-ws-admin-" + experiment.suffix})
        token_id = route_token(experiment, admin, "ws-route-" + experiment.suffix, experiment.model, cleanup)
        key = admin.api("POST", f"/api/token/{token_id}/key")["key"]
        path = f"/api/token/{token_id}/routing"
        view = admin.api("GET", path)
        admin.api("PUT", path, {"mode": "manual", "version": view["version"],
            "entries": [{"channel_id": experiment.channel_id, "enabled": True}]})
        original = root.api("GET", f"/api/channel/{experiment.channel_id}")
        config = {name: original[name] for name in ("id", "name", "type", "models", "channel_ratio", "max_concurrency", "priority", "weight", "base_url", "setting")}
        cleanup.callback(root.api, "PUT", "/api/channel/", config)
        socket = WebSocket.connect(experiment.base, key + "-" + str(experiment.channel_id))
        cleanup.callback(socket.close)
        before, sends = native_balances(user_id, token_id), experiment.sends
        socket.send({"type": "response.create", "model": experiment.model, "input": "local acceptance"})
        terminal = None
        for _ in range(12):
            event = socket.receive()
            if event.get("type") == "response.completed":
                terminal = event["response"]
                break
        require(terminal is not None and terminal["status"] == "completed"
                and terminal["usage"]["total_tokens"] == 12, "routing_pinned_ws_first_terminal")
        deadline = time.monotonic() + 10
        while True:
            after = native_balances(user_id, token_id)
            try:
                require_charge(before, after, 1000)
                break
            except RequirementError:
                if time.monotonic() >= deadline:
                    raise
                time.sleep(0.05)
        require(experiment.sends == sends + 1, "routing_pinned_ws_first_replayed")
        root.api("PUT", "/api/channel/", {**config, "models": experiment.model + "-unavailable"})
        socket.send({"type": "response.create", "model": experiment.model, "input": "must reject"})
        event = socket.receive()
        require(event.get("type") == "error" and event.get("status") == 403, "routing_pinned_ws_stale_ability_accepted")
        closed = False
        try:
            socket.receive()
        except EOFError:
            closed = True
        require(closed, "routing_pinned_ws_policy_connection_not_closed")
        require_charge(after, native_balances(user_id, token_id), 0)
        require(experiment.sends == sends + 1, "routing_pinned_ws_rejected_create_sent")
        require(experiment.rows(f"SELECT request_count FROM users WHERE id={user_id}") == [{"request_count": 1}],
                "routing_pinned_ws_request_count")
        experiment.checks.append({"name": "manual_administrator_pin_ws_ability_revoked",
            "native_wallet_grant": 10000, "charged_quota": 1000, "rejected_create_quota": 0,
            "upstream_requests": 1, "rejection_status": 403, "connection_closed": True,
            "close_code": "not exposed by candidate transport; Go regression verifies 1008"})


def run_extensions(experiment):
    """由已验证候选驱动调用；mock_tls_cert/key 必须由候选 CA 签发。"""
    for client in (experiment.root, experiment.user, experiment.other):
        parsed = urllib.parse.urlsplit(client.base)
        require(parsed.scheme == "http" and parsed.hostname == "127.0.0.1"
                and parsed.port is not None and not parsed.username and not parsed.password
                and parsed.path in ("", "/") and not parsed.query and not parsed.fragment,
                "extensions_require_loopback_candidate")
    require(isinstance(experiment.user_id, int) and experiment.user_id > 0, "extensions_require_candidate_user")
    # The shared rows implementation rejects any target not previously verified.
    require(experiment.rows("SELECT 1 AS extension_target_verified") == [{"extension_target_verified": 1}],
            "extensions_candidate_identity_not_verified")
    cert, key = Path(experiment.mock_tls_cert), Path(experiment.mock_tls_key)
    require(cert.is_file() and key.is_file() and not cert.is_symlink() and not key.is_symlink(), "extensions_mock_tls_files_required")
    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Gateway)
    server.daemon_threads = True
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.load_cert_chain(cert, key)
    server.socket = context.wrap_socket(server.socket, server_side=True)
    server.address = f"https://127.0.0.1:{server.server_port}"
    server.key, server.merchant = secrets.token_hex(24), "local-" + secrets.token_hex(8)
    server.orders, server.failures, server.lock, server.next_unknown = {}, [], threading.Lock(), False
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        run_routing_candidates(experiment)
        run_routing_pinned_websocket(experiment)
        run_gmpay(experiment, server)
        run_tickets(experiment)
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=5)

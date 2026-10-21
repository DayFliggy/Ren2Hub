"""工单与返利原生 HTTP 合同补充验收，仅供隔离本地候选调用。

调用者将工作区 scripts/server 加入 sys.path，完成候选身份校验并构造
既有 Experiment 后调用 run_ticket_regression(experiment)。本模块沿用
真实 Client、会话与安全证明；只通过 API 写业务，rows 仅供既有验收读库。
不要将此入口连接正式环境。通知失败另由本地 Go 通知回归覆盖。
"""
from urllib.parse import urlencode, urlsplit

from ren2hub_acceptance import require
from ren2hub_extensions_acceptance import PNG, business, rejected, run_tickets, submit_form


def _local_candidate(experiment):
    require(urlsplit(experiment.base).hostname in {"127.0.0.1", "localhost", "::1"},
            "ticket_regression_requires_loopback_candidate")


def _ticket_page(client, path, query, ticket_id, expected_count=1):
    page = client.api("GET", path + "?" + urlencode(query))
    require(isinstance(page, dict) and page.get("page") == query["page"]
            and page.get("page_size") == query["page_size"], "ticket_native_pagination")
    require(page.get("total") == 1 and len(page.get("items", [])) == expected_count,
            "ticket_filtered_page_count")
    if expected_count:
        require(page["items"][0]["id"] == ticket_id, "ticket_filtered_page_owner")
    return page


def run_ticket_regression(experiment):
    """复用附件、越权、PAT、关闭/重开验收，再检查队列与自动指派。"""
    _local_candidate(experiment)
    run_tickets(experiment)
    root, user, other = experiment.root, experiment.user, experiment.other
    before = root.api("GET", "/api/admin/tickets/summary")
    title = "Queue acceptance " + experiment.suffix
    created = business(submit_form(user, "/api/tickets", {
        "title": title, "content": "Queue contract check", "category": "billing",
        "priority": "high", "request_id": "queue-" + experiment.suffix,
    }, [("queue.png", "image/png", PNG)]), "ticket_queue_create")
    ticket_id = created["ticket"]["id"]
    require(isinstance(ticket_id, int) and ticket_id > 0, "ticket_queue_id")
    query = {"keyword": title, "status": "open", "page": 1, "page_size": 1}
    _ticket_page(user, "/api/tickets", query, ticket_id)
    _ticket_page(user, "/api/tickets", {**query, "page": 2}, ticket_id, 0)
    require(other.api("GET", "/api/tickets?" + urlencode(query))["total"] == 0,
            "ticket_other_user_list_leaked")
    admin_query = {**query, "category": "billing", "priority": "high", "assignee": "unassigned"}
    _ticket_page(root, "/api/admin/tickets", admin_query, ticket_id)
    summary = root.api("GET", "/api/admin/tickets/summary")
    require(summary == {"pending": before["pending"] + 1,
                        "unassigned": before["unassigned"] + 1,
                        "mine": before["mine"]}, "ticket_queue_summary_create")
    admin = root.api("GET", "/api/user/self")
    agents = root.api("GET", "/api/admin/tickets/agents")
    require(any(agent["id"] == admin["id"] for agent in agents["items"]),
            "ticket_reply_agent_missing")
    admin_path = f"/api/admin/tickets/{ticket_id}"
    detail = root.api("GET", admin_path)
    require(detail["ticket"]["assignee_id"] is None, "ticket_premature_assignment")
    image = detail["messages"][0]["images"][0]
    require(image.startswith("/api/admin/tickets/attachments/")
            and root.request("GET", image) == (200, PNG), "ticket_admin_attachment_content")
    for method, path, body in (
        ("GET", "/api/admin/tickets", None),
        ("GET", "/api/admin/tickets/summary", None),
        ("GET", "/api/admin/tickets/agents", None),
        ("GET", admin_path, None),
        ("GET", image, None),
        ("PATCH", admin_path + "/assignee", {"assignee_id": admin["id"]}),
        ("PATCH", admin_path + "/status", {"status": "closed"}),
    ):
        require(user.request(method, path, body)[0] == 403,
                "ordinary_user_ticket_administration_allowed")
    require(submit_form(other, admin_path + "/messages", {"content": "denied"})[0] == 403,
            "ordinary_user_support_reply_allowed")
    reply = business(submit_form(root, admin_path + "/messages", {"content": "Automatically assigned reply"}),
                     "ticket_auto_assigned_reply")
    require(reply["message"]["role"] == "support" and reply["ticket"]["status"] == "replied"
            and reply["ticket"]["assignee_id"] == admin["id"], "ticket_first_reply_auto_assignment")
    _ticket_page(root, "/api/admin/tickets", {**query, "status": "replied", "assignee": "mine"}, ticket_id)
    summary = root.api("GET", "/api/admin/tickets/summary")
    require(summary == {"pending": before["pending"], "unassigned": before["unassigned"],
                        "mine": before["mine"] + 1}, "ticket_queue_summary_reply")
    unassigned = root.api("PATCH", admin_path + "/assignee", {"assignee_id": None})
    require(unassigned["ticket"]["assignee_id"] is None
            and unassigned["ticket"]["assigned_at"] == 0, "ticket_native_unassign")
    require(root.api("GET", admin_path)["ticket"]["assignee_id"] is None,
            "ticket_unassign_not_persisted")
    rejected(user.request("GET", "/api/tickets?status=invalid"), "ticket_invalid_status_filter", "VALIDATION_ERROR")
    root.api("PATCH", admin_path + "/status", {"status": "closed"})
    experiment.record("ticket_native_pagination_filters_agents_summary_autoassign_unassign_and_admin_boundaries")
    return {"ticket_id": ticket_id}


def run_affiliate_read_contract(experiment):
    """核对新图表字段与用户隐私投影；不制造资金或改变邀请归因。"""
    _local_candidate(experiment)
    info = experiment.user.api("GET", "/api/user/affiliate")
    series = info.get("monthly_series")
    require(isinstance(series, list) and len(series) == 12, "affiliate_twelve_month_window")
    previous = None
    for point in series:
        require(isinstance(point.get("month"), str)
                and all(type(point.get(key)) is int and point[key] >= 0
                        for key in ("new_count", "cumulative", "reward", "quota")),
                "affiliate_month_native_fields")
        if previous is not None:
            require(point["cumulative"] == previous + point["new_count"],
                    "affiliate_month_cumulative_contract")
        previous = point["cumulative"]
    for order in ("asc", "desc"):
        page = experiment.user.api("GET", "/api/user/affiliate/invitees?" + urlencode({"p": 1, "page_size": 1, "order": order}))
        require(page["page"] == 1 and page["page_size"] == 1, "affiliate_invitee_pagination")
        for invitee in page["items"]:
            require(set(invitee) == {"masked_name", "invited_at", "reward_quota"},
                    "affiliate_invitee_private_field_exposed")
    rejected(experiment.user.request("POST", "/api/user/affiliate/transfer", {"amount": 1, "operation_id": "rejected-" + experiment.suffix}),
             "affiliate_partial_transfer_not_rejected")
    experiment.record("affiliate_native_monthly_counts_masked_invitees_and_full_transfer_body")

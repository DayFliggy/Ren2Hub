#!/usr/bin/env python3
"""在仅有回环网络的 namespace 中验证本地构建产物，不接入已有服务。"""
from __future__ import annotations

import argparse
from contextlib import contextmanager
import hashlib
import json
import os
from pathlib import Path
import secrets
import shutil
import signal
import socket
import stat
import subprocess
import sys
import time
import urllib.request

PROJECTS = ("desktop-light", "desktop-dark", "mobile-light", "mobile-dark")


def clean_env() -> dict[str, str]:
    # 只保留工具运行和显式浏览器缓存路径；不继承生产设置、代理或 LD_LIBRARY_PATH。
    allowed = ("PATH", "HOME", "LANG", "TZ", "PLAYWRIGHT_BROWSERS_PATH")
    return {key: os.environ[key] for key in allowed if key in os.environ}


def reject_links(path: Path) -> Path:
    path = Path(os.path.abspath(path))
    for entry in (*reversed(path.parents), path):
        try:
            metadata = entry.lstat()
        except FileNotFoundError:
            continue
        if stat.S_ISLNK(metadata.st_mode):
            raise RuntimeError("private output paths must not contain symbolic links")
        if stat.S_ISREG(metadata.st_mode) and metadata.st_nlink != 1:
            raise RuntimeError("private files must not have hard links")
    return path


def private_open(path: Path):
    reject_links(path)
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    return os.fdopen(descriptor, "wb")


def write_json(path: Path, value: object) -> None:
    with private_open(path) as stream:
        stream.write((json.dumps(value, ensure_ascii=False, indent=2) + "\n").encode())


def read_json(path: Path) -> dict:
    reject_links(path)
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(descriptor, "r") as stream:
        metadata = os.fstat(stream.fileno())
        if not stat.S_ISREG(metadata.st_mode) or metadata.st_nlink != 1 or metadata.st_uid != os.getuid():
            raise RuntimeError("evidence must be a regular file owned by the current user without hard links")
        return json.load(stream)


@contextmanager
def interruptible():
    def terminate(_signal, _frame):
        raise KeyboardInterrupt("browser validation interrupted")

    previous = signal.signal(signal.SIGTERM, terminate)
    try:
        yield
    finally:
        signal.signal(signal.SIGTERM, previous)


def stop(process: subprocess.Popen) -> None:
    if process.poll() is not None:
        return
    try:
        os.killpg(process.pid, signal.SIGTERM)
    except ProcessLookupError:
        return
    try:
        process.wait(timeout=10)
    except subprocess.TimeoutExpired:
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        process.wait(timeout=5)


def stop_namespace(process: subprocess.Popen, output: Path) -> None:
    # 先终止 namespace 的 PID 1，让其 finally 清理凭据；unshare 监督进程保持等待。
    children = Path(f"/proc/{process.pid}/task/{process.pid}/children")
    try:
        child_pids = [int(value) for value in children.read_text().split()]
    except FileNotFoundError:
        child_pids = []
    for pid in child_pids:
        try:
            os.kill(pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
    try:
        process.wait(timeout=25)
    except subprocess.TimeoutExpired:
        for pid in child_pids:
            try:
                os.kill(pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
        stop(process)
    finally:
        # 强制终止时内核负责杀掉 namespace 后代；外层仍移除本轮生成的凭据文件。
        for project in PROJECTS:
            session = output / (project + "-private") / "session.json"
            reject_links(session)
            session.unlink(missing_ok=True)


def api(base: str, path: str, body: dict | None = None) -> dict:
    request = urllib.request.Request(base + path, data=json.dumps(body).encode() if body is not None else None,
                                     headers={"Content-Type": "application/json"})
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    with opener.open(request, timeout=3) as response:
        value = json.load(response)
    if value.get("success") is not True:
        raise RuntimeError("native API rejected " + path)
    return value


def exercise(source: Path, output: Path, binary: Path, project: str) -> dict:
    private = output / (project + "-private")
    private.mkdir(mode=0o700)
    for name in ("logs", "attachments"):
        (private / name).mkdir(mode=0o700)
    with socket.socket() as reservation:
        reservation.bind(("127.0.0.1", 0))
        port = reservation.getsockname()[1]
    base = f"http://127.0.0.1:{port}"
    accounts = {role: {"username": prefix + secrets.token_hex(4), "password": "Local9!" + secrets.token_urlsafe(24)}
                for role, prefix in (("root", "r"), ("user", "u"), ("peer", "p"))}
    env = clean_env()
    env.update(PORT=str(port), SQLITE_PATH=str(private / "app.db"), SESSION_SECRET=secrets.token_hex(32),
               CRYPTO_SECRET=secrets.token_hex(32), SESSION_COOKIE_SECURE="false", GIN_MODE="release",
               TICKET_UPLOAD_DIR=str(private / "attachments"), DEFAULT_LANGUAGE="en")
    # UI suites share these synthetic accounts. Keep critical rate limiting
    # enabled at its native threshold, but avoid a preceding suite consuming
    # the next suite's entire 20-minute window in this isolated fixture.
    env["CRITICAL_RATE_LIMIT_DURATION"] = "1"
    result = {"project": project, "success": False, "fresh_database": True}
    server = None
    browser = None
    try:
        with private_open(private / "backend.log") as log:
            server = subprocess.Popen([str(binary), "--log-dir", str(private / "logs")], cwd=private,
                                      env=env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
            deadline = time.monotonic() + 40
            while True:
                if server.poll() is not None:
                    raise RuntimeError("local backend exited before readiness")
                try:
                    setup = api(base, "/api/setup")["data"]
                    break
                except (OSError, ValueError):
                    if time.monotonic() >= deadline:
                        raise RuntimeError("local backend readiness timed out") from None
                    time.sleep(0.2)
            if setup.get("status") or setup.get("root_init"):
                raise RuntimeError("backend did not start with a fresh database")
            root = accounts["root"]
            api(base, "/api/setup", {**root, "confirmPassword": root["password"], "SelfUseModeEnabled": False, "DemoSiteEnabled": False})
            api(base, "/api/user/register", accounts["user"])
            api(base, "/api/user/register", accounts["peer"])
            config = private / "session.json"
            write_json(config, {"baseURL": base, "accounts": accounts, "project": project})
            browser_env = clean_env()
            browser_env.update(REN2HUB_LOCAL_SESSION=str(config), REN2HUB_LOCAL_OUTPUT=str(private), CI="1")
            lib_dir = os.environ.get("REN2HUB_BROWSER_LIB_DIR")
            if lib_dir:
                browser_env["LD_LIBRARY_PATH"] = str(Path(lib_dir).resolve(strict=True))
            with private_open(private / "playwright.log") as browser_log:
                browser = subprocess.Popen([shutil.which("node") or "node", "node_modules/@playwright/test/cli.js", "test", "--project", project],
                                           cwd=source / "frontend", env=browser_env, stdout=browser_log,
                                           stderr=subprocess.STDOUT, start_new_session=True)
                code = browser.wait(timeout=180)
            report = read_json(private / "results.json")
            result.update(report)
            result["success"] = code == 0 and report.get("status") == "passed" and report.get("passed", 0) > 0 and report.get("failed") == 0 and report.get("skipped") == 0
    except (OSError, ValueError, RuntimeError, subprocess.TimeoutExpired) as error:
        # 公开结果仅含受控异常类别；原始输出限定在 0700 私有目录内。
        result["error_type"] = type(error).__name__
    finally:
        if browser is not None:
            stop(browser)
        if server is not None:
            stop(server)
        (private / "session.json").unlink(missing_ok=True)
    write_json(output / (project + ".json"), result)
    return result


def isolated(source: Path, output: Path, binary: Path) -> dict:
    interfaces = json.loads(subprocess.check_output(["ip", "-json", "link", "show"], text=True))
    if [entry["ifname"] for entry in interfaces] != ["lo"]:
        raise RuntimeError("browser worker requires a network namespace containing only loopback")
    subprocess.run(["ip", "link", "set", "lo", "up"], check=True, capture_output=True)
    # 新 network namespace 仅有 lo；后端即使监听 :PORT 也没有对外接口。
    results = [exercise(source, output, binary, name) for name in PROJECTS]
    return {"success": all(row["success"] for row in results), "network": "loopback-only-namespace",
            "binary_sha256": hashlib.sha256(binary.read_bytes()).hexdigest(), "projects": results}


def run(source: Path, output: Path, binary: Path) -> dict:
    source, binary = source.resolve(strict=True), binary.resolve(strict=True)
    output = reject_links(output)
    output.mkdir(parents=True, exist_ok=True, mode=0o700)
    if any(output.iterdir()):
        raise RuntimeError("browser output directory must be empty to prevent stale evidence")
    output.chmod(0o700)
    if not (source / "frontend/node_modules/@playwright/test/cli.js").is_file():
        raise RuntimeError("Playwright dependencies must be installed before browser validation")
    env = clean_env()
    if os.environ.get("REN2HUB_BROWSER_LIB_DIR"):
        env["REN2HUB_BROWSER_LIB_DIR"] = os.environ["REN2HUB_BROWSER_LIB_DIR"]
    command = ["unshare", "--user", "--map-root-user", "--net", "--pid", "--fork", "--kill-child=SIGTERM",
               sys.executable, "-B", str(Path(__file__).resolve()), "--source", str(source), "--output", str(output),
               "--binary", str(binary), "--isolated"]
    with interruptible(), private_open(output / "namespace.log") as log:
        process = subprocess.Popen(command, env=env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
        try:
            code = process.wait(timeout=900)
        except subprocess.TimeoutExpired:
            code = 124
        finally:
            stop_namespace(process, output)
    report_file = output / "browser.json"
    if not report_file.exists():
        write_json(report_file, {"success": False, "error_type": "NamespaceFailure", "exit_code": code})
    report = read_json(report_file)
    if code != 0:
        report["success"] = False
    return report


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("source", "output", "binary"):
        parser.add_argument("--" + name, required=True, type=Path)
    parser.add_argument("--browser-lib-dir", type=Path, help="显式指定 Chromium 运行库目录，不继承 LD_LIBRARY_PATH")
    parser.add_argument("--isolated", action="store_true", help=argparse.SUPPRESS)
    args = parser.parse_args()
    if args.browser_lib_dir:
        os.environ["REN2HUB_BROWSER_LIB_DIR"] = str(args.browser_lib_dir.resolve(strict=True))
    if args.isolated:
        # 不接受用户直接绕过 namespace：宿主只有一个 PID namespace init。
        if os.getpid() != 1:
            raise RuntimeError("isolated worker requires a new PID namespace")
        with interruptible():
            report = isolated(args.source, args.output, args.binary)
        write_json(args.output / "browser.json", report)
    else:
        report = run(args.source, args.output, args.binary)
    print(json.dumps({"success": report["success"], "report": str(args.output / "browser.json")}))
    return 0 if report["success"] else 1


if __name__ == "__main__":
    raise SystemExit(main())

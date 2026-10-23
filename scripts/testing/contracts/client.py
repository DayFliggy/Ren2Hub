import http.cookiejar
import http.client
import http.server
import json
import os
import secrets
import urllib.error
import urllib.parse
import urllib.request
from .common import login as account_login


class APIError(RuntimeError):
    """A failed API operation without response contents or credentials."""


class LoopbackClientHandler(urllib.request.HTTPHandler):
    """Give each simulated user its own real loopback source address."""
    def __init__(self):
        super().__init__()
        self.address = f'127.77.{secrets.randbelow(254) + 1}.{secrets.randbelow(254) + 1}'

    def http_open(self, request):
        if urllib.parse.urlsplit(request.full_url).hostname != '127.0.0.1':
            raise ValueError('acceptance HTTP peers must use loopback')
        return self.do_open(lambda host, **kwargs: http.client.HTTPConnection(
            host, source_address=(self.address, 0), **kwargs), request)


class Client:
    def __init__(self, base, *, redirect_handler=None, session_origin=None):
        self.base, self.bearer = base, ""
        if session_origin or os.environ.get('REN2HUB_ACCEPTANCE_SESSION_ORIGIN'):
            raise ValueError('candidate_sessions_require_loopback')
        loopback = LoopbackClientHandler()
        handlers = [urllib.request.ProxyHandler({}), loopback,
                    urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar())]
        if redirect_handler is not None:
            handlers.append(redirect_handler)
        self.opener = urllib.request.build_opener(*handlers)

    def request(self, method, path, body=None, bearer=None, proof=None, headers=None):
        origin = self.base
        headers = {"Content-Type": "application/json", "Origin": origin, **(headers or {})}
        if proof:
            headers["X-Security-Proof"] = proof
        credential = self.bearer if bearer is None else bearer
        if credential:
            headers["Authorization"] = "Bearer " + credential
        request = urllib.request.Request(origin + path,
                    data=body if isinstance(body, bytes) else (None if body is None else json.dumps(body).encode()),
                    headers=headers, method=method)
        try:
            with self.opener.open(request, timeout=20) as response:
                return response.status, response.read()
        except urllib.error.HTTPError as error:
            return error.code, error.read()

    def api(self, method, path, body=None, proof=None):
        status, raw = self.request(method, path, body, proof=proof)
        value = json.loads(raw)
        if status != 200 or not value.get("success"):
            raise APIError(f"API failed: {method} {urllib.parse.urlsplit(path).path} status={status}")
        return value.get("data")

    def login(self, username, password):
        value = account_login(self, username, password)
        self.bearer = value["access_token"]
        return value["user"]

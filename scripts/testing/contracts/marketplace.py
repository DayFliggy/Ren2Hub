"""服务端社区安装上报由本地 TLS 代理接收；代理从不连接其他地址。"""
from contextlib import contextmanager
import http.server
import json
import ssl
import threading

from .common import require

COMMUNITY_INDEX = 'https://www.newapi.ai/api/v1/community/index.json'
PROXY_PORT = 41749
ALLOWED_AUTHORITIES = frozenset(('www.newapi.ai:443', 'www.newapi.pro:443'))


class InstallHandler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_args):
        pass

    def do_POST(self):
        try:
            size = int(self.headers.get('Content-Length', '0'))
            require(self.path == '/api/v1/community/installs' and 0 < size <= 4096,
                    'marketplace_unexpected_report_request')
            value = json.loads(self.rfile.read(size))
            require(set(value) == {'key', 'version', 'gateway'}
                    and all(isinstance(item, str) and item for item in value.values()),
                    'marketplace_invalid_report_payload')
            self.server.reports.append(value)
            self.send_response(200)
            self.send_header('Content-Length', '0')
            self.end_headers()
        except Exception:
            self.server.failures.append('marketplace_report_invalid')
            self.send_error(400)
        finally:
            self.server.received.set()


class ProxyHandler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_args):
        pass

    def do_CONNECT(self):
        if self.path not in ALLOWED_AUTHORITIES:
            self.server.rejections += 1
            self.send_error(403)
            return
        self.server.tunnels += 1
        self.send_response(200, 'Connection Established')
        self.end_headers()
        self.wfile.flush()
        self.close_connection = True
        try:
            with self.server.tls.wrap_socket(self.connection, server_side=True) as connection:
                InstallHandler(connection, self.client_address, self.server)
        except (OSError, ssl.SSLError):
            self.server.failures.append('marketplace_tls_failed')
            self.server.received.set()

    def do_GET(self):
        self.server.rejections += 1
        self.send_error(403)

    do_POST = do_GET


@contextmanager
def local_marketplace(certificate, private_key, port=PROXY_PORT):
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.load_cert_chain(certificate, private_key)
    server = http.server.ThreadingHTTPServer(('127.0.0.1', port), ProxyHandler)
    server.tls, server.reports, server.failures = context, [], []
    server.tunnels = server.rejections = 0
    server.received = threading.Event()
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        yield server
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=5)
        require(not thread.is_alive(), 'marketplace_proxy_shutdown_failed')


def upload_community_plugin(experiment, body):
    with local_marketplace(experiment.mock_tls_cert, experiment.mock_tls_key) as server:
        experiment.root.api('POST', '/api/plugin/task', {**body, 'marketplaceIndexUrl': COMMUNITY_INDEX})
        require(server.received.wait(15), 'marketplace_install_report_not_received_locally')
        require(not server.failures and server.rejections == 0 and server.tunnels == 1
                and len(server.reports) == 1, 'marketplace_report_not_exactly_once')
        report = server.reports[0]
        require(report['key'] == 'candidate_provider_' + experiment.suffix and report['version'] == '1.0.0',
                'marketplace_report_identity_mismatch')
        experiment.marketplace = {'reports_received': len(server.reports), 'tls_tunnels': server.tunnels,
                                  'proxy_rejections': server.rejections,
                                  'destination': 'loopback TLS simulator'}
        experiment.record('community_marketplace_install_report_received_only_by_local_tls_proxy')

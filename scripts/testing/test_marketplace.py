"""真实 CONNECT/TLS 合同：社区报告只到本地，任意其他目标拒绝。"""
import http.client
import json
from pathlib import Path
import ssl
import subprocess
import tempfile
import unittest

from contracts.marketplace import local_marketplace


class MarketplaceProxyTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.directory = tempfile.TemporaryDirectory()
        directory = Path(cls.directory.name)
        cls.key, cls.cert = directory / 'key.pem', directory / 'cert.pem'
        subprocess.run(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1',
                        '-subj', '/CN=Local Candidate', '-addext',
                        'subjectAltName=DNS:www.newapi.ai,DNS:www.newapi.pro',
                        '-keyout', str(cls.key), '-out', str(cls.cert)],
                       capture_output=True, check=True, timeout=30)

    @classmethod
    def tearDownClass(cls):
        cls.directory.cleanup()

    def test_tls_install_report_is_received_locally_and_other_targets_rejected(self):
        with local_marketplace(self.cert, self.key, port=0) as server:
            context = ssl.create_default_context(cafile=str(self.cert))
            connection = http.client.HTTPSConnection('127.0.0.1', server.server_port, context=context, timeout=3)
            connection.set_tunnel('www.newapi.ai', 443)
            report = {'key': 'alice_local', 'version': '1.0.0', 'gateway': 'synthetic-gateway'}
            connection.request('POST', '/api/v1/community/installs', json.dumps(report),
                               headers={'Content-Type': 'application/json'})
            response = connection.getresponse()
            self.assertEqual(response.status, 200)
            response.read()
            connection.close()
            self.assertEqual(server.reports, [report])
            self.assertEqual(server.tunnels, 1)
            self.assertEqual(server.failures, [])
            blocked = http.client.HTTPConnection('127.0.0.1', server.server_port, timeout=3)
            blocked.request('CONNECT', 'foreign.example:443')
            self.assertEqual(blocked.getresponse().status, 403)
            blocked.close()
            self.assertEqual(server.rejections, 1)
            self.assertEqual(server.tunnels, 1)

    def test_invalid_install_payload_is_not_counted_as_success(self):
        with local_marketplace(self.cert, self.key, port=0) as server:
            connection = http.client.HTTPSConnection('127.0.0.1', server.server_port,
                            context=ssl.create_default_context(cafile=str(self.cert)), timeout=3)
            connection.set_tunnel('www.newapi.pro', 443)
            connection.request('POST', '/api/v1/community/installs', '{}')
            self.assertEqual(connection.getresponse().status, 400)
            connection.close()
            self.assertEqual(server.reports, [])
            self.assertEqual(server.failures, ['marketplace_report_invalid'])


if __name__ == '__main__':
    unittest.main()

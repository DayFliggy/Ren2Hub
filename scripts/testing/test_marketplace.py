"""真实 CONNECT/TLS 合同：社区报告只到本地，任意其他目标拒绝。"""
import http.client
import json
import os
from pathlib import Path
import shutil
import ssl
import subprocess
import tempfile
import unittest

from contracts.marketplace import local_marketplace
from contracts.common import candidate_no_proxy, RequirementError


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

    def test_go_proxy_from_environment_bypasses_only_the_owned_simulator(self):
        go = shutil.which('go')
        self.assertIsNotNone(go, 'Go is required to validate the real application proxy selection')
        simulator = '192.168.176.5'
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source, binary = root / 'main.go', root / 'proxy-probe'
            source.write_text('''package main
import ("encoding/json"; "net/http"; "os")
func main() {
 result := map[string]string{}
 for _, target := range os.Args[1:] {
  request, err := http.NewRequest("GET", target, nil); if err != nil { panic(err) }
  proxy, err := http.ProxyFromEnvironment(request); if err != nil { panic(err) }
  result[target] = "DIRECT"; if proxy != nil { result[target] = proxy.String() }
 }
 if err := json.NewEncoder(os.Stdout).Encode(result); err != nil { panic(err) }
}
''')
            environment = {**os.environ, 'GOWORK': 'off', 'GOTOOLCHAIN': 'local', 'GOPROXY': 'off',
                           'GOENV': 'off', 'GOFLAGS': '', 'CGO_ENABLED': '0'}
            built = subprocess.run([go, 'build', '-o', str(binary), str(source)], env=environment,
                                   capture_output=True, timeout=60)
            self.assertEqual(built.returncode, 0, built.stderr.decode())
            targets = ['http://' + simulator + ':19091/v1/chat/completions',
                       'https://' + simulator + ':19092/v1/responses',
                       'https://www.newapi.ai/api/v1/community/installs',
                       'https://www.newapi.pro/api/v1/community/installs',
                       'http://192.168.176.6:19091/v1/chat/completions', 'https://unrelated.example/']
            proxy = 'http://127.0.0.1:41749'
            for no_proxy, direct in ((candidate_no_proxy(), False), (candidate_no_proxy(simulator), True)):
                # No request is sent: this invokes Go's actual transport proxy
                # selector in a fresh process, avoiding its environment cache.
                process = subprocess.run([str(binary), *targets], env={
                    'HTTP_PROXY': proxy, 'HTTPS_PROXY': proxy, 'NO_PROXY': no_proxy},
                    capture_output=True, check=True, timeout=10)
                actual = json.loads(process.stdout)
                self.assertEqual(actual, {target: ('DIRECT' if direct and index < 2 else proxy)
                                          for index, target in enumerate(targets)})

    def test_no_proxy_rejects_ranges_wildcards_and_multiple_hosts(self):
        for address in ('*', '192.168.0.0/16', '192.168.176.5,foreign.example', '0.0.0.0', '224.0.0.1'):
            with self.subTest(address=address), self.assertRaises(RequirementError):
                candidate_no_proxy(address)


if __name__ == '__main__':
    unittest.main()

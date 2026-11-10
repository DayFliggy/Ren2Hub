"""通过可变供应商/许可/账本夹具检查成功断开合同，不依赖 HTTP 或 Docker。"""
from copy import deepcopy
from types import SimpleNamespace
import unittest
from unittest.mock import patch

from contracts.common import RequirementError
from contracts.routing_cases import client_disconnect_settles_once_and_releases
from routing_result import REQUIRED_CASES, evaluate


class DisconnectFixture:
    def __init__(self, fault=None):
        self.fault = fault
        self.current_case = 'client_disconnect_settles_once_and_releases'
        self.user_id, self.channel_map, self.model, self.key = 7, {'a':11}, 'route-test', 'fixture-only'
        self.exchanges, self.statistics, self.events = [], {}, []
        self.occupied, self.disconnected, self.releases, self.charge = 0, False, 0, 0
        self.hits = []
        self.root = SimpleNamespace(api=self.api)
        self.sim = SimpleNamespace(scenario=lambda *a, **k: None, cursor=lambda:0,
                                   records=lambda *a:deepcopy(self.hits), release=self.release)
        self.connection = SimpleNamespace(request=self.start, close=lambda:None,
                                           sock=SimpleNamespace(shutdown=self.disconnect))

    def route(self, *a, **k): pass
    def configure(self, *a, **k): pass
    def policy(self, *a, **k): pass

    def start(self, *a):
        self.occupied = 1
        self.hits = [{'channel':'a', 'outcome':'held', 'status':0}]
        self.events.append('held')

    def disconnect(self, *a):
        self.disconnected = True
        self.events.append('disconnect')
        if self.fault == 'early_release':
            self.occupied = 0
            self.hits[0]['outcome'] = 'cancelled'

    def api(self, method, path):
        return {'users':[{'id':7, 'in_flight':self.occupied}],
                'channels':[{'id':11, 'in_flight':self.occupied}]}

    def request(self, expected_charge):
        assert self.disconnected and expected_charge == 0
        self.events.append('busy_probe')
        if self.fault == 'replay_before_release':
            self.hits.append(dict(self.hits[0]))
        return (200 if self.fault == 'lost_admission' else 429), b''

    def release(self, name):
        self.releases += 1
        self.events.append('release')
        self.occupied = 1 if self.fault == 'permit_leak' else 0
        self.charge = 0 if self.fault == 'refund' else 2000 if self.fault == 'double_charge' else 1000
        for row in self.hits:
            row.update(outcome='success', status=200, finished_at='2026-11-05T00:00:00Z')
        if self.fault == 'replay_after_release':
            self.hits.append(dict(self.hits[0]))

    def balances(self): return {'wallet':100000-self.charge}

    def settled(self, before, amount):
        if before['wallet']-self.balances()['wallet'] != amount:
            raise RequirementError('settlement_amount_mismatch')
        return self.balances()

    def zero(self):
        if self.occupied:
            raise RequirementError('routing_permit_leak')
        return {'user':0, 'channels':{'a':0}}

    def rows(self, sql):
        if 'MAX(id)' in sql: return [{'id':8}]
        logs = [{'request_id':'native-request-id', 'type':2, 'quota':1000}]
        if self.fault == 'duplicate_settlement':
            logs.append({'request_id':'native-request-id', 'type':2, 'quota':1000})
            logs.append({'request_id':'native-request-id', 'type':6, 'quota':1000})
        return logs


class ClientDisconnectTests(unittest.TestCase):
    def run_case(self, fixture):
        with patch('contracts.routing_cases.http.client.HTTPConnection', return_value=fixture.connection):
            return client_disconnect_settles_once_and_releases(fixture)

    def test_success_disconnect_holds_until_release_then_charges_once(self):
        fixture = DisconnectFixture()
        result = self.run_case(fixture)
        self.assertEqual(fixture.events, ['held','disconnect','busy_probe','release'])
        self.assertEqual((fixture.releases, result['charge'], result['settlements']), (1,1000,1))
        evidence = fixture.exchanges[0]
        self.assertEqual(evidence['request_id'], 'native-request-id')
        self.assertEqual(evidence['occupancy_after_disconnect'], {'user':1,'channel':1})
        self.assertEqual(evidence['actual_charge'], 1000)

    def test_rejects_refund_early_release_replay_duplicate_and_leak(self):
        for fault in ('early_release','lost_admission','replay_before_release','refund','double_charge',
                      'replay_after_release','duplicate_settlement','permit_leak'):
            with self.subTest(fault=fault):
                fixture = DisconnectFixture(fault)
                with self.assertRaises(RequirementError): self.run_case(fixture)
                self.assertEqual(fixture.releases, 1, '错误路径也必须释放事件屏障')

    def test_send_failure_still_releases_supplier_barrier(self):
        fixture = DisconnectFixture()
        fixture.connection.request = lambda *a: (_ for _ in ()).throw(OSError('fixture send failure'))
        with self.assertRaises(OSError): self.run_case(fixture)
        self.assertEqual(fixture.releases, 1)

    def test_old_cancel_refund_case_cannot_satisfy_approved_inventory(self):
        cases = [{'name':name,'status':'pass'} for name in REQUIRED_CASES]
        renamed = next(row for row in cases if row['name']=='client_disconnect_settles_once_and_releases')
        renamed['name'] = 'client_cancel_refund_release'
        report = evaluate(cases, REQUIRED_CASES)
        self.assertEqual(report['exit_code'], 1)
        self.assertIn('client_disconnect_settles_once_and_releases', report['missing_cases'])

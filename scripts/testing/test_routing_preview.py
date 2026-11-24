"""Responses WebSocket 渠道开关关闭时拒绝建连创建并零扣费，重新启用后恢复。"""
import json
from types import SimpleNamespace
import unittest
from unittest.mock import patch

from contracts.common import RequirementError
from contracts.routing_cases import responses_websocket_ineligible_channel


class WebSocketEligibilityFixture:
    def __init__(self, fault=None):
        self.fault = fault
        self.base, self.key, self.model = 'http://fixture', 'fixture-only', 'route-test'
        self.user_id, self.token_id, self.channel_map = 7, 8, {'a': 11}
        self.current_case = 'responses_websocket_ineligible_channel'
        self.exchanges, self.statistics = [], {}
        self.enabled = True
        self.successful_creates = 0
        self.sim = SimpleNamespace(
            cursor=lambda: 0,
            records=lambda *a: [{'hit': True}] if self.fault == 'supplier_hit' else [],
        )
        self.connection = SimpleNamespace(
            request_id='handshake',
            send=lambda value: None,
            receive=lambda: {'type': 'response.completed' if self.fault == 'accepted' else 'error'},
            close=lambda: None,
        )

    def route(self, *a, **k):
        pass

    def balances(self):
        return {'wallet': 100000}

    def settled(self, before, amount):
        assert amount == 0
        return before

    def configure(self, name, setting):
        self.enabled = json.loads(setting)['responses_websocket_enabled']

    def websocket(self):
        assert self.enabled
        self.successful_creates += 1


class WebSocketEligibilityTests(unittest.TestCase):
    def run_case(self, fixture):
        with patch('contracts.websocket.WebSocket.connect', return_value=fixture.connection):
            return responses_websocket_ineligible_channel(fixture)

    def test_websocket_disabled_rejects_then_enabled_completes(self):
        fixture = WebSocketEligibilityFixture()
        result = self.run_case(fixture)
        self.assertEqual(result['actual_event'], 'error')
        self.assertEqual(result['enabled_actual_event'], 'response.completed')
        self.assertEqual(fixture.successful_creates, 1)
        self.assertTrue(fixture.enabled)

    def test_ineligible_usage_fails_and_restores_enabled_setting(self):
        for fault in ('accepted', 'supplier_hit'):
            with self.subTest(fault=fault):
                fixture = WebSocketEligibilityFixture(fault)
                with self.assertRaises(RequirementError):
                    self.run_case(fixture)
                self.assertTrue(fixture.enabled)
                self.assertEqual(fixture.successful_creates, 0)

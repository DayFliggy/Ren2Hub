"""预览以显式 transport 区分 HTTP stream 与 WebSocket，并执行开关两侧。"""
import json
from types import SimpleNamespace
from urllib.parse import parse_qs, urlparse
import unittest
from unittest.mock import patch

from contracts.common import RequirementError
from contracts.routing_cases import preview_ws


class PreviewFixture:
    def __init__(self, fault=None):
        self.fault=fault
        self.base,self.key,self.model='http://fixture','fixture-only','route-test'
        self.user_id,self.token_id,self.channel_map=7,8,{'a':11}
        self.current_case='preview_websocket_eligibility'
        self.exchanges,self.statistics,self.preview_queries=[],{},[]
        self.enabled=True
        self.successful_creates=0
        self.user=SimpleNamespace(api=self.preview)
        self.sim=SimpleNamespace(cursor=lambda:0,records=lambda *a:[])
        self.connection=SimpleNamespace(request_id='handshake',send=lambda value:None,
            receive=lambda:{'type':'error'},close=lambda:None)

    def route(self,*a,**k): pass
    def balances(self): return {'wallet':100000}
    def settled(self,before,amount):
        assert amount==0
        return before
    def configure(self,name,setting):
        self.enabled=json.loads(setting)['responses_websocket_enabled']
    def preview(self,method,path):
        query=parse_qs(urlparse(path).query)
        self.preview_queries.append(query)
        assert query['transport']==['websocket'] and query['stream']==['true']
        eligible=self.enabled
        if self.fault=='disabled_eligible': eligible=True
        if self.fault=='enabled_ineligible': eligible=False
        return {'transport':'http' if self.fault=='wrong_transport' else 'websocket',
                'entries':[{'channel_id':11,'eligible':eligible}]}
    def websocket(self):
        assert self.enabled
        self.successful_creates+=1


class PreviewTransportTests(unittest.TestCase):
    def run_case(self,fixture):
        with patch('contracts.websocket.WebSocket.connect',return_value=fixture.connection):
            return preview_ws(fixture)

    def test_explicit_websocket_disabled_and_enabled_match_actual_execution(self):
        fixture=PreviewFixture()
        result=self.run_case(fixture)
        self.assertFalse(result['preview_eligible'])
        self.assertTrue(result['enabled_preview_eligible'])
        self.assertEqual(fixture.successful_creates,1)
        self.assertEqual(len(fixture.preview_queries),2)
        self.assertTrue(fixture.enabled)

    def test_mismatches_fail_and_always_restore_enabled_setting(self):
        for fault in ('disabled_eligible','enabled_ineligible','wrong_transport'):
            with self.subTest(fault=fault):
                fixture=PreviewFixture(fault)
                with self.assertRaises(RequirementError): self.run_case(fixture)
                self.assertTrue(fixture.enabled)
                self.assertEqual(fixture.successful_creates,0)

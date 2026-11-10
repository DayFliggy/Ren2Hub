import unittest

from routing_result import (KNOWN_UPSTREAM, REQUIRED_CASES, evaluate, origin_panic_frame,
                            websocket_request_identity, weight_tail_probability, has_private_error_field,
                            combined_exit_code)


class RoutingEvidenceTests(unittest.TestCase):
    def test_local_failure_outranks_known_only_across_layers(self):
        self.assertEqual(combined_exit_code([{'exit_code':2},{'exit_code':1}]),1)
        self.assertEqual(combined_exit_code([{'exit_code':2},{'success':True}]),2)
        self.assertEqual(combined_exit_code([{'success':True},{'success':True}]),0)
        self.assertEqual(combined_exit_code([{'exit_code':2},{}]),1)

    def test_only_complete_passes_exit_zero(self):
        self.assertEqual(evaluate([{'name': 'a', 'status': 'pass'}], ['a'])['exit_code'], 0)
        self.assertEqual(evaluate([], [])['exit_code'], 1)
        self.assertEqual(evaluate([{'name': 'a', 'status': 'pass'}], ['a', 'b'])['exit_code'], 1)

    def test_known_upstream_requires_diagnosis_and_blocks_are_linked(self):
        name = 'upstream_native_remix_probe'
        case = {'name': name, 'status': 'known_upstream', **KNOWN_UPSTREAM[name]}
        blocked = {'name': 'billing', 'status': 'blocked', 'blocked_by': name}
        report = evaluate([case, blocked], [name, 'billing'])
        self.assertEqual(report['exit_code'], 2)
        self.assertEqual(report['counts']['pass'], 0)
        self.assertFalse(report['success'])
        for key in ('diagnosis', 'upstream'):
            self.assertEqual(evaluate([{k: v for k, v in case.items() if k != key}], [name])['exit_code'], 1)
        blocked['blocked_by'] = 'unobserved'
        self.assertEqual(evaluate([case, blocked], [name, 'billing'])['exit_code'], 1)

    def test_local_case_cannot_borrow_an_upstream_diagnosis(self):
        case = {'name':'local', 'status':'known_upstream', **KNOWN_UPSTREAM['upstream_native_remix_probe']}
        self.assertEqual(evaluate([case], ['local'])['exit_code'], 1)

    def test_removed_execution_case_still_fails_inventory(self):
        cases = [{'name':name,'status':'pass'} for name in REQUIRED_CASES[:-1]]
        self.assertEqual(evaluate(cases, REQUIRED_CASES)['missing_cases'], [REQUIRED_CASES[-1]])
        self.assertEqual(evaluate(cases, REQUIRED_CASES)['exit_code'], 1)

    def test_local_failure_missing_case_and_cleanup_override_known(self):
        known = {'name': 'shared', 'status': 'known_upstream', 'upstream': '#4121', 'diagnosis': 'nil_ChannelMeta'}
        for status in ('fail', 'not_run', 'unexpected'):
            self.assertEqual(evaluate([known, {'name': 'local', 'status': status}], ['shared', 'local'])['exit_code'], 1)
        self.assertEqual(evaluate([known], ['shared'], ['remove_container'])['exit_code'], 1)
        self.assertEqual(evaluate([known, known], ['shared'])['exit_code'], 1)


class RequestEvidenceTests(unittest.TestCase):
    def test_private_error_fields_are_checked_recursively(self):
        self.assertTrue(has_private_error_field({'error':{'details':[{'channel_id':7}]}}))
        self.assertTrue(has_private_error_field({'error':{'API_KEY':'private'}}))
        self.assertFalse(has_private_error_field({'error':{'code':'invalid_request','message':'bad input'}}))

    def test_ws_uses_observed_event_id_and_rejects_ambiguous_logs(self):
        identifier = '202611051216364488372138268d9d6QGB8HgVG-ws-0'
        self.assertEqual(websocket_request_identity([{'request_id':identifier,'quota':1000}]), identifier)
        for rows in ([], [{'request_id':None}], [{'request_id':'handshake'}],
                     [{'request_id':identifier},{'request_id':'other-ws-0'}]):
            with self.subTest(rows=rows), self.assertRaises(ValueError):
                websocket_request_identity(rows)

    def test_weight_audit_rejects_significant_bias_without_exact_hit_rule(self):
        self.assertLess(weight_tail_probability(0,80,.25), .001)
        # Two-sided exact probability differs from a one-sided lower tail.
        self.assertGreater(weight_tail_probability(8,80,.25), .001)
        self.assertLess(weight_tail_probability(8,80,.25), .01)
        self.assertGreater(weight_tail_probability(19,80,.25), .001)
        self.assertGreater(weight_tail_probability(20,80,.25), .001)


class PanicIdentityTests(unittest.TestCase):
    def test_exact_statement_in_exact_function_is_required(self):
        source = 'func ResolveOriginTask(c int) {\n\tif originTask.ChannelId != info.ChannelId {\n}\n'
        self.assertIsNotNone(origin_panic_frame(b'/app/relay/relay_task.go:2 (0x123)', source))
        for trace in (b'/app/relay/relay_task.go:20 (0x123)', b'/other/relay_task.go:2', b'500 nil pointer'):
            self.assertIsNone(origin_panic_frame(trace, source))
        self.assertIsNone(origin_panic_frame(b'/app/relay/relay_task.go:2 ', source.replace('ResolveOriginTask','Other')))
        self.assertIsNone(origin_panic_frame(b'/app/relay/relay_task.go:2 ', source + source))

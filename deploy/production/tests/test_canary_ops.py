#!/usr/bin/env python3
"""Offline tests for the Ops Monitor Phase 1 restore-canary adapter."""
import importlib.util
from pathlib import Path
import tempfile
import types
import unittest

PATH = Path(__file__).resolve().parent.parent / 'prepare_canary.py'
spec = importlib.util.spec_from_file_location('production_canary', PATH)
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)

PCT = {'p50_ms': 1815, 'p90_ms': 4169, 'p95_ms': 5405, 'p99_ms': 9650, 'avg_ms': 2363, 'max_ms': 35094}
DUR = {'p50_ms': 8712, 'p90_ms': 31073, 'p95_ms': 104286, 'p99_ms': 231298, 'avg_ms': 15616, 'max_ms': 901021}


class Checks(unittest.TestCase):
    def worker(self, root, fault=None):
        worker = object.__new__(m.OpsHealthCanary)
        worker.root, worker.admin_key = root, 'restored-admin-key'
        calls = []

        def http(self, params, authenticated=True):
            calls.append(dict(params))
            if not authenticated: return (200 if fault == 'auth' else 401), {}
            data = {'health_score': 140 if fault == 'score' else 96, 'ttft': dict(PCT), 'duration': dict(DUR),
                    'gateway_overhead': {'window_minutes': 60, 'sample_count': 0}}
            if fault == 'overhead': data.pop('gateway_overhead')
            if params.get('platform') and fault != 'leak': data.pop('gateway_overhead')
            if params.get('mode') == 'preagg':
                if fault == 'not_populated': return 409, {'code': 'OPS_PREAGG_NOT_READY', 'message': 'Pre-aggregated ops metrics are not populated yet'}
                if fault == 'merged': data['ttft']['p99_ms'] = 24569
            return 200, {'code': 0, 'data': data}
        worker.ops_http = types.MethodType(http, worker)
        worker.calls = calls
        return worker


    def test_restored_copy_passes_and_compares_one_explicit_window(self):
        with tempfile.TemporaryDirectory() as raw:
            worker = self.worker(Path(raw))
            result = worker.ops_checks()
            self.assertTrue(result['passed'])
            self.assertEqual('identical', result['preagg_vs_raw_24h'])
            self.assertFalse(result['production_writes'])
            windows = [(c['start_time'], c['end_time']) for c in worker.calls if 'start_time' in c]
            self.assertEqual(2, len(windows))
            self.assertEqual(windows[0], windows[1])
            self.assertTrue((Path(raw) / 'ops-health-result.json').is_file())

    def test_unpopulated_preagg_is_recorded_not_failed(self):
        with tempfile.TemporaryDirectory() as raw:
            self.assertEqual('not_populated', self.worker(Path(raw), 'not_populated').ops_checks()['preagg_vs_raw_24h'])

    def test_regressions_fail_closed(self):
        for fault in ('auth', 'score', 'overhead', 'leak', 'merged'):
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as raw:
                with self.assertRaises(m.Failure): self.worker(Path(raw), fault).ops_checks()


if __name__ == '__main__': unittest.main()

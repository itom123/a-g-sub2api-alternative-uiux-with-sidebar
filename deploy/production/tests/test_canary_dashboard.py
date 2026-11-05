#!/usr/bin/env python3
"""Offline tests for exact native dashboard checks on the restored copy."""
import importlib.util
from pathlib import Path
import tempfile
import types
import unittest

PATH = Path(__file__).resolve().parent.parent / 'prepare_canary.py'
spec = importlib.util.spec_from_file_location('production_canary', PATH)
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


class Checks(unittest.TestCase):
    def worker(self, root, fault=None):
        worker = object.__new__(m.DashboardCanary)
        worker.root, worker.user, worker.insight_key = root, 700, 701
        worker.insight_before = '123|12'
        key = {'api_key_id':701,'key_name':'Synthetic','deleted':False,'requests':6003,'total_tokens':60021,
               'range_actual_cost':2,'lifetime_actual_cost':9,'today_actual_cost':2,'favourite_model':'dashboard-requested'}
        activity = {'as_of':'2026-11-01T20:00:00.123Z','coverage':'retained_logs','range_start':'2026-11-01T00:00:00Z','range_end_exclusive':'2026-11-01T20:00:00.123Z',
                    'keys':[key]+[{'api_key_id':1000+i,'requests':0} for i in range(1005)],
                    'active_windows':[{'id':name,'requests':6003,'tokens':60021,'rpm':6003/minutes,'tpm':60021/minutes} for name,minutes in [('60m',60),('12h',720),('24h',1440)]]}
        if fault == 'midnight':
            key['today_actual_cost'] = 0
            activity['as_of'] = '2026-11-02T00:01:00Z'
            activity['range_end_exclusive'] = activity['as_of']
        if fault == 'bad_cutoff': activity['as_of'] = 'invalid'
        if fault == 'sampled': key['requests'] = 5000
        if fault == 'lifetimerange': key['lifetime_actual_cost'] = 2
        if fault == 'keys': activity['keys'] = activity['keys'][:1000]
        if fault == 'windows': activity['active_windows'][0]['requests'] = 5000
        if fault == 'leak': key['account_cost'] = 1
        def http(self, path, authenticated=True):
            if not authenticated: return (200 if fault == 'auth' else 401), {}
            if 'user_id=1' in path and fault == 'override': return 200, {'data':{'key_activity':{'keys':[]}}}
            return 200, {'data':{'key_activity_status':'available','key_activity':activity}}
        worker.insight_http = types.MethodType(http, worker)
        def sql(self, query):
            if '::date' in query: return '2026-11-01|2026-11-02' if fault == 'midnight' else '2026-11-01|2026-11-01'
            if 'json_build_object' in query: return '{"requests":6003,"tokens":60021,"cost":2}'
            if 'COALESCE(sum(actual_cost),0)' in query:
                self.asserted_cutoff = query
                return '0' if fault == 'midnight' else '2'
            if 'balance::text' in query: return '0|0' if fault == 'mutation' else self.insight_before
            raise AssertionError('Unexpected SQL')
        worker.sql = types.MethodType(sql, worker)
        return worker


    def test_complete_fixture_passes_without_activation(self):
        with tempfile.TemporaryDirectory() as raw:
            result = self.worker(Path(raw)).dashboard_checks()
            self.assertTrue(result['passed'])
            self.assertEqual(1006,result['retained_keys'])
            self.assertFalse(result['production_writes'])

    def test_midnight_uses_fixture_dates_and_api_captured_day(self):
        with tempfile.TemporaryDirectory() as raw:
            worker = self.worker(Path(raw), 'midnight')
            self.assertTrue(worker.dashboard_checks()['passed'])
            self.assertIn("date_trunc('day','2026-11-02T00:01:00Z'::timestamptz", worker.asserted_cutoff)
            self.assertIn("created_at < '2026-11-02T00:01:00Z'::timestamptz", worker.asserted_cutoff)
            self.assertNotIn('now()', worker.asserted_cutoff)

    def test_miscounts_auth_leaks_and_mutations_fail_closed(self):
        for fault in ('sampled','lifetimerange','keys','windows','leak','auth','override','mutation','bad_cutoff'):
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as raw:
                with self.assertRaises(m.Failure): self.worker(Path(raw),fault).dashboard_checks()

if __name__ == '__main__': unittest.main()

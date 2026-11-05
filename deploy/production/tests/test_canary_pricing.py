"""Restored-copy managed OpenClaw pricing checks; no network or live writes."""
import importlib.util
from pathlib import Path
import tempfile
import types
import unittest

spec = importlib.util.spec_from_file_location('production_canary', Path(__file__).resolve().parent.parent / 'prepare_canary.py')
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


class PricingChecks(unittest.TestCase):
    def worker(self, root, fault=None):
        worker = object.__new__(m.OpenClawPricingCanary)
        worker.root = root
        worker.rate = 0.3
        worker.identity = {'name':'openai-龙虾','platform':'openai','status':'active','subscription_type':'standard','is_exclusive':False}
        worker.writes = []
        def http(self, group_id, payload=None, authenticated=True):
            if not authenticated:
                return (200 if fault == 'auth' else 401), {}
            if payload is not None:
                self.writes.append(payload)
                if 'rate_multiplier' not in payload:
                    return (200, {}) if fault == 'identity_unlocked' else (403, {'reason':'OPENCLAW_BILLING_LOCKED'})
                if fault != 'persist':
                    self.rate = payload['rate_multiplier']
            identity = dict(self.identity)
            if fault == 'identity_drift' and self.writes:
                identity['platform'] = 'anthropic'
            return 200, {'code':0,'data':dict(identity, rate_multiplier=self.rate)}
        def sql(self, query):
            if query.startswith('SELECT id FROM groups'):
                return '47'
            if query.startswith('SELECT rate_multiplier'):
                return '9' if fault == 'db_persist' else str(self.rate)
            raise AssertionError('unexpected SQL')
        worker.group_http = types.MethodType(http, worker)
        worker.sql = types.MethodType(sql, worker)
        return worker

    def test_admin_pricing_persists_and_all_identity_fields_remain_locked(self):
        with tempfile.TemporaryDirectory() as raw:
            worker = self.worker(Path(raw))
            result = worker.pricing_checks()
            self.assertTrue(result['passed'])
            self.assertFalse(result['production_writes'])
            self.assertEqual([{'rate_multiplier':0.427},{'rate_multiplier':1.25},{'name':'renamed-managed-group'},{'status':'inactive'},{'is_exclusive':True}], worker.writes)
            self.assertTrue((Path(raw) / 'openclaw-pricing-result.json').exists())

    def test_auth_persistence_and_identity_regressions_block_eligibility(self):
        for fault in ('auth','persist','db_persist','identity_unlocked','identity_drift'):
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as raw:
                worker = self.worker(Path(raw), fault)
                with self.assertRaises(m.Failure):
                    worker.pricing_checks()
                self.assertFalse((Path(raw) / 'openclaw-pricing-result.json').exists())


if __name__ == '__main__':
    unittest.main()

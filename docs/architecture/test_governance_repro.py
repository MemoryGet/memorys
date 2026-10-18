"""Diagnostic execution must never masquerade as complete repository evidence."""
import argparse
import unittest

from check_postgres_wsl import governance_rounds, validate_repository_proof


class GovernanceReproTests(unittest.TestCase):
    def test_round_limit(self):
        for value in ('0', '1', '50'):
            self.assertEqual(governance_rounds(value), int(value))
        for value in ('-1', '51', 'nan', '1;extra'):
            with self.assertRaises(argparse.ArgumentTypeError):
                governance_rounds(value)

    def test_diagnostic_is_never_full_acceptance(self):
        proof = {'passed': True, 'production_acceptance': False, 'diagnostic_only': True, 'checkpoint': 'review-and-first-renewal', 'publication_lease_samples': [{}, {}], 'publication_task_samples': [{}, {}]}
        validate_repository_proof(proof, diagnostic=True)
        with self.assertRaises(RuntimeError):
            validate_repository_proof(proof)
        proof['diagnostic_only'] = False
        with self.assertRaises(RuntimeError):
            validate_repository_proof(proof, diagnostic=True)

    def test_complete_requires_full_output(self):
        proof = {'passed': True, 'production_acceptance': False, 'checks': ['full checkpoint']}
        validate_repository_proof(proof)
        with self.assertRaises(RuntimeError):
            validate_repository_proof(proof, diagnostic=True)
        del proof['checks']
        with self.assertRaises(RuntimeError):
            validate_repository_proof(proof)

    def test_clock_anomaly_never_passes(self):
        proof = {'passed': True, 'production_acceptance': False, 'checks': ['full checkpoint'], 'fixture_clock_observation': {'unstable': True}}
        with self.assertRaises(RuntimeError):
            validate_repository_proof(proof)


if __name__ == '__main__':
    unittest.main()

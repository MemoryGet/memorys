import copy
import unittest

from check_assembly_semantics import check_binding, fixtures, validate


class VersionCompatibilityTests(unittest.TestCase):
    def test_newer_same_major_lifecycle_accepted_offline(self):
        for supplied in ["1.1", "1.10"]:
            config, providers = fixtures()
            providers[next(iter(providers))]["contract_version"] = supplied
            self.assertEqual(validate(config, providers), ["design-fixture"])

    def test_business_minor_requirement_and_major_rejection(self):
        binding={"instance_id":"parser", "capability":"parser", "contract":"1.1", "required_features":[]}
        for supplied in ["1.1","1.2","1.10","1."+"9"*40]:
            manifests={"parser":{"capabilities":[{"name":"parser","version":supplied,"features":[]}]}}
            self.assertEqual(check_binding(binding,manifests)["version"],supplied)
        for supplied in ["1.0","2.0"]:
            manifests={"parser":{"capabilities":[{"name":"parser","version":supplied,"features":[]}]}}
            with self.assertRaises(ValueError):check_binding(binding,manifests)

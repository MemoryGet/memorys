import copy
import unittest
from jsonschema import ValidationError
from check_assembly_semantics import fixtures, validate


class ReadDeploymentTests(unittest.TestCase):
    def test_explicit_role_and_features_are_required(self):
        config, providers = fixtures()
        self.assertEqual(validate(config, providers), ["design-fixture"])
        for mutate in (lambda c: c["bindings"].pop("query_read_unit"),
                       lambda c: c["bindings"]["query_read_unit"].update(required_features=[]),
                       lambda c: c["bindings"]["query_read_unit"].update(capability="metadata_uow")):
            c = copy.deepcopy(config); mutate(c)
            with self.assertRaises((ValueError, ValidationError)): validate(c, providers)

    def test_provider_must_declare_required_read_features(self):
        config, providers = fixtures()
        for feature in ("snapshot.fixed_view", "lifecycle.scoped_read"):
            p = copy.deepcopy(providers)
            capability = next(c for c in next(iter(p.values()))["capabilities"] if c["name"] == "query_read_unit")
            capability["features"].remove(feature)
            with self.assertRaises(ValueError): validate(config, p)

    def test_missing_capability_and_instance_rejected(self):
        config, providers = fixtures()
        p = copy.deepcopy(providers)
        manifest = next(iter(p.values()))
        manifest["capabilities"] = [c for c in manifest["capabilities"] if c["name"] != "query_read_unit"]
        with self.assertRaises(ValueError): validate(config, p)
        config["bindings"]["query_read_unit"]["instance_id"] = "missing"
        with self.assertRaises(ValueError): validate(config, providers)


if __name__ == "__main__": unittest.main()

import copy
import json
from pathlib import Path
import unittest
from jsonschema import Draft202012Validator

from check_manifest_design import validate_manifest

ROOT = Path(__file__).resolve().parent


class ExtensionManifestTests(unittest.TestCase):
    def test_every_missing_feature_is_rejected(self):
        policy = json.loads((ROOT.parents[1]/"pkg/kernel/extension-features.v1.json").read_text())
        schema = Draft202012Validator(json.loads((ROOT/"schemas/module-manifest.v1.schema.json").read_text()))
        base = json.loads((ROOT/"examples/parser-manifest.json").read_text())
        for name, features in policy["capabilities"].items():
            value = copy.deepcopy(base)
            value["capabilities"] = [{"name":name, "version":"1.0", "features":features}]
            validate_manifest(value,schema)
            for omitted in features:
                with self.subTest(capability=name, omitted=omitted):
                    bad = copy.deepcopy(value)
                    bad["capabilities"][0]["features"].remove(omitted)
                    with self.assertRaises(ValueError): validate_manifest(bad,schema)

    def test_absent_extensions_leave_base_manifest_valid(self):
        schema = Draft202012Validator(json.loads((ROOT/"schemas/module-manifest.v1.schema.json").read_text()))
        validate_manifest(json.loads((ROOT/"examples/parser-manifest.json").read_text()),schema)

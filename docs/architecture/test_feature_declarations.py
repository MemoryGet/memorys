import copy
import json
from pathlib import Path
import unittest
from jsonschema import Draft202012Validator, ValidationError

from check_manifest_design import validate_manifest

ROOT=Path(__file__).resolve().parent


class FeatureDeclarationTests(unittest.TestCase):
    def test_invalid_names_and_duplicate_features_rejected_offline(self):
        base=json.loads((ROOT/"examples/parser-manifest.json").read_text())
        validator=Draft202012Validator(json.loads((ROOT/"schemas/module-manifest.v1.schema.json").read_text()))
        for features in [[""],["contains space"],["a"*101],["future.optional","future.optional"],["\ud800"]]:
            value=copy.deepcopy(base);value["capabilities"][0]["features"]=features
            with self.assertRaises(ValidationError):validate_manifest(value,validator)

    def test_well_formed_unrequested_feature_remains_extensible(self):
        value=json.loads((ROOT/"examples/parser-manifest.json").read_text())
        value["capabilities"][0]["features"]=["future.optional"]
        validator=Draft202012Validator(json.loads((ROOT/"schemas/module-manifest.v1.schema.json").read_text()))
        validate_manifest(value,validator)

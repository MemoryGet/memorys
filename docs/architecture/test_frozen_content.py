"""Frozen byte identity and structural rejection, independent of Go implementation."""
import copy
import json
import unittest

from jsonschema.exceptions import ValidationError

from build_frozen_golden import samples
from canonical_content import canonical, frozen
from check_frozen_content import validate_payload


def data(value):
    return json.dumps(value, ensure_ascii=False).encode("utf-8")


class FrozenContentTests(unittest.TestCase):
    def test_manual_primitive_bytes_and_decimal_equivalence(self):
        self.assertEqual(canonical('{"b":1.2500,"a":"中文😀\\n"}'.encode()),
                         '{"a":"中文😀\\n","b":125e-2}'.encode())
        self.assertEqual(canonical(b'[10,1e1,-0,0.001200]'), b'[1e1,1e1,0,12e-4]')
        self.assertEqual(canonical(b'"\\ud83d\\ude00"'), '"😀"'.encode())

    def test_equivalent_key_order_and_numeric_spelling_keep_hashes(self):
        value = samples()[0][1]
        raw = data(value)
        alternative = json.dumps(value, ensure_ascii=True, sort_keys=True).replace("0.125", "1.2500e-1").encode()
        self.assertEqual(frozen(raw), frozen(alternative))

    def test_text_newline_and_empty_caption_changes_are_detected(self):
        items = dict(samples())
        self.assertNotEqual(frozen(data(items["line_feed"])), frozen(data(items["carriage_return_line_feed"])))
        self.assertNotEqual(frozen(data(items["merged_table_omitted_caption"])), frozen(data(items["merged_table_empty_caption"])))

    def test_attachment_hash_changes_content_but_not_blocks(self):
        value = copy.deepcopy(samples()[0][1])
        before = frozen(data(value))
        value["blobs"][0]["sha256"] = "d" * 64
        after = frozen(data(value))
        self.assertEqual(before["blocks_hash"], after["blocks_hash"])
        self.assertNotEqual(before["content_hash"], after["content_hash"])

    def test_unknown_null_unsorted_and_missing_blob_fail(self):
        source = samples()[0][1]
        bad = []
        value = copy.deepcopy(source); value["supplier_id"] = "private"; bad.append(value)
        value = copy.deepcopy(source); value["blocks"][0]["text"] = None; bad.append(value)
        value = copy.deepcopy(source); value["blocks"][0]["order"] = 1; bad.append(value)
        value = copy.deepcopy(source); value["blobs"] = []; bad.append(value)
        value = copy.deepcopy(source); value["blobs"].append(copy.deepcopy(value["blobs"][0])); bad.append(value)
        for value in bad:
            with self.assertRaises((ValueError, ValidationError)):
                validate_payload(value)

    def test_invalid_unicode_duplicate_numbers_and_depth_fail(self):
        for raw in [b'"\\ud800"', b'"\\udc00"', b'{"x":1,"x":2}', b'1e10001', b'NaN', b'"\xff"',
                    b'[' * 65 + b'0' + b']' * 65, b'1' + b'0' * 256]:
            with self.assertRaises(ValueError):
                canonical(raw)


if __name__ == "__main__":
    unittest.main()

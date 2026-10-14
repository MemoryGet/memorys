"""Python checks of exact saved vectors; Go tests consume the same independent file."""
import json
from pathlib import Path
import unittest
from access_grant_validation import binding_digest
from build_grant_golden import build


class GrantGoldenTests(unittest.TestCase):
    def test_saved_python_vectors_and_equivalent_key_order(self):
        saved=json.loads((Path(__file__).parent/'examples/grant-binding-golden.v1.json').read_text(encoding='utf-8'))
        self.assertEqual(saved,build())
        cases={c['name']:c for c in saved['cases']}
        self.assertEqual(cases['exact_decimal']['digest'],cases['key_order']['digest'])
        self.assertNotEqual(cases['unicode_lf']['digest'],cases['unicode_crlf']['digest'])

    def test_scalar_array_duplicate_unicode_and_extra_json_rejected(self):
        for value in (b'null',b'[]',b'"str"',b'{"x":1,"x":2}',b'{} {}',b'{"x":"\xff"}'):
            with self.subTest(value=value),self.assertRaises((ValueError,UnicodeError)):binding_digest(value,'request')


if __name__=='__main__':unittest.main()

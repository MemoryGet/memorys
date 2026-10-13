"""Remote wire schema acceptance; does not verify service identity or access grants."""
import copy
import unittest

from jsonschema import Draft202012Validator

from build_remote_contract import build, operations
from remote_wire_validation import validate, validator


def context():
    return {"request_id": "request-1", "invocation_id": "invoke-1", "module_instance_id": "parser-a",
            "contract_version": "1.0", "deadline": "2026-10-12T12:00:00Z", "profile_digest": "a" * 64,
            "scope": {"scope_id": "scope-1", "subject_id": "subject-1", "action": "document.parse", "space_id": "space-1",
                      "kb_ids": ["kb-1"], "policy_version": 1, "expires_at": "2026-10-12T12:00:00Z"}}


class RemoteContractTests(unittest.TestCase):
    def setUp(self):
        self.spec = build()

    def validator(self, name):
        return validator(self.spec, name)

    def test_operations_have_service_auth_grant_and_local_schemas(self):
        self.assertEqual(len(self.spec["paths"]), 17)
        for path, item in self.spec["paths"].items():
            self.assertTrue(path.startswith("/modules/v1/"))
            operation = item["post"]
            self.assertTrue(operation["requestBody"]["required"])
            self.assertEqual(operation["parameters"][0]["name"], "X-AKOS-Access-Grant")
            self.assertTrue(operation["parameters"][0]["required"])
        self.assertEqual(self.spec["security"], [{"ServiceAuth": []}])
        for schema in self.spec["components"]["schemas"].values():
            Draft202012Validator.check_schema(schema)

    def test_parser_fixture_and_context_field_rejections(self):
        request = {"context": context(), "input": {"draft_id": "draft-1", "input": {"id": "blob-1", "sha256": "b" * 64, "size": 100,
                    "media_type": "application/pdf"}, "format": "pdf", "options": {"languages": ["zh"], "enable_ocr": True}}}
        validator = self.validator("parser_parse_Request")
        validator.validate(request)
        for change in [lambda x: x.pop("context"), lambda x: x["context"].update(user_id="spoof"),
                       lambda x: x["context"].update(contract_version="2.0"), lambda x: x["context"].update(deadline="invalid"),
                       lambda x: x["input"].update(url="https://invalid.example/private")]:
            bad = copy.deepcopy(request); change(bad)
            self.assertFalse(validator.is_valid(bad))

    def test_composite_does_not_accept_external_vectors(self):
        self.assertIn("vectors", self.spec["components"]["schemas"]["IndexPrepareInput"]["properties"])
        self.assertNotIn("vectors", self.spec["components"]["schemas"]["CompositePrepareInput"]["properties"])
        self.assertFalse(self.spec["components"]["schemas"]["CompositePrepareInput"]["additionalProperties"])

    def test_candidate_retains_generation_ids_and_safe_errors_reject_causes(self):
        schemas = self.spec["components"]["schemas"]
        self.assertIn("generation_ids", schemas["Candidate"]["required"])
        error = {"request_id": "request-1", "invocation_id": "invoke-1", "error": {"code": "dependency_unavailable",
                 "safe_message": "Retry later", "retryable": True, "request_id": "request-1"}}
        validator = self.validator("RemoteError")
        validator.validate(error)
        error["error"]["cause"] = "private-token"
        self.assertFalse(validator.is_valid(error))

    def test_invocation_state_cannot_claim_success_without_result(self):
        validator = self.validator("InvocationStatus")
        validator.validate({"target_invocation_id": "invoke-1", "state": "running"})
        self.assertFalse(validator.is_valid({"target_invocation_id": "invoke-1", "state": "succeeded"}))
        success = {"target_invocation_id": "invoke-1", "state": "succeeded",
                   "result": {"operation": "index_writer_delete", "value": {"completed": True}}}
        validator.validate(success)
        success["state"] = "cancelled"
        self.assertFalse(validator.is_valid(success))

    def test_binary_and_sse_are_explicit_not_json_byte_arrays(self):
        blob = self.spec["paths"]["/modules/v1/blob_store/open"]["post"]["responses"]["200"]
        self.assertIn("application/octet-stream", blob["content"])
        stream = self.spec["paths"]["/modules/v1/generator/generate"]["post"]["responses"]["200"]
        self.assertIn("x-event-data-schema", stream["content"]["text/event-stream"])
        mutation_count = sum(item[4] for item in operations())
        self.assertEqual(mutation_count, 6)

    def test_calendar_and_nonfinite_numbers_are_not_only_annotations(self):
        self.assertFalse(self.validator("ModuleContext").is_valid(dict(context(), deadline="2026-02-30T12:00:00Z")))
        validate(self.spec, "Vector", [0.5])
        with self.assertRaises(ValueError):
            validate(self.spec, "Vector", [float("nan")])


if __name__ == "__main__":
    unittest.main()

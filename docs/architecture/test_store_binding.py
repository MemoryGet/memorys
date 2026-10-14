import copy
import json
import unittest
from store_binding_validation import load_bundle, validate_bundle


def fixture():
    identity = {"dataset_id": "dataset", "metadata_store_epoch_id": "store", "journal_stream_id": "stream",
                "binding_version": 18446744073709551615, "metadata_model_version": 9007199254740993, "schema_digest": "a"*64}
    endpoints = [{"role": role, "module_instance_id": role + "-instance", "endpoint_identity": role + "-endpoint"} for role in ("metadata_uow", "query_read_unit")]
    expected = {"binding_id": "binding", "identity": identity, "activation_cursor": "required", "endpoints": endpoints}
    observations = [{"observation_id": "obs-"+e["role"], "binding_id": "binding", "identity": copy.deepcopy(identity), "endpoint": copy.deepcopy(e), "current_cursor": "unknown"} for e in endpoints]
    return {"schema_version": 1, "expectation": expected, "observations": observations}


class StoreBindingTests(unittest.TestCase):
    def reject(self, mutate):
        b = fixture(); mutate(b)
        with self.assertRaisesRegex(ValueError, "invalid_store_binding_review"): validate_bundle(b)

    def test_precise_versions_and_no_readiness_claim(self):
        b = fixture(); self.assertEqual(load_bundle(json.dumps(b).encode()), b)
        report = validate_bundle(b)
        self.assertEqual(report["matched_observations"], 2)
        for key in ("provenance_verified", "coverage_verified", "ready_for_service"): self.assertFalse(report[key])
        b["observations"][0]["current_cursor"] = "lexically-z-still-unverified"
        self.assertFalse(validate_bundle(b)["coverage_verified"])

    def test_dataset_epoch_model_and_endpoint_mismatch(self):
        for key, value in (("dataset_id", "clone"), ("metadata_store_epoch_id", "old"), ("journal_stream_id", "other"), ("binding_version", 2), ("metadata_model_version", 1), ("schema_digest", "b"*64)):
            self.reject(lambda b, k=key, v=value: b["observations"][1]["identity"].update({k: v}))
        self.reject(lambda b: b["observations"][1]["endpoint"].update(endpoint_identity="other"))

    def test_inventory_missing_extra_duplicate_and_receipt_reuse(self):
        self.reject(lambda b: b["observations"].pop())
        self.reject(lambda b: b["observations"].append(copy.deepcopy(b["observations"][0])))
        self.reject(lambda b: b["observations"][1].update(endpoint=copy.deepcopy(b["observations"][0]["endpoint"])))
        self.reject(lambda b: b["observations"][1].update(observation_id=b["observations"][0]["observation_id"]))

    def test_closed_fields_and_integer_types(self):
        self.reject(lambda b: b.update(credential="not-a-real-secret"))
        self.reject(lambda b: b["expectation"].pop("activation_cursor"))
        self.reject(lambda b: b["expectation"]["identity"].update(binding_version=1.0))
        self.reject(lambda b: b["expectation"]["identity"].update(binding_version=True))
        self.reject(lambda b: b["expectation"]["identity"].update(binding_version=18446744073709551616))

    def test_raw_json_and_utf8_byte_limits(self):
        raw = json.dumps(fixture()).encode()
        for bad in (raw.replace(b'"schema_version": 1', b'"schema_version": 1, "schema_version": 1'), raw.replace(b'"schema_version": 1', b'"schema_version": 1.0'), b'{"x":"\\ud800"}', b'x'*((4<<20)+1)):
            with self.assertRaises(ValueError): load_bundle(bad)
        self.reject(lambda b: b["observations"][0].update(current_cursor="😀"*1025))


if __name__ == "__main__": unittest.main()

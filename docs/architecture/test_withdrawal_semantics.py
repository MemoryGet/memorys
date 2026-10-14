import hashlib
import json
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[2]


class WithdrawalSemanticVectors(unittest.TestCase):
    def test_independent_python_bytes_and_hashes(self):
        vectors = json.loads((ROOT / "pkg/withdrawal/testdata/semantic-vectors.json").read_text(encoding="utf-8"))
        for vector in vectors:
            request = vector["input"]
            command = request["Command"]
            principal = command["Principal"]
            delegation = principal.get("Delegation")
            actor = {"format": "akos.withdraw.actor/1", "subject_id": principal["SubjectID"],
                     "subject_type": principal["SubjectType"], "issuer": principal["Issuer"],
                     "client_id": principal["ClientID"],
                     "delegation": {"user_id": delegation["UserID"], "client_id": delegation["ClientID"]} if delegation else None}
            actor_bytes = json.dumps(actor, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
            actor_hash = hashlib.sha256(("akos.withdraw.actor/1\n" + actor_bytes).encode("utf-8")).hexdigest()
            body = {"format": "akos.withdraw.command/1", "action": "publication.withdraw",
                    "actor_digest": actor_hash, "space_id": command["Scope"]["SpaceID"],
                    "knowledge_base_id": command["Scope"]["KnowledgeBaseIDs"][0],
                    "document_id": request["DocumentID"], "expected_epoch": str(request["ExpectedEpoch"]),
                    "reason": request["Reason"], "profile_digest": command["Request"]["ProfileDigest"]}
            body_bytes = json.dumps(body, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
            body_hash = hashlib.sha256(("akos.withdraw.command/1\n" + body_bytes).encode("utf-8")).hexdigest()
            self.assertEqual((actor_bytes, actor_hash, body_bytes, body_hash),
                             (vector["actor_bytes"], vector["actor_digest"], vector["command_bytes"], vector["command_digest"]))


if __name__ == "__main__":
    unittest.main()

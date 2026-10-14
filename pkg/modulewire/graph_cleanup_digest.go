package modulewire

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

type graphCleanupSemantic struct {
	SchemaVersion string
	Value         contracts.GraphCleanupSemantic
}

func init() {
	register(contracts.GraphProjectionTarget{}, "SpaceID:space_id KnowledgeBaseID:kb_id BackendEpochID:backend_epoch_id GenerationID:generation_id IndexProfileDigest:index_profile_digest ExtractionProfileDigest:extraction_profile_digest OntologyDigest:ontology_digest SourceSnapshotDigest:source_snapshot_digest")
	register(contracts.GraphCleanupSemantic{}, "OperationID:operation_id SubjectID:subject_id Kind:kind Target:target DocumentID:document_id? RevisionID:revision_id? PublicationEpoch:publication_epoch? RetentionPermitID:retention_permit_id?")
	register(graphCleanupSemantic{}, "SchemaVersion:schema_version Value:operation")
}

func validGraphCleanupSemantic(value contracts.GraphCleanupSemantic) bool {
	if !memoryID(value.OperationID) || !memoryID(value.SubjectID) || contracts.ValidateGraphProjectionTarget(value.Target) != nil {
		return false
	}
	for _, id := range []contracts.ID{value.Target.SpaceID, value.Target.KnowledgeBaseID, value.Target.BackendEpochID, value.Target.GenerationID} {
		if !memoryID(id) {
			return false
		}
	}
	switch value.Kind {
	case "source_invalidation":
		return memoryID(value.DocumentID) && memoryID(value.RevisionID) && value.PublicationEpoch > 0 && value.RetentionPermitID == ""
	case "generation_deletion":
		return memoryID(value.RetentionPermitID) && value.DocumentID == "" && value.RevisionID == "" && value.PublicationEpoch == 0
	}
	return false
}

func EncodeGraphCleanupSemantic(value contracts.GraphCleanupSemantic) ([]byte, error) {
	if !validGraphCleanupSemantic(value) {
		return nil, invalid()
	}
	return encode(graphCleanupSemantic{SchemaVersion: "1.0", Value: value})
}

func DecodeGraphCleanupSemantic(raw []byte) (contracts.GraphCleanupSemantic, error) {
	var body graphCleanupSemantic
	if decode(raw, &body) != nil || body.SchemaVersion != "1.0" || !strictStoreIntegers(raw) || !validGraphCleanupSemantic(body.Value) {
		return contracts.GraphCleanupSemantic{}, invalid()
	}
	var root, operation map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil || json.Unmarshal(root["operation"], &operation) != nil {
		return contracts.GraphCleanupSemantic{}, invalid()
	}
	forbidden := []string{"retention_permit_id"}
	if body.Value.Kind == "generation_deletion" {
		forbidden = []string{"document_id", "revision_id", "publication_epoch"}
	}
	for _, key := range forbidden {
		if _, present := operation[key]; present {
			return contracts.GraphCleanupSemantic{}, invalid()
		}
	}
	return body.Value, nil
}

// Hashing valid declared semantics does not authenticate a caller/permit,
// persist the operation, or prove any physical deletion occurred.
func GraphCleanupRequestDigest(value contracts.GraphCleanupSemantic) (contracts.Digest, error) {
	raw, err := EncodeGraphCleanupSemantic(value)
	if err != nil {
		return "", err
	}
	canonicalBody, err := canonical.JSON(raw)
	if err != nil {
		return "", invalid()
	}
	tag := "akos.graph-cleanup/source-invalidation/1\x00"
	if value.Kind == "generation_deletion" {
		tag = "akos.graph-cleanup/generation-deletion/1\x00"
	}
	h := sha256.New()
	_, _ = h.Write([]byte(tag))
	_, _ = h.Write(canonicalBody)
	return contracts.Digest(hex.EncodeToString(h.Sum(nil))), nil
}

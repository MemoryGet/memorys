package modulewire_test

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/modulewire"
)

func cleanupSemantic() contracts.GraphCleanupSemantic {
	d := contracts.Digest(strings.Repeat("a", 64))
	return contracts.GraphCleanupSemantic{OperationID: "operation", SubjectID: "subject", Kind: "source_invalidation", DocumentID: "document", RevisionID: "revision", PublicationEpoch: math.MaxUint64, Target: contracts.GraphProjectionTarget{SpaceID: "space", KnowledgeBaseID: "kb", BackendEpochID: "epoch", GenerationID: "generation", IndexProfileDigest: d, ExtractionProfileDigest: d, OntologyDigest: d, SourceSnapshotDigest: d}}
}

func TestGraphCleanupSemanticRoundtripAndDigestChanges(t *testing.T) {
	s := cleanupSemantic()
	raw, err := modulewire.EncodeGraphCleanupSemantic(s)
	if err != nil {
		t.Fatal(err)
	}
	out, err := modulewire.DecodeGraphCleanupSemantic(raw)
	if err != nil || !reflect.DeepEqual(out, s) {
		t.Fatal(out, err)
	}
	digest, err := modulewire.GraphCleanupRequestDigest(s)
	if err != nil || len(digest) != 64 {
		t.Fatal(digest, err)
	}
	for _, change := range []func(*contracts.GraphCleanupSemantic){
		func(s *contracts.GraphCleanupSemantic) { s.OperationID = "other" },
		func(s *contracts.GraphCleanupSemantic) { s.SubjectID = "other" },
		func(s *contracts.GraphCleanupSemantic) { s.DocumentID = "other" },
		func(s *contracts.GraphCleanupSemantic) { s.RevisionID = "other" },
		func(s *contracts.GraphCleanupSemantic) { s.PublicationEpoch-- },
		func(s *contracts.GraphCleanupSemantic) { s.Target.GenerationID = "other" },
		func(s *contracts.GraphCleanupSemantic) {
			s.Target.SourceSnapshotDigest = contracts.Digest(strings.Repeat("b", 64))
		},
	} {
		changed := s
		change(&changed)
		got, err := modulewire.GraphCleanupRequestDigest(changed)
		if err != nil || got == digest {
			t.Fatal("semantic change not bound", got, err)
		}
	}
}

func TestGraphCleanupOperationVariantsCannotMix(t *testing.T) {
	s := cleanupSemantic()
	s.Kind = "generation_deletion"
	s.RetentionPermitID = "permit"
	if _, err := modulewire.GraphCleanupRequestDigest(s); err == nil {
		t.Fatal("mixed operation accepted")
	}
	s.DocumentID = ""
	s.RevisionID = ""
	s.PublicationEpoch = 0
	before, err := modulewire.GraphCleanupRequestDigest(s)
	if err != nil {
		t.Fatal(err)
	}
	s.RetentionPermitID = "other-permit"
	after, err := modulewire.GraphCleanupRequestDigest(s)
	if err != nil || before == after {
		t.Fatal("permit not bound")
	}
}

func TestGraphCleanupSemanticRejectsRawAmbiguity(t *testing.T) {
	raw, err := modulewire.EncodeGraphCleanupSemantic(cleanupSemantic())
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		strings.Replace(string(raw), "18446744073709551615", "1.0", 1),
		strings.Replace(string(raw), `"subject_id":"subject"`, `"subject_id":"subject","subject_id":"other"`, 1),
		strings.Replace(string(raw), `"subject_id":"subject"`, `"subject_id":"subject","scope_id":"transient"`, 1),
		strings.Replace(string(raw), `"schema_version":"1.0"`, `"schema_version":"2.0"`, 1),
	} {
		if _, err := modulewire.DecodeGraphCleanupSemantic([]byte(bad)); err == nil {
			t.Fatal("ambiguous body accepted")
		}
	}
}

// Package preparation coordinates approved text projection preparation. Its
// ports expose platform values only, independent of any database or engine.
package preparation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/indexsnapshot"
)

var ErrPreparation = errors.New("publication index preparation rejected")
var ErrPreparationUnknown = errors.New("preparation commit requires same-attempt reconciliation")

type Request struct {
	Access  document.PublicationReadWorkRequest
	Profile contracts.IndexProfile
	Chunks  []contracts.Chunk
}
type Artifact struct {
	OperationID, JobID, AttemptID contracts.ID
	FencingToken                  uint64
	Profile                       contracts.IndexProfile
	GenerationSet                 contracts.GenerationSet
	SnapshotDigest                contracts.Digest
	Digest                        contracts.Digest
}
type Backend interface {
	PrepareOwned(context.Context, Request) (Artifact, error)
	InspectOwned(context.Context, document.PublicationReadWorkRequest, Artifact) (contracts.IndexSnapshot, error)
}
type Worker struct {
	Tasks              document.PublicationTaskStore
	Splitter           contracts.Splitter
	ResolveProfile     func(context.Context, document.PublicationBackendSnapshot) (contracts.IndexProfile, error)
	Backend            Backend
	SplitterInstanceID contracts.ID
}

func own[T any](value T, limit int) (T, error) {
	var result T
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > limit {
		return result, ErrPreparation
	}
	if json.Unmarshal(raw, &result) != nil {
		return result, ErrPreparation
	}
	return result, nil
}

func id(v contracts.ID) bool {
	return v != "" && utf8.ValidString(string(v)) && utf8.RuneCountInString(string(v)) <= 200
}
func hash(raw []byte) contracts.Digest {
	sum := sha256.Sum256(raw)
	return contracts.Digest(hex.EncodeToString(sum[:]))
}
func validStrings(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.String:
		return utf8.ValidString(v.String())
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			return validStrings(v.Elem())
		}
	case reflect.Map:
		for _, key := range v.MapKeys() {
			if !validStrings(key) || !validStrings(v.MapIndex(key)) {
				return false
			}
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if !validStrings(v.Field(i)) {
				return false
			}
		}
	case reflect.Array, reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			if !validStrings(v.Index(i)) {
				return false
			}
		}
	}
	return true
}

// ProfileDigest commits the complete platform-native IndexProfile except its
// own Digest field. This format is versioned; it is not a supplier fingerprint.
func ProfileDigest(profile contracts.IndexProfile) (contracts.Digest, error) {
	if !id(profile.ID) || profile.Version == 0 || !validStrings(reflect.ValueOf(profile)) {
		return "", ErrPreparation
	}
	raw, err := json.Marshal(profile)
	if err != nil || len(raw) > 1<<20 {
		return "", ErrPreparation
	}
	var body map[string]json.RawMessage
	if json.Unmarshal(raw, &body) != nil {
		return "", ErrPreparation
	}
	delete(body, "Digest")
	raw, err = json.Marshal(map[string]any{"format": "akos.index-profile/1", "profile": body})
	if err != nil {
		return "", ErrPreparation
	}
	encoded, err := canonical.JSON(raw)
	if err != nil {
		return "", ErrPreparation
	}
	return hash(append([]byte("akos.index-profile/1\x00"), encoded...)), nil
}

func TargetIDs(operation, attempt contracts.ID) (contracts.ID, contracts.ID) {
	identity, _ := json.Marshal([]contracts.ID{operation, attempt})
	return contracts.ID(hash(append([]byte("akos.prepared-set/1\x00"), identity...))), contracts.ID(hash(append([]byte("akos.prepared-keyword/1\x00"), identity...)))
}

// Build checks source coverage and constructs a reference projection. Its
// snapshot is an expectation, never evidence that a backend actually wrote it.
func Build(work document.PublicationWork, profile contracts.IndexProfile, chunks []contracts.Chunk) (Artifact, contracts.IndexSnapshot, error) {
	var empty Artifact
	var noSnapshot contracts.IndexSnapshot
	digest, err := ProfileDigest(profile)
	b := work.Plan.Backend
	if err != nil || profile.Digest != digest || digest != b.ProfileDigest || profile.ID != b.IndexProfile.ID || profile.Version != b.IndexProfile.Version || work.Job.ProfileDigest != digest || work.Job.State != "running" || work.Job.ID != work.Lease.JobID || work.Job.OperationID != work.Plan.Result.Operation.ID || work.Job.FencingToken != work.Lease.FencingToken || !id(work.Lease.AttemptID) || work.Lease.FencingToken == 0 || work.Revision.ID != work.Plan.Review.Revision.ID || work.Revision.ContentHash != work.Plan.Review.Revision.ContentHash || len(chunks) == 0 || len(chunks) > 1000 {
		return empty, noSnapshot, ErrPreparation
	}
	if profile.Mode != "independent" || len(b.RequiredChannels) != 1 || b.RequiredChannels[0] != contracts.KeywordChannel || len(profile.Policy.RequiredChannels) != 1 || profile.Policy.RequiredChannels[0] != contracts.KeywordChannel {
		return empty, noSnapshot, contracts.PublicError{Code: "capability_missing", SafeMessage: "Native preparation supports the keyword channel only"}
	}
	writer, ok := profile.Bindings[contracts.IndexWriting]
	if !ok || !id(writer.InstanceID) || writer.Capability != contracts.IndexWriting || writer.ContractVersion != "1.0" {
		return empty, noSnapshot, ErrPreparation
	}
	splitter, ok := profile.Bindings[contracts.Splitting]
	if !ok || !id(splitter.InstanceID) || splitter.Capability != contracts.Splitting || splitter.ContractVersion != "1.0" || profile.Policy.Chunking.Strategy != "characters" || profile.Policy.Chunking.MaxCodepoints < 1 || profile.Policy.Chunking.OverlapCodepoints < 0 || profile.Policy.Chunking.OverlapCodepoints >= profile.Policy.Chunking.MaxCodepoints {
		return empty, noSnapshot, ErrPreparation
	}
	if profile.Policy.SchemaVersion != 1 || profile.Policy.ProjectionFormatVersion != "akos.chunks/1" || profile.Policy.Chunking.MaxCodepoints > 1000000 || profile.Policy.Chunking.ParentMaxCodepoints != nil || len(profile.Routes) != 1 || !id(profile.Routes[0].ID) || len(profile.Routes[0].Channels) != 1 || profile.Routes[0].Channels[0] != contracts.KeywordChannel || !reflect.DeepEqual(profile.Routes[0].Binding, writer) {
		return empty, noSnapshot, ErrPreparation
	}
	if !id(work.Job.ID) || !id(work.Job.OperationID) || !id(work.Plan.SpaceID) || !id(work.Plan.KnowledgeBaseID) || !id(work.Lease.WorkerID) {
		return empty, noSnapshot, ErrPreparation
	}
	blocks := map[contracts.ID]contracts.Block{}
	texts := map[contracts.ID][]rune{}
	coverage := map[contracts.ID][]contracts.CharacterRange{}
	for _, block := range work.Revision.Blocks {
		if block.Content.Kind != contracts.TextBlock || block.RevisionID != work.Revision.ID || !id(block.ID) || blocks[block.ID].ID != "" || !utf8.ValidString(block.Content.Text) {
			return empty, noSnapshot, ErrPreparation
		}
		blocks[block.ID] = block
		texts[block.ID] = []rune(block.Content.Text)
	}
	setID, generationID := TargetIDs(work.Job.OperationID, work.Lease.AttemptID)
	snapshot := contracts.IndexSnapshot{Format: indexsnapshot.Format, SpaceID: work.Plan.SpaceID, KnowledgeBaseID: work.Plan.KnowledgeBaseID, Generations: []contracts.IndexDeletionState{{SpaceID: work.Plan.SpaceID, KnowledgeBaseID: work.Plan.KnowledgeBaseID, DocumentID: work.Revision.DocumentID, RevisionID: work.Revision.ID, GenerationID: generationID, BackendEpochID: b.EpochID, RecordCount: len(chunks)}}}
	seen := map[contracts.ID]bool{}
	chunkBytes := 0
	for _, chunk := range chunks {
		if !id(chunk.ID) || seen[chunk.ID] || chunk.RevisionID != work.Revision.ID || chunk.ProfileDigest != digest || !utf8.ValidString(chunk.Text) || chunk.ContentHash != hash([]byte(chunk.Text)) || len(chunk.Spans) != 1 || chunk.Spans[0].Characters == nil {
			return empty, noSnapshot, ErrPreparation
		}
		seen[chunk.ID] = true
		chunkBytes += len(chunk.Text)
		if chunkBytes > 4<<20 {
			return empty, noSnapshot, ErrPreparation
		}
		span := chunk.Spans[0]
		block, ok := blocks[span.BlockID]
		if !ok {
			return empty, noSnapshot, ErrPreparation
		}
		r := *span.Characters
		runes := texts[block.ID]
		if r.Start < 0 || r.End <= r.Start || r.End > len(runes) || r.End-r.Start > profile.Policy.Chunking.MaxCodepoints || string(runes[r.Start:r.End]) != chunk.Text {
			return empty, noSnapshot, ErrPreparation
		}
		coverage[block.ID] = append(coverage[block.ID], r)
		snapshot.Records = append(snapshot.Records, contracts.IndexProjection{SpaceID: work.Plan.SpaceID, KnowledgeBaseID: work.Plan.KnowledgeBaseID, DocumentID: work.Revision.DocumentID, GenerationID: generationID, BackendEpochID: b.EpochID, Published: false, Chunk: chunk, Citation: contracts.Citation{DocumentID: work.Revision.DocumentID, RevisionID: work.Revision.ID, BlockID: block.ID, Source: block.Content.Source}})
	}
	for id, block := range blocks {
		ranges := coverage[id]
		sort.Slice(ranges, func(i, j int) bool {
			return ranges[i].Start < ranges[j].Start || ranges[i].Start == ranges[j].Start && ranges[i].End < ranges[j].End
		})
		end := 0
		for _, r := range ranges {
			if r.Start > end {
				return empty, noSnapshot, ErrPreparation
			}
			if r.End > end {
				end = r.End
			}
		}
		if end != utf8.RuneCountInString(block.Content.Text) {
			return empty, noSnapshot, ErrPreparation
		}
	}
	snapshot, err = indexsnapshot.Seal(snapshot)
	if err != nil {
		return empty, noSnapshot, ErrPreparation
	}
	proof := contracts.ReadinessProof{GenerationSetID: setID, AttemptID: work.Lease.AttemptID, ContentDigest: work.Revision.ContentHash, ProfileDigest: digest, Generations: []contracts.GenerationReadiness{{GenerationID: generationID, Channel: contracts.KeywordChannel, BackendEpochID: b.EpochID, ChunkCount: len(chunks)}}}
	generation := contracts.IndexGeneration{ID: generationID, KnowledgeBaseID: work.Plan.KnowledgeBaseID, RevisionID: work.Revision.ID, Channel: contracts.KeywordChannel, ProfileDigest: digest, BackendEpochID: b.EpochID, State: "ready", Readiness: &proof, BackendBinding: &contracts.BackendBinding{PlatformResourceType: "index_generation", PlatformResourceID: generationID, ModuleInstanceID: writer.InstanceID, BackendID: "prepared/" + string(generationID), GenerationID: &generationID, Purpose: "publication.prepare"}}
	set := contracts.GenerationSet{ID: setID, RevisionID: work.Revision.ID, BackendEpochID: b.EpochID, State: "ready", Channels: map[contracts.Channel]contracts.IndexGeneration{contracts.KeywordChannel: generation}}
	if contracts.ValidateReadyGenerationSet(set, contracts.GenerationExpectation{KnowledgeBaseID: work.Plan.KnowledgeBaseID, RevisionID: work.Revision.ID, BackendEpochID: b.EpochID, ProfileDigest: digest, ContentDigest: work.Revision.ContentHash, RequiredChannels: b.RequiredChannels}) != nil {
		return empty, noSnapshot, ErrPreparation
	}
	artifact := Artifact{OperationID: work.Job.OperationID, JobID: work.Job.ID, AttemptID: work.Lease.AttemptID, FencingToken: work.Lease.FencingToken, Profile: profile, GenerationSet: set, SnapshotDigest: snapshot.Checksum}
	artifact.Digest, err = ArtifactDigest(artifact)
	if err != nil {
		return empty, noSnapshot, ErrPreparation
	}
	// Detach nested Profile/Readiness pointers from caller-owned inputs.
	raw, _ := json.Marshal(artifact)
	var owned Artifact
	if json.Unmarshal(raw, &owned) != nil {
		return empty, noSnapshot, ErrPreparation
	}
	return owned, snapshot, nil
}

func ArtifactDigest(artifact Artifact) (contracts.Digest, error) {
	raw, err := artifactDigestInput(artifact)
	if err != nil {
		return "", err
	}
	return artifactDigestBytes(raw)
}

// artifactDigestInput keeps validation and encoding identical for ordinary
// digesting and the invocation-local memo. Digest remains excluded from the
// committed body, but validStrings still checks it as before.
func artifactDigestInput(artifact Artifact) ([]byte, error) {
	if !validStrings(reflect.ValueOf(artifact)) {
		return nil, ErrPreparation
	}
	// Encode the same fields once instead of encoding, decoding into RawMessage
	// entries, deleting Digest and encoding again for every published document.
	// Canonicalization still defines the exact existing digest byte format.
	body := artifactDigestBody{artifact.OperationID, artifact.JobID, artifact.AttemptID,
		artifact.FencingToken, artifact.Profile, artifact.GenerationSet, artifact.SnapshotDigest}
	raw, err := json.Marshal(struct {
		Format   string             `json:"format"`
		Artifact artifactDigestBody `json:"artifact"`
	}{"akos.prepared-artifact/1", body})
	if err != nil {
		return nil, ErrPreparation
	}
	return raw, nil
}

func artifactDigestBytes(raw []byte) (contracts.Digest, error) {
	encoded, err := canonical.JSON(raw)
	if err != nil {
		return "", ErrPreparation
	}
	return hash(append([]byte("akos.prepared-artifact/1\x00"), encoded...)), nil
}

type artifactDigestBody struct {
	OperationID, JobID, AttemptID contracts.ID
	FencingToken                  uint64
	Profile                       contracts.IndexProfile
	GenerationSet                 contracts.GenerationSet
	SnapshotDigest                contracts.Digest
}

// Adapter-private backend handles may differ. All platform facts and the
// complete returned artifact digest must still match authoritative expectations.
func MatchArtifact(actual, expected Artifact) error {
	digest, err := ArtifactDigest(actual)
	if err != nil || actual.Digest != digest {
		return ErrPreparation
	}
	raw, _ := json.Marshal(actual)
	var normalized Artifact
	if json.Unmarshal(raw, &normalized) != nil {
		return ErrPreparation
	}
	if len(normalized.GenerationSet.Channels) != len(expected.GenerationSet.Channels) {
		return ErrPreparation
	}
	for channel, want := range expected.GenerationSet.Channels {
		got, ok := normalized.GenerationSet.Channels[channel]
		if !ok || got.BackendBinding == nil || got.BackendBinding.BackendID == "" || want.BackendBinding == nil {
			return ErrPreparation
		}
		got.BackendBinding.BackendID = want.BackendBinding.BackendID
		normalized.GenerationSet.Channels[channel] = got
	}
	normalized.Digest = expected.Digest
	if !reflect.DeepEqual(normalized, expected) {
		return ErrPreparation
	}
	return nil
}

func (w Worker) Prepare(ctx context.Context, access document.PublicationReadWorkRequest) (Artifact, error) {
	if ctx == nil || w.Tasks == nil || w.Splitter == nil || w.ResolveProfile == nil || w.Backend == nil {
		return Artifact{}, ErrPreparation
	}
	raw, err := json.Marshal(access)
	if err != nil {
		return Artifact{}, ErrPreparation
	}
	var owned document.PublicationReadWorkRequest
	if json.Unmarshal(raw, &owned) != nil {
		return Artifact{}, ErrPreparation
	}
	access = owned
	ctx, cancel := context.WithDeadline(ctx, access.Lease.ExpiresAt)
	defer cancel()
	work, err := w.Tasks.ReadOwned(ctx, access)
	if err != nil {
		return Artifact{}, err
	}
	work, err = own(work, 12<<20)
	if err != nil {
		return Artifact{}, ErrPreparation
	}
	draft := contracts.Draft{ID: "worker-content-check", DocumentID: work.Revision.DocumentID, State: "draft", RowVersion: 1, Blobs: work.Revision.Blobs}
	for _, block := range work.Revision.Blocks {
		if block.RevisionID != work.Revision.ID {
			return Artifact{}, ErrPreparation
		}
		draft.Blocks = append(draft.Blocks, contracts.DraftBlock{ID: block.ID, DraftID: draft.ID, RowVersion: 1, Order: block.Order, Content: block.Content})
	}
	frozen, err := document.BuildFrozenContent(draft)
	if err != nil || frozen.ContentHash != work.Revision.ContentHash || frozen.BlocksHash != work.Revision.BlocksHash || !bytes.Equal(frozen.Payload, work.FrozenPayload) {
		return Artifact{}, ErrPreparation
	}
	backendCopy, err := own(work.Plan.Backend, 1<<20)
	if err != nil {
		return Artifact{}, ErrPreparation
	}
	profile, err := w.ResolveProfile(ctx, backendCopy)
	if err != nil {
		return Artifact{}, ErrPreparation
	}
	profile, err = own(profile, 1<<20)
	if err != nil {
		return Artifact{}, ErrPreparation
	}
	digest, err := ProfileDigest(profile)
	if err != nil || digest != work.Plan.Backend.ProfileDigest || profile.Digest != digest {
		return Artifact{}, ErrPreparation
	}
	if w.SplitterInstanceID == "" || w.SplitterInstanceID != profile.Bindings[contracts.Splitting].InstanceID {
		return Artifact{}, ErrPreparation
	}
	splitRequest, err := own(contracts.SplitRequest{Request: access.Request, RevisionID: work.Revision.ID, Blocks: work.Revision.Blocks, Policy: profile.Policy.Chunking}, 8<<20)
	if err != nil {
		return Artifact{}, ErrPreparation
	}
	chunks, err := w.Splitter.Split(ctx, splitRequest)
	if err != nil {
		return Artifact{}, ErrPreparation
	}
	chunks, err = own(chunks, 8<<20)
	if err != nil {
		return Artifact{}, ErrPreparation
	}
	expected, reference, err := Build(work, profile, chunks)
	if err != nil {
		return Artifact{}, err
	}
	request, err := own(Request{Access: access, Profile: profile, Chunks: chunks}, 8<<20)
	if err != nil {
		return Artifact{}, ErrPreparation
	}
	artifact, err := w.Backend.PrepareOwned(ctx, request)
	if err != nil {
		return Artifact{}, err
	}
	artifact, err = own(artifact, 2<<20)
	if err != nil {
		return Artifact{}, ErrPreparation
	}
	if MatchArtifact(artifact, expected) != nil {
		return Artifact{}, ErrPreparation
	}
	inspectAccess, _ := own(access, 65536)
	inspectArtifact, _ := own(artifact, 2<<20)
	actual, err := w.Backend.InspectOwned(ctx, inspectAccess, inspectArtifact)
	if err != nil {
		return Artifact{}, err
	}
	verified, err := indexsnapshot.Verify(actual)
	if err != nil || verified.Checksum != reference.Checksum || len(verified.Records) != len(reference.Records) {
		return Artifact{}, ErrPreparation
	}
	if _, err := w.Tasks.ReadOwned(ctx, access); err != nil {
		return Artifact{}, err
	}
	if ctx.Err() != nil {
		return Artifact{}, ErrPreparation
	}
	return artifact, nil
}

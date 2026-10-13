package modulewire_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/modulewire"
)

func TestVerifyAndBoundedDeleteMappings(t *testing.T) {
	request := indexRequest()
	call := modulewire.Call{InvocationID: "i", ModuleInstanceID: "index-a"}
	proof := contracts.ReadinessProof{GenerationSetID: "set", AttemptID: "attempt", ContentDigest: contracts.Digest(strings.Repeat("c", 64)), ProfileDigest: request.Generations[0].ProfileDigest,
		Generations: []contracts.GenerationReadiness{{GenerationID: "gen", Channel: contracts.KeywordChannel, BackendEpochID: "epoch", ChunkCount: 1}}}
	data, err := modulewire.EncodeIndexVerifyRequest(call, request.Request, request.Scope, proof)
	if err != nil {
		t.Fatal(err)
	}
	_, context, scope, actual, err := modulewire.DecodeIndexVerifyRequest(data)
	if err != nil || !reflect.DeepEqual(context, request.Request) || !reflect.DeepEqual(scope, request.Scope) || !reflect.DeepEqual(actual, proof) {
		t.Fatal("verify fields lost", err)
	}
	for _, chunks := range [][]contracts.ID{{}, {"chunk"}} {
		deletion := contracts.IndexDeleteRequest{Request: request.Request, Scope: request.Scope, GenerationID: "gen", ChunkIDs: chunks}
		data, err = modulewire.EncodeIndexDeleteRequest(call, deletion)
		if err != nil {
			t.Fatal(err)
		}
		_, actual, err := modulewire.DecodeIndexDeleteRequest(data)
		if err != nil || !reflect.DeepEqual(actual, deletion) {
			t.Fatal("delete target changed", err)
		}
	}
	bad := strings.Replace(string(data), `"mode":"chunks"`, `"mode":"generation"`, 1)
	if _, _, err := modulewire.DecodeIndexDeleteRequest([]byte(bad)); err == nil {
		t.Fatal("ambiguous deletion accepted")
	}
	data, err = modulewire.EncodeIndexAck("req-1", "i")
	if err != nil {
		t.Fatal(err)
	}
	if err := modulewire.DecodeIndexAck(data, "req-1", "i"); err != nil {
		t.Fatal(err)
	}
	bad = strings.Replace(string(data), `true`, `false`, 1)
	if err := modulewire.DecodeIndexAck([]byte(bad), "req-1", "i"); err == nil {
		t.Fatal("false success accepted")
	}
}

func TestPrepareTargetsRejectUnsafeCombinations(t *testing.T) {
	for _, change := range []func(*contracts.IndexPrepareRequest){
		func(r *contracts.IndexPrepareRequest) { r.FencingToken = 0 },
		func(r *contracts.IndexPrepareRequest) { r.Generations[0].State = "ready" },
		func(r *contracts.IndexPrepareRequest) { r.Generations[0].KnowledgeBaseID = "other-kb" },
		func(r *contracts.IndexPrepareRequest) { r.Chunks[0].RevisionID = "other-revision" },
		func(r *contracts.IndexPrepareRequest) { r.Chunks[0].Spans[0].Characters.End = -1 },
		func(r *contracts.IndexPrepareRequest) { r.Generations = append(r.Generations, r.Generations[0]) },
		func(r *contracts.IndexPrepareRequest) { r.Vectors = nil },
		func(r *contracts.IndexPrepareRequest) { r.Vectors[0] = contracts.Vector{0.1} },
	} {
		request := indexRequest()
		change(&request)
		if _, err := modulewire.EncodeIndexPrepareRequest(modulewire.Call{InvocationID: "i", ModuleInstanceID: "m"}, request); err == nil {
			t.Fatal("unsafe prepare target encoded")
		}
	}
}

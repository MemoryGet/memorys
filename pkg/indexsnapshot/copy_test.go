package indexsnapshot

import (
	"bytes"

	"encoding/json"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"math"
	"math/rand"
	"reflect"

	"strings"
	"testing"
)

func ownFixture() contracts.IndexSnapshot {
	n := 3
	span := contracts.CharacterRange{Start: 1, End: 4}
	box := contracts.BoundingBox{Left: .5, Top: 2, Right: 3, Bottom: 4}
	locator := contracts.Locator{Precision: "exact", Page: &n, Slide: &n, Worksheet: "sheet", CellRange: "A1", ParagraphPath: []int{1, 2}, Characters: &span, Box: &box}
	source := contracts.SourceAnchor{Kind: "revision", BlobID: "blob", RevisionBlockID: "block", Locator: locator, DerivedFrom: []contracts.OriginalAnchor{{BlobID: "origin", Locator: locator}}}
	g := contracts.IndexDeletionState{SpaceID: "space", KnowledgeBaseID: "kb", DocumentID: "doc", RevisionID: "rev", GenerationID: "gen", BackendEpochID: "epoch", RecordCount: 1}
	r := contracts.IndexProjection{SpaceID: "space", KnowledgeBaseID: "kb", DocumentID: "doc", GenerationID: "gen", BackendEpochID: "epoch", Published: true, Chunk: contracts.Chunk{ID: "chunk", RevisionID: "rev", Spans: []contracts.BlockSpan{{BlockID: "block", Characters: &span}}, Text: strings.Repeat("text 文本 <>&", 80), ProfileDigest: "profile", ContentHash: "hash"}, Citation: contracts.Citation{DocumentID: "doc", RevisionID: "rev", BlockID: "block", Source: source}}
	return contracts.IndexSnapshot{Format: Format, SpaceID: "space", KnowledgeBaseID: "kb", Generations: []contracts.IndexDeletionState{g}, Records: []contracts.IndexProjection{r}}
}
func legacyCopy(s contracts.IndexSnapshot) (contracts.IndexSnapshot, error) {
	raw, e := json.Marshal(s)
	if e != nil || len(raw) > 4<<20 {
		return contracts.IndexSnapshot{}, ErrSnapshot
	}
	var v contracts.IndexSnapshot
	e = json.Unmarshal(raw, &v)
	return v, e
}
func stringPaths(v reflect.Value, path []int, out *[][]int) {
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			stringPaths(v.Elem(), append(append([]int{}, path...), -1), out)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			stringPaths(v.Field(i), append(append([]int{}, path...), i), out)
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			stringPaths(v.Index(i), append(append([]int{}, path...), i), out)
		}
	case reflect.String:
		*out = append(*out, append([]int{}, path...))
	}
}
func at(v reflect.Value, path []int) reflect.Value {
	for _, i := range path {
		if i < 0 {
			v = v.Elem()
		} else if v.Kind() == reflect.Struct {
			v = v.Field(i)
		} else {
			v = v.Index(i)
		}
	}
	return v
}
func mutate(v reflect.Value) {
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			mutate(v.Elem())
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			mutate(v.Field(i))
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			mutate(v.Index(i))
		}
	case reflect.String:
		v.SetString("changed")
	case reflect.Int:
		v.SetInt(v.Int() + 1)
	case reflect.Float64:
		v.SetFloat(v.Float() + 1)
	case reflect.Bool:
		v.SetBool(!v.Bool())
	}
}
func checkParity(t *testing.T, s contracts.IndexSnapshot) {
	t.Helper()
	want, e1 := legacyCopy(s)
	got, e2 := Copy(s)
	if (e1 == nil) != (e2 == nil) || e1 == nil && !reflect.DeepEqual(want, got) {
		t.Fatal("copy differs from legacy JSON normalization")
	}
	if e1 != nil {
		if _, err := Seal(s); err == nil {
			t.Fatal("invalid or oversized input sealed")
		}
		return
	}
	want, e1 = Seal(want)
	got, e2 = Seal(s)
	if (e1 == nil) != (e2 == nil) || e1 == nil && !reflect.DeepEqual(want, got) {
		t.Fatal("seal validity, sorting or digest differs")
	}
}
func TestSnapshotOwnCopyLegacyParity(t *testing.T) {
	s := ownFixture()
	checkParity(t, s)
	var paths [][]int
	stringPaths(reflect.ValueOf(s), nil, &paths)
	for _, path := range paths {
		c, _ := legacyCopy(s)
		v := at(reflect.ValueOf(&c).Elem(), path)
		v.SetString(v.String() + string([]byte{255, 254}))
		checkParity(t, c)
	}
	s.Records[0].Citation.Source.DerivedFrom = []contracts.OriginalAnchor{}
	s.Records[0].Citation.Source.Locator.ParagraphPath = []int{}
	s.ChunkFences = []contracts.IndexChunkFence{}
	checkParity(t, s)
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 500; i++ {
		s = ownFixture()
		v := math.Float64frombits(rng.Uint64())
		s.Records[0].Citation.Source.Locator.Box.Left = v
		checkParity(t, s)
	}
	for _, v := range []float64{math.Copysign(0, -1), math.NaN(), math.Inf(1), math.Inf(-1)} {
		s = ownFixture()
		s.Records[0].Citation.Source.Locator.Box.Top = v
		checkParity(t, s)
	}
	s = ownFixture()
	s.Records[0].Chunk.Text = strings.Repeat("x", 4<<20)
	checkParity(t, s)
}
func TestSnapshotOwnCopyNoMutableAlias(t *testing.T) {
	s := ownFixture()
	before, _ := json.Marshal(s)
	for _, f := range []func(contracts.IndexSnapshot) (contracts.IndexSnapshot, error){Copy, Seal} {
		got, e := f(s)
		if e != nil {
			t.Fatal(e)
		}
		mutate(reflect.ValueOf(&got).Elem())
		after, _ := json.Marshal(s)
		if !bytes.Equal(before, after) {
			t.Fatal("copy or seal retained mutable input references")
		}
	}
}
func TestSnapshotStableDigest(t *testing.T) {
	s, e := Seal(ownFixture())
	if e != nil || string(s.Checksum) != "9e43041e25ffbbb54ea0ee1f6ecfc9f7bb57a59ef44560e85f290ca9371f23a6" {
		t.Fatal("snapshot format digest changed", s.Checksum, e)
	}
}
func TestSnapshotFenceOwnership(t *testing.T) {
	s := ownFixture()
	s.ChunkFences = []contracts.IndexChunkFence{{GenerationID: "gen", ChunkID: "deleted"}}
	for _, f := range []func(contracts.IndexSnapshot) (contracts.IndexSnapshot, error){Copy, Seal} {
		got, e := f(s)
		if e != nil {
			t.Fatal(e)
		}
		got.ChunkFences[0].ChunkID = "changed"
		if s.ChunkFences[0].ChunkID != "deleted" {
			t.Fatal("fence slice aliases input")
		}
	}
}

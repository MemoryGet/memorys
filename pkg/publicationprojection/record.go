package publicationprojection

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrInvalid = errors.New("invalid publication projection")

// Record is a draft platform projection DTO, not authenticated publication data.
type Record struct {
	SchemaVersion           int                 `json:"schema_version"`
	ProjectionID            contracts.ID        `json:"projection_id"`
	RequestID               contracts.ID        `json:"request_id"`
	InvocationID            contracts.ID        `json:"invocation_id"`
	ParentScopeID           contracts.ID        `json:"parent_scope_id"`
	ChildScopeID            contracts.ID        `json:"child_scope_id"`
	SpaceID                 contracts.ID        `json:"space_id"`
	KnowledgeBaseID         contracts.ID        `json:"knowledge_base_id"`
	BackendEpochID          contracts.ID        `json:"backend_epoch_id"`
	RouteID                 contracts.ID        `json:"route_id"`
	ModuleInstanceID        contracts.ID        `json:"module_instance_id"`
	ParentPublicationDigest contracts.Digest    `json:"parent_publication_digest"`
	QueryProfileDigest      contracts.Digest    `json:"query_profile_digest"`
	PipelineDigest          contracts.Digest    `json:"pipeline_digest"`
	PolicyVersion           uint64              `json:"policy_version"`
	Channels                []contracts.Channel `json:"channels"`
	Entries                 []Entry             `json:"entries"`
	ExpiresAtUnixNS         int64               `json:"expires_at_unix_ns"`
}

type Entry struct {
	DocumentID         contracts.ID      `json:"document_id"`
	RevisionID         contracts.ID      `json:"revision_id"`
	PublicationVersion uint64            `json:"publication_version"`
	GenerationSetID    contracts.ID      `json:"generation_set_id"`
	GenerationID       contracts.ID      `json:"generation_id"`
	Channel            contracts.Channel `json:"channel"`
	IndexProfileDigest contracts.Digest  `json:"index_profile_digest"`
}

func validID(v contracts.ID) bool {
	return v != "" && utf8.ValidString(string(v)) && utf8.RuneCountInString(string(v)) <= 200
}
func validDigest(v contracts.Digest) bool {
	if len(v) != 64 {
		return false
	}
	for _, c := range v {
		if !((c >= 'a' && c <= 'f') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}

// closed checks exact case and required keys, unlike encoding/json's default
// case-insensitive struct lookup. Duplicate/Unicode checks happen first.
func closed(raw []byte, keys string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	names := strings.Fields(keys)
	if len(fields) != len(names) {
		return false
	}
	for _, name := range names {
		value, ok := fields[name]
		if !ok || string(value) == "null" {
			return false
		}
	}
	return true
}

// Load validates exact JSON shape and local relationships. It does not validate
// provenance, current time/policy, publication or the parent snapshot.
func Load(raw []byte) (Record, error) {
	if _, err := canonical.JSON(raw); err != nil {
		return Record{}, ErrInvalid
	}
	if !closed(raw, "schema_version projection_id request_id invocation_id parent_scope_id child_scope_id space_id knowledge_base_id backend_epoch_id route_id module_instance_id parent_publication_digest query_profile_digest pipeline_digest policy_version channels entries expires_at_unix_ns") {
		return Record{}, ErrInvalid
	}
	var envelope struct {
		Entries []json.RawMessage `json:"entries"`
	}
	if json.Unmarshal(raw, &envelope) != nil || len(envelope.Entries) > 1000 {
		return Record{}, ErrInvalid
	}
	for _, entry := range envelope.Entries {
		if !closed(entry, "document_id revision_id publication_version generation_set_id generation_id channel index_profile_digest") {
			return Record{}, ErrInvalid
		}
	}
	var record Record
	if json.Unmarshal(raw, &record) != nil || Validate(record) != nil {
		return Record{}, ErrInvalid
	}
	return record, nil
}

func entryLess(a, b Entry) bool {
	av := []string{string(a.DocumentID), string(a.RevisionID), string(a.Channel), string(a.GenerationID)}
	bv := []string{string(b.DocumentID), string(b.RevisionID), string(b.Channel), string(b.GenerationID)}
	for i := range av {
		if av[i] != bv[i] {
			return av[i] < bv[i]
		}
	}
	return false
}

func Validate(r Record) error {
	if r.SchemaVersion != 1 || r.PolicyVersion == 0 || r.ExpiresAtUnixNS <= 0 || len(r.Channels) < 1 || len(r.Channels) > 3 || len(r.Entries) < 1 || len(r.Entries) > 1000 {
		return ErrInvalid
	}
	for _, id := range []contracts.ID{r.ProjectionID, r.RequestID, r.InvocationID, r.ParentScopeID, r.ChildScopeID, r.SpaceID, r.KnowledgeBaseID, r.BackendEpochID, r.RouteID, r.ModuleInstanceID} {
		if !validID(id) {
			return ErrInvalid
		}
	}
	for _, d := range []contracts.Digest{r.ParentPublicationDigest, r.QueryProfileDigest, r.PipelineDigest} {
		if !validDigest(d) {
			return ErrInvalid
		}
	}
	order := map[contracts.Channel]int{contracts.KeywordChannel: 0, contracts.VectorChannel: 1, contracts.GraphChannel: 2}
	channels := map[contracts.Channel]bool{}
	previous := -1
	for _, c := range r.Channels {
		n, ok := order[c]
		if !ok || n <= previous {
			return ErrInvalid
		}
		channels[c] = false
		previous = n
	}
	type docIdentity struct {
		Revision, Set contracts.ID
		Version       uint64
	}
	type setIdentity struct {
		Doc, Revision contracts.ID
		Profile       contracts.Digest
	}
	docs := map[contracts.ID]docIdentity{}
	revisions := map[contracts.ID]contracts.ID{}
	sets := map[contracts.ID]setIdentity{}
	gens := map[contracts.ID]bool{}
	pairs := map[[2]string]bool{}
	for i, e := range r.Entries {
		if i > 0 && entryLess(e, r.Entries[i-1]) {
			return ErrInvalid
		}
		for _, id := range []contracts.ID{e.DocumentID, e.RevisionID, e.GenerationSetID, e.GenerationID} {
			if !validID(id) {
				return ErrInvalid
			}
		}
		if e.PublicationVersion == 0 || !validDigest(e.IndexProfileDigest) || gens[e.GenerationID] {
			return ErrInvalid
		}
		gens[e.GenerationID] = true
		if _, ok := channels[e.Channel]; !ok {
			return ErrInvalid
		}
		channels[e.Channel] = true
		d := docIdentity{e.RevisionID, e.GenerationSetID, e.PublicationVersion}
		if old, ok := docs[e.DocumentID]; ok && old != d {
			return ErrInvalid
		}
		docs[e.DocumentID] = d
		if old, ok := revisions[e.RevisionID]; ok && old != e.DocumentID {
			return ErrInvalid
		}
		revisions[e.RevisionID] = e.DocumentID
		s := setIdentity{e.DocumentID, e.RevisionID, e.IndexProfileDigest}
		if old, ok := sets[e.GenerationSetID]; ok && old != s {
			return ErrInvalid
		}
		sets[e.GenerationSetID] = s
		pair := [2]string{string(e.DocumentID), string(e.Channel)}
		if pairs[pair] {
			return ErrInvalid
		}
		pairs[pair] = true
	}
	for _, covered := range channels {
		if !covered {
			return ErrInvalid
		}
	}
	return nil
}

func ValidatedDigest(raw []byte) (Record, contracts.Digest, error) {
	r, err := Load(raw)
	if err != nil {
		return Record{}, "", err
	}
	d, err := DigestUnchecked(raw)
	if err != nil {
		return Record{}, "", ErrInvalid
	}
	return r, d, nil
}

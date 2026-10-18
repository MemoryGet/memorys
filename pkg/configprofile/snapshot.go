// Package configprofile commits provider-neutral configuration snapshots.
package configprofile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/preparation"
)

var ErrSnapshot = errors.New("configuration snapshot rejected")
var identifier = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,99}$`)
var version = regexp.MustCompile(`^[1-9][0-9]*\.(0|[1-9][0-9]*)(\.0)?$`)

type Snapshot struct {
	Format   string
	Pipeline contracts.PipelineProfile
	Index    contracts.IndexProfile
	Query    contracts.QueryProfile
}

// LoadFile reads one deployment-owned, sealed profile snapshot. It does not
// watch or reload the path; replacement requires a new assembler generation.
func LoadFile(path string) (Snapshot, error) {
	if path == "" {
		return Snapshot{}, ErrSnapshot
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 || len(raw) > 1<<20 {
		return Snapshot{}, ErrSnapshot
	}
	return Decode(raw)
}

func commit(format string, value any) (contracts.Digest, error) {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 1<<20 {
		return "", ErrSnapshot
	}
	encoded, err := canonical.JSON(raw)
	if err != nil {
		return "", ErrSnapshot
	}
	sum := sha256.Sum256(append([]byte(format+"\x00"), encoded...))
	return contracts.Digest(hex.EncodeToString(sum[:])), nil
}
func bindings(m map[contracts.Capability]contracts.ModuleBinding) bool {
	if len(m) == 0 || len(m) > 64 {
		return false
	}
	for cap, b := range m {
		if cap != b.Capability || !identifier.MatchString(string(cap)) || !identifier.MatchString(string(b.InstanceID)) || !version.MatchString(b.ContractVersion) || len(b.RequiredFeatures) > 64 {
			return false
		}
		seen := map[string]bool{}
		for _, f := range b.RequiredFeatures {
			if !identifier.MatchString(f) || seen[f] {
				return false
			}
			seen[f] = true
		}
	}
	return true
}
func queryValid(q contracts.QueryProfile) bool {
	p := q.Policy
	f := q.Reflection
	if !identifier.MatchString(string(q.ID)) || q.Version == 0 || !bindings(q.Bindings) || p.SchemaVersion != 1 || p.CandidateLimit < 1 || p.CandidateLimit > 1000 || p.AnswerEvidenceLimit < 1 || p.AnswerEvidenceLimit > p.CandidateLimit || p.FusionMethod != "rrf" || p.RankConstant < 1 || p.RankConstant > 10000 || p.RetrievalTimeoutMS < 1 || p.RetrievalTimeoutMS > 30000 || p.RerankTimeoutMS < 0 || p.RerankTimeoutMS > 30000 || p.GenerationTimeoutMS < 0 || p.GenerationTimeoutMS > 30000 || p.MaxOutputTokens < 0 || p.MaxOutputTokens > 1000000 || len(q.Routes) == 0 || len(q.Routes) > 32 || f.MaxExtraRounds < 0 || f.MaxExtraRounds > 2 || f.MaxPlanQueries < 0 || f.MaxPlanQueries > 32 || f.TotalBudgetMS < 0 || f.TotalBudgetMS > 30000 {
		return false
	}
	if p.EnableGeneration && (p.MaxOutputTokens == 0 || p.GenerationTimeoutMS == 0 || q.Bindings[contracts.Generation].InstanceID == "") || p.EnableRerank && (p.RerankTimeoutMS == 0 || q.Bindings[contracts.Reranking].InstanceID == "") || f.Enabled && (f.TotalBudgetMS == 0 || f.MaxPlanQueries == 0) || !f.Enabled && (f.MaxExtraRounds != 0 || f.MaxPlanQueries != 0 || f.TotalBudgetMS != 0) {
		return false
	}
	allocated := 0
	seen := map[contracts.ID]bool{}
	for _, r := range q.Routes {
		if !bindings(map[contracts.Capability]contracts.ModuleBinding{r.Binding.Capability: r.Binding}) || (r.Binding.Capability != contracts.Retrieval && r.Binding.Capability != contracts.CompositeRetrieval) || !identifier.MatchString(string(r.ID)) || seen[r.ID] || r.CandidateLimit < 1 || len(r.Channels) == 0 || len(r.Channels) > 3 {
			return false
		}
		seen[r.ID] = true
		allocated += r.CandidateLimit
		if allocated > p.CandidateLimit {
			return false
		}
		channels := map[contracts.Channel]bool{}
		for _, c := range r.Channels {
			if (c != contracts.KeywordChannel && c != contracts.VectorChannel && c != contracts.GraphChannel) || channels[c] {
				return false
			}
			channels[c] = true
		}
	}
	return true
}

// Seal owns its returned maps/slices and recomputes all three commitments.
// Replacing referenced index/query content changes the pipeline digest too.
func Seal(s Snapshot) (Snapshot, error) {
	if s.Format != "akos.profile-snapshot/1" || !identifier.MatchString(string(s.Pipeline.ID)) || s.Pipeline.Version == 0 || s.Pipeline.IndexProfile.ID != s.Index.ID || s.Pipeline.IndexProfile.Version != s.Index.Version || s.Pipeline.QueryProfile.ID != s.Query.ID || s.Pipeline.QueryProfile.Version != s.Query.Version || !bindings(s.Index.Bindings) || !queryValid(s.Query) {
		return Snapshot{}, ErrSnapshot
	}
	raw, err := json.Marshal(s)
	if err != nil || len(raw) > 1<<20 {
		return Snapshot{}, ErrSnapshot
	}
	var own Snapshot
	if json.Unmarshal(raw, &own) != nil {
		return Snapshot{}, ErrSnapshot
	}
	s = own
	s.Index.Digest, err = preparation.ProfileDigest(s.Index)
	if err != nil {
		return Snapshot{}, ErrSnapshot
	}
	s.Query.Digest = ""
	s.Query.Digest, err = commit("akos.query-profile/1", s.Query)
	if err != nil {
		return Snapshot{}, ErrSnapshot
	}
	s.Pipeline.Digest = ""
	s.Pipeline.Digest, err = commit("akos.pipeline-profile/1", struct {
		Pipeline                 contracts.PipelineProfile
		IndexDigest, QueryDigest contracts.Digest
	}{s.Pipeline, s.Index.Digest, s.Query.Digest})
	if err != nil {
		return Snapshot{}, ErrSnapshot
	}
	return s, nil
}
func Encode(s Snapshot) ([]byte, error) {
	verified, err := Seal(s)
	if err != nil || verified.Pipeline.Digest != s.Pipeline.Digest || verified.Query.Digest != s.Query.Digest || verified.Index.Digest != s.Index.Digest {
		return nil, ErrSnapshot
	}
	return json.Marshal(verified)
}
func Decode(raw []byte) (Snapshot, error) {
	if len(raw) == 0 || len(raw) > 1<<20 {
		return Snapshot{}, ErrSnapshot
	}
	if _, err := canonical.JSON(raw); err != nil {
		return Snapshot{}, ErrSnapshot
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var s Snapshot
	if d.Decode(&s) != nil {
		return Snapshot{}, ErrSnapshot
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return Snapshot{}, ErrSnapshot
	}
	encoded, err := Encode(s)
	if err != nil {
		return Snapshot{}, ErrSnapshot
	}
	a, _ := canonical.JSON(raw)
	b, _ := canonical.JSON(encoded)
	if !bytes.Equal(a, b) {
		return Snapshot{}, ErrSnapshot
	}
	return s, nil
}

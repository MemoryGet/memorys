package fusion

import (
	"context"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

type routePolicy struct {
	retriever contracts.ID
	limit     int
	required  bool
}

// ProfileRRF captures the route budget of a separately verified configuration.
// It does not authenticate the profiles or authorize any candidate's content.
type ProfileRRF struct {
	pipeline     contracts.ProfileReference
	query        contracts.ProfileReference
	digest       contracts.Digest
	queryDigest  contracts.Digest
	rankConstant int
	limit        int
	routes       map[contracts.ID]routePolicy
}

var _ contracts.Fuser = (*ProfileRRF)(nil)

type ProfileIdentity struct {
	Pipeline       contracts.ProfileReference
	Query          contracts.ProfileReference
	PipelineDigest contracts.Digest
	QueryDigest    contracts.Digest
}

func digestValid(v contracts.Digest) bool {
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

func NewRRFForProfile(pipeline contracts.PipelineProfile, query contracts.QueryProfile) (*ProfileRRF, error) {
	if !validID(pipeline.ID) || pipeline.Version == 0 || !digestValid(pipeline.Digest) || !validID(query.ID) || query.Version == 0 || !digestValid(query.Digest) || pipeline.QueryProfile.ID != query.ID || pipeline.QueryProfile.Version != query.Version {
		return nil, ErrInvalid
	}
	if query.Policy.FusionMethod != "rrf" || query.Policy.RankConstant < 1 || query.Policy.RankConstant > 10000 || query.Policy.CandidateLimit < 1 || query.Policy.CandidateLimit > 1000 || len(query.Routes) == 0 || len(query.Routes) > 32 {
		return nil, ErrInvalid
	}
	f := &ProfileRRF{pipeline: contracts.ProfileReference{ID: pipeline.ID, Version: pipeline.Version}, query: contracts.ProfileReference{ID: query.ID, Version: query.Version}, digest: pipeline.Digest, queryDigest: query.Digest, rankConstant: query.Policy.RankConstant, limit: query.Policy.CandidateLimit, routes: map[contracts.ID]routePolicy{}}
	allocated := 0
	for _, route := range query.Routes {
		if !validID(route.ID) || !validID(route.Binding.InstanceID) || route.CandidateLimit < 1 || route.CandidateLimit > f.limit || (route.Binding.Capability != contracts.Retrieval && route.Binding.Capability != contracts.CompositeRetrieval) {
			return nil, ErrInvalid
		}
		if _, exists := f.routes[route.ID]; exists {
			return nil, ErrInvalid
		}
		allocated += route.CandidateLimit
		if allocated > f.limit {
			return nil, ErrInvalid
		}
		f.routes[route.ID] = routePolicy{route.Binding.InstanceID, route.CandidateLimit, route.Required}
	}
	return f, nil
}

func (f *ProfileRRF) Identity() ProfileIdentity {
	if f == nil {
		return ProfileIdentity{}
	}
	return ProfileIdentity{f.pipeline, f.query, f.digest, f.queryDigest}
}

func (f *ProfileRRF) Fuse(ctx context.Context, request contracts.FuseRequest) ([]contracts.Candidate, error) {
	if f == nil || ctx == nil || !validID(request.Request.RequestID) || request.Request.ProfileDigest != f.digest || request.RankConstant != f.rankConstant || request.Limit < 1 || request.Limit > f.limit || len(request.Lists) > 32 {
		return nil, ErrInvalid
	}
	seen := map[contracts.ID]bool{}
	total := 0
	for _, list := range request.Lists {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		policy, exists := f.routes[list.RouteID]
		if !exists || len(list.Candidates) > policy.limit {
			return nil, ErrInvalid
		}
		seen[list.RouteID] = true
		total += len(list.Candidates)
		if total > f.limit {
			return nil, ErrInvalid
		}
		for _, candidate := range list.Candidates {
			if candidate.RetrieverID != policy.retriever {
				return nil, ErrInvalid
			}
		}
	}
	for route, policy := range f.routes {
		if policy.required && !seen[route] {
			return nil, ErrInvalid
		}
	}
	return (RRF{}).Fuse(ctx, request)
}

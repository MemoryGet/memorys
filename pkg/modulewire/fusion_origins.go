package modulewire

import "github.com/MemoryGet/memorys/pkg/contracts"

func checkFusionOrigins(candidate contracts.Candidate) error {
	if candidate.FusionOrigins == nil {
		return nil
	}
	if len(candidate.FusionOrigins) == 0 || len(candidate.FusionOrigins) > 32 {
		return invalid()
	}
	routes := map[contracts.ID]bool{}
	generations := map[contracts.ID]bool{}
	allowed := map[contracts.ID]bool{}
	for _, id := range candidate.GenerationIDs {
		allowed[id] = true
	}
	for _, origin := range candidate.FusionOrigins {
		if ensureIDs(origin.RouteID, origin.RetrieverID) != nil || routes[origin.RouteID] || origin.Rank < 1 || origin.Rank > 1000 || len(origin.GenerationIDs) == 0 {
			return invalid()
		}
		routes[origin.RouteID] = true
		for _, id := range origin.GenerationIDs {
			if ensureIDs(id) != nil || !allowed[id] {
				return invalid()
			}
			generations[id] = true
		}
	}
	if len(generations) != len(allowed) {
		return invalid()
	}
	return nil
}

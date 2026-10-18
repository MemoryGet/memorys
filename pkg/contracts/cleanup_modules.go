package contracts

import "context"

// CleanupModuleBinding is a current deployment decision for a retained target,
// separate from immutable source metadata. It contains no credentials/endpoints.
type CleanupModuleBinding struct {
	Version                                                      uint64
	SpaceID, KnowledgeBaseID, BindingReference, ModuleInstanceID ID
	ProfileDigest, RuntimeConfigDigest                           Digest
	Provider, ImplementationVersion, ContractVersion             string
	GenerationID, BackendEpochID                                 ID
	Channel                                                      Channel
}
type CleanupModuleSource interface {
	ResolveCleanupModule(context.Context, AccessScope, CleanupOperation) (CleanupModuleBinding, error)
}

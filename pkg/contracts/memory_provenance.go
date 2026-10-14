package contracts

const (
	MemoryUserConfirmed    = "user_confirmed"
	MemoryTaskExperience   = "task_experience"
	MemoryKnowledgeSummary = "knowledge_summary"
)

// ValidateMemoryProvenance checks declared source categories and identifier
// relationships only. It does not authenticate confirmations/tasks, locate
// citation spans or authorize current access to any source.
func ValidateMemoryProvenance(p MemoryProvenance) error {
	invalid := func() error {
		return PublicError{Code: "validation_failed", SafeMessage: "Invalid memory provenance"}
	}
	for _, id := range []*ID{p.ConfirmationID, p.TaskID, p.FeedbackID} {
		if id != nil && !storeText(string(*id)) {
			return invalid()
		}
	}
	switch p.Kind {
	case MemoryUserConfirmed:
		if p.ConfirmationID == nil || p.TaskID != nil || p.FeedbackID != nil {
			return invalid()
		}
	case MemoryTaskExperience:
		if p.ConfirmationID != nil || (p.TaskID == nil && p.FeedbackID == nil) {
			return invalid()
		}
	case MemoryKnowledgeSummary:
		if p.ConfirmationID != nil || p.TaskID != nil || p.FeedbackID != nil || len(p.Citations) == 0 {
			return invalid()
		}
	default:
		return invalid()
	}
	if len(p.Citations) > 256 {
		return invalid()
	}
	revisionOwners := map[ID]ID{}
	for _, c := range p.Citations {
		if !storeText(string(c.DocumentID)) || !storeText(string(c.RevisionID)) || !storeText(string(c.BlockID)) {
			return invalid()
		}
		if owner, exists := revisionOwners[c.RevisionID]; exists && owner != c.DocumentID {
			return invalid()
		}
		revisionOwners[c.RevisionID] = c.DocumentID
	}
	return nil
}

package contracts

func invalidGraphProjection() error {
	return PublicError{Code: "validation_failed", SafeMessage: "Invalid graph projection result"}
}

// Shape only; these IDs/digests do not prove authority, origin or content.
func ValidateGraphProjectionTarget(target GraphProjectionTarget) error {
	for _, id := range []ID{target.SpaceID, target.KnowledgeBaseID, target.BackendEpochID, target.GenerationID} {
		if !storeText(string(id)) {
			return invalidGraphProjection()
		}
	}
	for _, digest := range []Digest{target.IndexProfileDigest, target.ExtractionProfileDigest, target.OntologyDigest, target.SourceSnapshotDigest} {
		if !validDigest(digest) {
			return invalidGraphProjection()
		}
	}
	return nil
}

// Expectations come from the application operation. A provider's matching
// declaration is not proof that filters or source completeness were tested.
func ValidateGraphProjectionProof(target GraphProjectionTarget, attempt ID, input Digest, state string, proof GraphProjectionProof) error {
	if ValidateGraphProjectionTarget(target) != nil || ValidateGraphProjectionTarget(proof.Target) != nil || !storeText(string(attempt)) || !validDigest(input) || (state != "prepared" && state != "verified") {
		return invalidGraphProjection()
	}
	if proof.Target != target || proof.AttemptID != attempt || proof.InputDigest != input || proof.State != state || !storeText(string(proof.ProofID)) {
		return invalidGraphProjection()
	}
	return nil
}

// Completion requires an evidence reference but does not authenticate that
// evidence. Unknown cannot be represented as completed or a release permit.
func ValidateGraphCleanupReceipt(expected GraphCleanupExpectation, receipt GraphCleanupReceipt) error {
	target, operation := expected.Target, expected.OperationID
	if ValidateGraphProjectionTarget(target) != nil || ValidateGraphProjectionTarget(receipt.Target) != nil || !storeText(string(operation)) || receipt.OperationID != operation || receipt.Target != target {
		return invalidGraphProjection()
	}
	if (expected.OperationKind != "source_invalidation" && expected.OperationKind != "generation_deletion") || !validDigest(expected.RequestDigest) || receipt.OperationKind != expected.OperationKind || receipt.RequestDigest != expected.RequestDigest {
		return invalidGraphProjection()
	}
	if receipt.ProviderRecordReference != "" && !storeText(string(receipt.ProviderRecordReference)) {
		return invalidGraphProjection()
	}
	switch receipt.State {
	case "completed":
		if receipt.CompletedAt == nil || receipt.CompletedAt.IsZero() || receipt.ProviderRecordReference == "" {
			return invalidGraphProjection()
		}
	case "queued", "running", "failed", "unknown":
		if receipt.CompletedAt != nil {
			return invalidGraphProjection()
		}
	default:
		return invalidGraphProjection()
	}
	return nil
}

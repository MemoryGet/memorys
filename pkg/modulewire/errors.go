package modulewire

import "github.com/MemoryGet/memorys/pkg/contracts"

type remoteFailure struct {
	RequestID, InvocationID contracts.ID
	Error                   contracts.PublicError
}
type safeErrorDefinition struct {
	message  string
	mayRetry bool
}

var safeErrorCatalog = map[contracts.Code]safeErrorDefinition{
	"invalid_argument":       {"Invalid request", false},
	"validation_failed":      {"Validation failed", false},
	"unsupported_format":     {"Unsupported format", false},
	"unauthenticated":        {"Authentication required", false},
	"forbidden":              {"Access denied", false},
	"not_found":              {"Resource unavailable", false},
	"conflict":               {"Request conflict", true},
	"publication_changed":    {"Publication changed", true},
	"cursor_expired":         {"Cursor expired", true},
	"capability_missing":     {"Capability unavailable", false},
	"contract_incompatible":  {"Contract incompatible", false},
	"rate_limited":           {"Rate limit exceeded", true},
	"deadline_exceeded":      {"Deadline exceeded", true},
	"dependency_unavailable": {"Dependency unavailable", true},
	"internal":               {"Internal failure", false},
}

func init() {
	register(remoteFailure{}, "RequestID:request_id InvocationID:invocation_id Error:error")
	register(contracts.PublicError{}, "Code:code SafeMessage:safe_message Retryable:retryable RequestID:request_id")
}

// NormalizeRemoteError discards incoming message text and uses a closed catalog.
// Retryable is advisory; this function never authorizes replaying a write.
func NormalizeRemoteError(failure contracts.PublicError, requestID contracts.ID) (contracts.PublicError, error) {
	definition, known := safeErrorCatalog[failure.Code]
	if !known || requestID == "" || failure.RequestID != requestID {
		return contracts.PublicError{}, invalid()
	}
	return contracts.PublicError{Code: failure.Code, SafeMessage: definition.message, Retryable: failure.Retryable && definition.mayRetry, RequestID: requestID}, nil
}
func EncodeRemoteError(requestID, invocationID contracts.ID, failure contracts.PublicError) ([]byte, error) {
	if ensureIDs(requestID, invocationID) != nil {
		return nil, invalid()
	}
	normalized, err := NormalizeRemoteError(failure, requestID)
	if err != nil {
		return nil, err
	}
	return encode(remoteFailure{requestID, invocationID, normalized})
}
func DecodeRemoteError(data []byte, requestID, invocationID contracts.ID) (contracts.PublicError, error) {
	if ensureIDs(requestID, invocationID) != nil {
		return contracts.PublicError{}, invalid()
	}
	var response remoteFailure
	if err := decode(data, &response); err != nil {
		return contracts.PublicError{}, err
	}
	if response.RequestID != requestID || response.InvocationID != invocationID {
		return contracts.PublicError{}, invalid()
	}
	return NormalizeRemoteError(response.Error, requestID)
}

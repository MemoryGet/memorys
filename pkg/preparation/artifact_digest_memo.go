package preparation

import (
	"sync"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

// ArtifactDigestMemo stores only a pure digest keyed by the exact freshly
// encoded artifact body. It is bounded and intended for one invocation; it
// must not be used as a substitute for reading or validating current data.
type ArtifactDigestMemo struct {
	mu      sync.Mutex
	entries map[string]contracts.Digest
	bytes   int
}

// Digest repeats input validation and encoding on every call. A hit avoids only
// canonicalization and hashing; changes to any committed byte cause a miss.
func (m *ArtifactDigestMemo) Digest(artifact Artifact) (contracts.Digest, error) {
	raw, err := artifactDigestInput(artifact)
	if err != nil || len(raw) > 4<<20 {
		return "", ErrPreparation
	}
	m.mu.Lock()
	value, ok := m.entries[string(raw)]
	m.mu.Unlock()
	if ok {
		return value, nil
	}
	value, err = artifactDigestBytes(raw)
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	if len(m.entries) < 1000 && len(raw) <= (4<<20)-m.bytes {
		if m.entries == nil {
			m.entries = make(map[string]contracts.Digest)
		}
		if _, exists := m.entries[string(raw)]; !exists {
			m.entries[string(raw)] = value
			m.bytes += len(raw)
		}
	}
	m.mu.Unlock()
	return value, nil
}

package kernel

import (
	"reflect"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

func matches[T any](value any) bool { _, ok := value.(T); return ok }

var portChecks = map[contracts.Capability]func(any) bool{
	contracts.Authentication:          matches[contracts.Authenticator],
	contracts.Authorization:           matches[contracts.Authorizer],
	contracts.BlobStorage:             matches[contracts.BlobStore],
	contracts.Parsing:                 matches[contracts.Parser],
	contracts.Splitting:               matches[contracts.Splitter],
	contracts.Embedding:               matches[contracts.Embedder],
	contracts.IndexWriting:            matches[contracts.IndexWriter],
	contracts.IndexCleanupCapability:  matches[contracts.IndexCleanupBackend],
	contracts.IndexSnapshotCapability: matches[contracts.IndexSnapshotStore],
	contracts.Retrieval:               matches[contracts.Retriever],
	contracts.CompositeRetrieval:      matches[contracts.CompositeEngine],
	contracts.Fusion:                  matches[contracts.Fuser],
	contracts.Reranking:               matches[contracts.Reranker],
	contracts.Generation:              matches[contracts.Generator],
	contracts.Ingestion:               matches[contracts.Ingestor],
	contracts.Publication:             matches[contracts.PublicationService],
	contracts.Reflection:              matches[contracts.Reflector],
	contracts.MemoryStorage:           matches[contracts.Memory],
	contracts.GraphStorage:            matches[contracts.Graph],
	contracts.GraphProjectionControl:  matches[contracts.GraphProjectionManager],
	contracts.Metadata:                matches[contracts.MetadataRepository],
	contracts.Transactions:            matches[contracts.UnitOfWork],
	contracts.QueryReading:            matches[contracts.QueryReadUnit],
	contracts.TaskFacts:               matches[contracts.TaskRepository],
	contracts.TaskDelivery:            matches[contracts.TaskQueue],
	contracts.Events:                  matches[contracts.EventBus],
	contracts.Audit:                   matches[contracts.AuditRepository],
	contracts.Evaluation:              matches[contracts.Evaluator],
	contracts.Observability:           matches[contracts.Observer],
}

func nilService(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}

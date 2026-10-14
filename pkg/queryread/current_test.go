package queryread_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/queryread"
)

type currentUnit struct {
	opens      int
	withdrawn  bool
	failID     contracts.ID
	wrongSpace bool
}
type currentRepository struct {
	withdrawn  bool
	failID     contracts.ID
	wrongSpace bool
}

func (r currentRepository) ReadPublishedPage(context.Context, contracts.PublishedPageRequest) (contracts.PublishedPage, error) {
	return contracts.PublishedPage{}, errors.New("unused")
}
func (r currentRepository) LoadActiveEpoch(context.Context, contracts.ID) (contracts.BackendEpoch, error) {
	return contracts.BackendEpoch{}, errors.New("unused")
}
func (r currentRepository) LoadCurrentDocument(_ context.Context, id contracts.ID) (contracts.CurrentDocumentState, error) {
	if id == r.failID {
		return contracts.CurrentDocumentState{}, errors.New("private database detail")
	}
	d := contracts.Document{ID: id, SpaceID: "space", KnowledgeBaseID: "kb", RowVersion: 1}
	if r.wrongSpace {
		d.SpaceID = "other"
	}
	if r.withdrawn {
		d.RowVersion = 2
	} else {
		revision := contracts.ID("revision")
		d.ActiveRevisionID = &revision
	}
	return contracts.CurrentDocumentState{Document: d}, nil
}
func (u *currentUnit) WithinRead(c context.Context, _ contracts.AccessScope, callback func(context.Context, contracts.QueryReadIdentity, contracts.QueryReadRepository) error) error {
	u.opens++
	return callback(c, contracts.QueryReadIdentity{StoreEpochID: "store", ReadVersion: strconv.Itoa(u.opens), Watermark: "watermark"}, currentRepository{u.withdrawn, u.failID, u.wrongSpace})
}
func currentScope() contracts.AccessScope {
	return contracts.AccessScope{ScopeID: "scope", SubjectID: "subject", SpaceID: "space", KnowledgeBaseIDs: []contracts.ID{"kb"}, Action: "knowledge.search", PolicyVersion: 1, ExpiresAt: time.Now().Add(time.Minute)}
}

func TestCurrentObservationOpensSeparateViewAfterWithdrawal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	unit := &currentUnit{}
	var old contracts.CurrentDocumentState
	if err := unit.WithinRead(ctx, currentScope(), func(c context.Context, _ contracts.QueryReadIdentity, r contracts.QueryReadRepository) error {
		var err error
		old, err = r.LoadCurrentDocument(c, "doc")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	unit.withdrawn = true
	result, err := queryread.ObserveCurrent(ctx, unit, currentScope(), []contracts.ID{"doc"}, queryread.CurrentLimits{MaxDocuments: 2, MaxBytes: 10000})
	if err != nil || unit.opens != 2 || result.Identity.ReadVersion != "2" || result.Documents[0].Document.ActiveRevisionID != nil || old.Document.ActiveRevisionID == nil {
		t.Fatal(result, err, unit.opens)
	}
}

func TestCurrentObservationDoesNotReturnPartialOrForeignState(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for _, unit := range []*currentUnit{{failID: "second"}, {wrongSpace: true}} {
		result, err := queryread.ObserveCurrent(ctx, unit, currentScope(), []contracts.ID{"first", "second"}, queryread.CurrentLimits{MaxDocuments: 2, MaxBytes: 10000})
		if err == nil || len(result.Documents) != 0 || result.Identity.ReadVersion != "" {
			t.Fatal("partial or foreign state returned", result, err)
		}
	}
}

func TestCurrentObservationLimitsAndInvalidAdmission(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	unit := &currentUnit{}
	for _, ids := range [][]contracts.ID{nil, {"doc", "doc"}, {""}} {
		if _, err := queryread.ObserveCurrent(ctx, unit, currentScope(), ids, queryread.CurrentLimits{MaxDocuments: 2, MaxBytes: 10000}); err == nil || unit.opens != 0 {
			t.Fatal("bad request read backend")
		}
	}
	if _, err := queryread.ObserveCurrent(context.Background(), unit, currentScope(), []contracts.ID{"doc"}, queryread.CurrentLimits{MaxDocuments: 2, MaxBytes: 10000}); err == nil || unit.opens != 0 {
		t.Fatal("unbounded context admitted")
	}
	result, err := queryread.ObserveCurrent(ctx, unit, currentScope(), []contracts.ID{"doc"}, queryread.CurrentLimits{MaxDocuments: 2, MaxBytes: 1})
	if err != queryread.ErrLimit || len(result.Documents) != 0 {
		t.Fatal(result, err)
	}
}

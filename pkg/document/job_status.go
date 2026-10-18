package document

import (
	"context"
	"errors"
	"math"
	"reflect"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrJobStatus = errors.New("job status unavailable")
var ErrJobUnauthenticated = errors.New("job authentication required")

type JobStatusInput struct {
	Credential contracts.Credential
	Request    contracts.RequestContext
	JobID      contracts.ID
}
type JobStatusService struct {
	Auth   contracts.Authenticator
	Reader contracts.ResourceJobReader
}

func validResourceJob(record contracts.ResourceJob, id contracts.ID) bool {
	j := record.Job
	if j.ID != id || !frozenID(record.ResourceID) || !frozenID(j.SpaceID) || !frozenID(j.OperationID) || !candidateDigest(j.ProfileDigest) || j.RowVersion == 0 || !utf8.ValidString(j.Stage) || j.Stage == "" || utf8.RuneCountInString(j.Stage) > 10000 || math.IsNaN(j.Progress) || math.IsInf(j.Progress, 0) || j.Progress < 0 || j.Progress > 1 {
		return false
	}
	switch j.State {
	case "queued", "running", "completed", "failed", "cancelled":
		return true
	}
	return false
}

func (p JobStatusService) GetJob(ctx context.Context, input JobStatusInput) (contracts.ResourceJob, error) {
	var empty contracts.ResourceJob
	if ctx == nil || p.Auth == nil || p.Reader == nil || !frozenID(input.JobID) || !frozenID(input.Request.RequestID) || input.Request.Deadline.IsZero() {
		return empty, ErrJobStatus
	}
	if _, ok := ctx.Deadline(); !ok {
		return empty, ErrJobStatus
	}
	ctx, cancel := context.WithDeadline(ctx, input.Request.Deadline)
	defer cancel()
	credential := input.Credential
	credential.Secret = append([]byte(nil), credential.Secret...)
	defer clear(credential.Secret)
	authenticate := func() (contracts.Principal, error) {
		copied := credential
		copied.Secret = append([]byte(nil), credential.Secret...)
		defer clear(copied.Secret)
		who, e := p.Auth.Authenticate(ctx, copied)
		if e != nil || ctx.Err() != nil {
			return contracts.Principal{}, ErrJobUnauthenticated
		}
		who, e = deletionCopy(who)
		if e != nil || !frozenID(who.SubjectID) || who.Issuer == "" {
			return contracts.Principal{}, ErrJobUnauthenticated
		}
		return who, nil
	}
	who, e := authenticate()
	if e != nil {
		return empty, e
	}
	read := func() (contracts.ResourceJob, error) {
		q, e := deletionCopy(contracts.JobReadRequest{Request: input.Request, Principal: who, JobID: input.JobID})
		if e != nil {
			return empty, ErrJobStatus
		}
		record, e := p.Reader.ReadResourceJob(ctx, q)
		if e != nil {
			return empty, e
		}
		record, e = deletionCopy(record)
		if e != nil || !validResourceJob(record, input.JobID) {
			return empty, ErrJobStatus
		}
		return record, nil
	}
	before, e := read()
	if e != nil {
		return empty, e
	}
	fresh, e := authenticate()
	if e != nil || !reflect.DeepEqual(who, fresh) {
		return empty, ErrJobUnauthenticated
	}
	after, e := read()
	if e != nil {
		return empty, e
	}
	if before.ResourceID != after.ResourceID || before.Job.SpaceID != after.Job.SpaceID || before.Job.OperationID != after.Job.OperationID || before.Job.ProfileDigest != after.Job.ProfileDigest || before.Job.Stage != after.Job.Stage || after.Job.RowVersion < before.Job.RowVersion || (after.Job.RowVersion == before.Job.RowVersion && !reflect.DeepEqual(before, after)) {
		return empty, ErrJobStatus
	}
	fresh, e = authenticate()
	if e != nil || !reflect.DeepEqual(who, fresh) {
		return empty, ErrJobUnauthenticated
	}
	if ctx.Err() != nil {
		return empty, ErrJobStatus
	}
	return after, nil
}

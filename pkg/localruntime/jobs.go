package localruntime

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/postgres"
)

// JobApplications exposes the durable deletion-job views/control ports to a
// host without making the local runtime depend on transport packages.
type JobApplications struct {
	Status JobStatusApplication
	Cancel JobCancellationApplication
	Retry  JobRetryApplication
}

type JobStatusApplication struct{ runtime *Search }
type JobCancellationApplication struct{ runtime *Search }
type JobRetryApplication struct{ runtime *Search }

// ParseJobApplications exposes parser-task status/control through the same
// strict transport handlers as other durable jobs, while using document.parse
// authorization and the parse-task repository.
type ParseJobApplications struct {
	Status ParseJobStatusApplication
	Cancel ParseJobCancellationApplication
	Retry  ParseJobRetryApplication
}

type ParseJobStatusApplication struct{ runtime *Search }
type ParseJobCancellationApplication struct{ runtime *Search }
type ParseJobRetryApplication struct{ runtime *Search }

type PublicationJobStatusApplication struct{ runtime *Search }
type PublicationJobCancellationApplication struct{ runtime *Search }
type PublicationJobRetryApplication struct{ runtime *Search }

func (s *Search) NewPublicationJobStatusApplication() (PublicationJobStatusApplication, error) {
	if s == nil || s.db == nil || s.auth == nil {
		return PublicationJobStatusApplication{}, errors.New("publication job status unavailable")
	}
	return PublicationJobStatusApplication{runtime: s}, nil
}

func (s *Search) NewPublicationJobCancellationApplication() (PublicationJobCancellationApplication, error) {
	if s == nil || s.db == nil || s.auth == nil {
		return PublicationJobCancellationApplication{}, errors.New("publication job cancellation unavailable")
	}
	return PublicationJobCancellationApplication{runtime: s}, nil
}

func (s *Search) NewPublicationJobRetryApplication() (PublicationJobRetryApplication, error) {
	if s == nil || s.db == nil || s.auth == nil {
		return PublicationJobRetryApplication{}, errors.New("publication job retry unavailable")
	}
	return PublicationJobRetryApplication{runtime: s}, nil
}

func (a PublicationJobStatusApplication) GetJob(ctx context.Context, input document.JobStatusInput) (contracts.ResourceJob, error) {
	if a.runtime == nil {
		return contracts.ResourceJob{}, document.ErrJobStatus
	}
	authorizer := postgres.Authorizer{DB: a.runtime.db, ScopeLifetime: time.Minute}
	reader := postgres.PublicationTaskResourceReader{DB: a.runtime.db, Authorizer: authorizer}
	return (document.JobStatusService{Auth: a.runtime.auth, Reader: reader}).GetJob(ctx, input)
}

func (a PublicationJobCancellationApplication) CancelJob(ctx context.Context, input document.JobCancellationInput) (contracts.ResourceJob, error) {
	if a.runtime == nil {
		return contracts.ResourceJob{}, document.ErrJobCancellation
	}
	authorizer := postgres.Authorizer{DB: a.runtime.db, ScopeLifetime: time.Minute}
	reader := postgres.PublicationTaskResourceReader{DB: a.runtime.db, Authorizer: authorizer}
	service := document.PublicationJobCancellationService{Auth: a.runtime.auth, Reader: reader, Authorizer: authorizer, Control: postgres.PublicationTaskControl{DB: a.runtime.db}}
	return service.CancelJob(ctx, input)
}

func (a PublicationJobCancellationApplication) PauseJob(ctx context.Context, input document.JobCancellationInput) (contracts.ResourceJob, error) {
	if a.runtime == nil {
		return contracts.ResourceJob{}, document.ErrJobCancellation
	}
	authorizer := postgres.Authorizer{DB: a.runtime.db, ScopeLifetime: time.Minute}
	reader := postgres.PublicationTaskResourceReader{DB: a.runtime.db, Authorizer: authorizer}
	service := document.PublicationJobPauseService{Auth: a.runtime.auth, Reader: reader, Authorizer: authorizer, Control: postgres.PublicationTaskControl{DB: a.runtime.db}}
	return service.PauseJob(ctx, input)
}

func (a PublicationJobRetryApplication) RetryJob(ctx context.Context, input document.JobRetryInput) (contracts.PublicationTaskRetryResult, error) {
	if a.runtime == nil {
		return contracts.PublicationTaskRetryResult{}, document.ErrPublicationRetry
	}
	version, digest, err := a.runtime.ProfileIdentity()
	if err != nil || version == 0 {
		return contracts.PublicationTaskRetryResult{}, document.ErrPublicationRetry
	}
	authorizer := postgres.Authorizer{DB: a.runtime.db, ScopeLifetime: time.Minute}
	reader := postgres.PublicationTaskResourceReader{DB: a.runtime.db, Authorizer: authorizer}
	service := document.PublicationJobRetryService{Auth: a.runtime.auth, Reader: reader, Authorizer: authorizer, Retrier: postgres.PublicationRetry{DB: a.runtime.db, Authorizer: authorizer, ProfileDigest: digest}}
	return service.RetryJob(ctx, input)
}

func (s *Search) NewJobApplications() (JobApplications, error) {
	if s == nil || s.db == nil || s.auth == nil {
		return JobApplications{}, errors.New("job applications unavailable")
	}
	return JobApplications{Status: JobStatusApplication{runtime: s}, Cancel: JobCancellationApplication{runtime: s}, Retry: JobRetryApplication{runtime: s}}, nil
}

func (s *Search) NewParseJobApplications() (ParseJobApplications, error) {
	if s == nil || s.db == nil || s.auth == nil {
		return ParseJobApplications{}, errors.New("parse job applications unavailable")
	}
	return ParseJobApplications{Status: ParseJobStatusApplication{runtime: s}, Cancel: ParseJobCancellationApplication{runtime: s}, Retry: ParseJobRetryApplication{runtime: s}}, nil
}

func (a ParseJobStatusApplication) GetJob(ctx context.Context, input document.JobStatusInput) (contracts.ResourceJob, error) {
	if a.runtime == nil {
		return contracts.ResourceJob{}, document.ErrJobStatus
	}
	authorizer := postgres.Authorizer{DB: a.runtime.db, ScopeLifetime: time.Minute}
	reader := postgres.ParseTaskResourceReader{DB: a.runtime.db, Authorizer: authorizer}
	return (document.JobStatusService{Auth: a.runtime.auth, Reader: reader}).GetJob(ctx, input)
}

func (a ParseJobCancellationApplication) CancelJob(ctx context.Context, input document.JobCancellationInput) (contracts.ResourceJob, error) {
	if a.runtime == nil {
		return contracts.ResourceJob{}, document.ErrParseJob
	}
	authorizer := postgres.Authorizer{DB: a.runtime.db, ScopeLifetime: time.Minute}
	reader := postgres.ParseTaskResourceReader{DB: a.runtime.db, Authorizer: authorizer}
	service := document.ParseJobCancellationService{Auth: a.runtime.auth, Reader: reader, Authorizer: authorizer, Control: postgres.ParseTaskControl{DB: a.runtime.db}}
	return service.CancelJob(ctx, input)
}

// PauseJob implements the parser pause transport without widening the
// existing cancellation application surface used by older hosts.
func (a ParseJobCancellationApplication) PauseJob(ctx context.Context, input document.JobCancellationInput) (contracts.ResourceJob, error) {
	if a.runtime == nil {
		return contracts.ResourceJob{}, document.ErrJobPause
	}
	authorizer := postgres.Authorizer{DB: a.runtime.db, ScopeLifetime: time.Minute}
	reader := postgres.ParseTaskResourceReader{DB: a.runtime.db, Authorizer: authorizer}
	service := document.ParseJobPauseService{Auth: a.runtime.auth, Reader: reader, Authorizer: authorizer, Control: postgres.ParseTaskControl{DB: a.runtime.db}}
	return service.PauseJob(ctx, input)
}

func (a ParseJobRetryApplication) RetryJob(ctx context.Context, input document.JobRetryInput) (contracts.ResourceJob, error) {
	if a.runtime == nil {
		return contracts.ResourceJob{}, document.ErrParseJob
	}
	authorizer := postgres.Authorizer{DB: a.runtime.db, ScopeLifetime: time.Minute}
	reader := postgres.ParseTaskResourceReader{DB: a.runtime.db, Authorizer: authorizer}
	service := document.ParseJobRetryService{Auth: a.runtime.auth, Reader: reader, Authorizer: authorizer, Control: postgres.ParseTaskControl{DB: a.runtime.db}}
	return service.RetryJob(ctx, input)
}

func (a JobStatusApplication) GetJob(ctx context.Context, input document.JobStatusInput) (contracts.ResourceJob, error) {
	if a.runtime == nil {
		return contracts.ResourceJob{}, document.ErrJobStatus
	}
	authorizer := postgres.Authorizer{DB: a.runtime.db, ScopeLifetime: time.Minute}
	return (document.JobStatusService{Auth: a.runtime.auth, Reader: postgres.DeletionJobReader{DB: a.runtime.db, Authorizer: authorizer}}).GetJob(ctx, input)
}

func (a JobCancellationApplication) CancelJob(ctx context.Context, input document.JobCancellationInput) (contracts.ResourceJob, error) {
	if a.runtime == nil {
		return contracts.ResourceJob{}, document.ErrJobCancellation
	}
	service, err := a.runtime.jobCancellationService(ctx, input)
	if err != nil {
		return contracts.ResourceJob{}, err
	}
	return service.CancelJob(ctx, input)
}

func (a JobRetryApplication) RetryJob(ctx context.Context, input document.JobRetryInput) (contracts.ResourceJob, error) {
	if a.runtime == nil {
		return contracts.ResourceJob{}, document.ErrJobRetry
	}
	service, err := a.runtime.jobRetryService(ctx, input)
	if err != nil {
		if errors.Is(err, document.ErrJobCancellationUnauthenticated) {
			return contracts.ResourceJob{}, document.ErrJobRetryUnauthenticated
		}
		return contracts.ResourceJob{}, err
	}
	return service.RetryJob(ctx, input)
}

func (s *Search) jobAuthority(ctx context.Context, input document.JobCancellationInput) (contracts.Principal, postgres.DeletionCleanupJobs, error) {
	var empty postgres.DeletionCleanupJobs
	if ctx == nil || s == nil || s.db == nil || s.auth == nil {
		return contracts.Principal{}, empty, document.ErrJobCancellation
	}
	credential := contracts.Credential{Kind: input.Credential.Kind, Secret: append([]byte(nil), input.Credential.Secret...)}
	who, err := s.auth.Authenticate(ctx, credential)
	clear(credential.Secret)
	if err != nil {
		return contracts.Principal{}, empty, document.ErrJobCancellationUnauthenticated
	}
	authorizer := postgres.Authorizer{DB: s.db, ScopeLifetime: time.Minute}
	verify := func(c context.Context, before contracts.AccessScope) error {
		secret := contracts.Credential{Kind: input.Credential.Kind, Secret: append([]byte(nil), input.Credential.Secret...)}
		defer clear(secret.Secret)
		current, authErr := s.auth.Authenticate(c, secret)
		if authErr != nil {
			return document.ErrJobCancellationDenied
		}
		fresh, authErr := authorizer.Authorize(c, contracts.AuthorizeRequest{Principal: current, Action: before.Action, SpaceID: before.SpaceID, KnowledgeBaseIDs: append([]contracts.ID(nil), before.KnowledgeBaseIDs...)})
		if authErr != nil || current.SubjectID != who.SubjectID || current.SubjectType != who.SubjectType || current.Issuer != who.Issuer || current.ClientID != who.ClientID || fresh.Action != before.Action || fresh.SpaceID != before.SpaceID || fresh.SubjectID != before.SubjectID || fresh.PolicyVersion != before.PolicyVersion || !reflect.DeepEqual(fresh.KnowledgeBaseIDs, before.KnowledgeBaseIDs) || !fresh.ExpiresAt.After(time.Now()) {
			return document.ErrJobCancellationDenied
		}
		return nil
	}
	authorize := func(c context.Context, scope contracts.AccessScope, _ contracts.ResourceJob) error {
		return verify(c, scope)
	}
	return who, postgres.DeletionCleanupJobs{DB: s.db, Verify: verify, AuthorizeJob: authorize, AuthorizeControl: authorize}, nil
}

func (s *Search) jobCancellationService(ctx context.Context, input document.JobCancellationInput) (document.JobCancellationService, error) {
	_, control, err := s.jobAuthority(ctx, input)
	if err != nil {
		return document.JobCancellationService{}, err
	}
	authorizer := postgres.Authorizer{DB: s.db, ScopeLifetime: time.Minute}
	return document.JobCancellationService{Auth: s.auth, Reader: postgres.DeletionJobReader{DB: s.db, Authorizer: authorizer}, Authorizer: authorizer, Control: control}, nil
}

func (s *Search) jobRetryService(ctx context.Context, input document.JobRetryInput) (document.JobRetryService, error) {
	_, control, err := s.jobAuthority(ctx, input)
	if err != nil {
		return document.JobRetryService{}, err
	}
	authorizer := postgres.Authorizer{DB: s.db, ScopeLifetime: time.Minute}
	return document.JobRetryService{Auth: s.auth, Reader: postgres.DeletionJobReader{DB: s.db, Authorizer: authorizer}, Authorizer: authorizer, Control: control}, nil
}

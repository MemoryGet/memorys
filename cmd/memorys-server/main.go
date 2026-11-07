package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/api"
	"github.com/MemoryGet/memorys/pkg/bootstrap"
	"github.com/MemoryGet/memorys/pkg/localruntime"
	"github.com/MemoryGet/memorys/pkg/mcpgateway"
	"github.com/MemoryGet/memorys/pkg/modelclient"
	"github.com/MemoryGet/memorys/pkg/parseassembly"
	"github.com/MemoryGet/memorys/pkg/publicationassembly"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// Business routes require trusted application authorization/preparation hooks.
// Bootstrap does not invent identities or publish uploaded content.
func newMux() http.Handler { return newMuxWithSearch(nil) }

func newMuxWithSearch(search api.PartnerSearchApplication) http.Handler {
	return newMuxWithServicesAndReady(search, nil, nil)
}

func newMuxWithServices(search api.PartnerSearchApplication, mcpHandler http.Handler) http.Handler {
	return newMuxWithServicesAndReady(search, mcpHandler, nil)
}

func newMuxWithServicesAndReady(search api.PartnerSearchApplication, mcpHandler http.Handler, ready func(context.Context) error) http.Handler {
	return newMuxWithReadAndReady(search, nil, mcpHandler, ready)
}

func newMuxWithRead(search api.PartnerSearchApplication, read api.CurrentReadApplication, mcpHandler http.Handler) http.Handler {
	return newMuxWithReadAndReady(search, read, mcpHandler, nil)
}

func newMuxWithReadAndReady(search api.PartnerSearchApplication, read api.CurrentReadApplication, mcpHandler http.Handler, ready func(context.Context) error) http.Handler {
	return newMuxWithDeletionAndReady(search, read, mcpHandler, nil, search, ready)
}

func newMuxWithDeletion(search api.PartnerSearchApplication, read api.CurrentReadApplication, mcpHandler http.Handler, deletion api.DocumentDeletionApplication, deletionProfile api.ProfileIdentitySource) http.Handler {
	return newMuxWithDeletionAndReady(search, read, mcpHandler, deletion, deletionProfile, nil)
}

func newMuxWithDeletionAndReady(search api.PartnerSearchApplication, read api.CurrentReadApplication, mcpHandler http.Handler, deletion api.DocumentDeletionApplication, deletionProfile api.ProfileIdentitySource, ready func(context.Context) error) http.Handler {
	return newMuxWithJobStatusAndReady(search, read, mcpHandler, deletion, deletionProfile, nil, ready)
}

func newMuxWithJobStatus(search api.PartnerSearchApplication, read api.CurrentReadApplication, mcpHandler http.Handler, deletion api.DocumentDeletionApplication, deletionProfile api.ProfileIdentitySource, jobs api.JobStatusApplication) http.Handler {
	return newMuxWithJobStatusAndReady(search, read, mcpHandler, deletion, deletionProfile, jobs, nil)
}

func newMuxWithJobStatusAndReady(search api.PartnerSearchApplication, read api.CurrentReadApplication, mcpHandler http.Handler, deletion api.DocumentDeletionApplication, deletionProfile api.ProfileIdentitySource, jobs api.JobStatusApplication, ready func(context.Context) error) http.Handler {
	return newMuxWithJobControlAndReady(search, read, mcpHandler, deletion, deletionProfile, jobs, nil, ready)
}

func newMuxWithJobControl(search api.PartnerSearchApplication, read api.CurrentReadApplication, mcpHandler http.Handler, deletion api.DocumentDeletionApplication, deletionProfile api.ProfileIdentitySource, jobs api.JobStatusApplication, cancel api.JobCancellationApplication) http.Handler {
	return newMuxWithJobControlAndReady(search, read, mcpHandler, deletion, deletionProfile, jobs, cancel, nil)
}

func newMuxWithJobControlAndReady(search api.PartnerSearchApplication, read api.CurrentReadApplication, mcpHandler http.Handler, deletion api.DocumentDeletionApplication, deletionProfile api.ProfileIdentitySource, jobs api.JobStatusApplication, cancel api.JobCancellationApplication, ready func(context.Context) error) http.Handler {
	return newMuxWithJobControlAskAndReady(search, read, mcpHandler, deletion, deletionProfile, jobs, cancel, nil, ready)
}

func newMuxWithJobControlAskAndReady(search api.PartnerSearchApplication, read api.CurrentReadApplication, mcpHandler http.Handler, deletion api.DocumentDeletionApplication, deletionProfile api.ProfileIdentitySource, jobs api.JobStatusApplication, cancel api.JobCancellationApplication, ask api.PartnerAskApplication, ready func(context.Context) error) http.Handler {
	return newMuxWithJobControlAskFeedbackAndReady(search, read, mcpHandler, deletion, deletionProfile, jobs, cancel, nil, ask, nil, ready)
}

func newMuxWithJobControlAskFeedbackAndReady(search api.PartnerSearchApplication, read api.CurrentReadApplication, mcpHandler http.Handler, deletion api.DocumentDeletionApplication, deletionProfile api.ProfileIdentitySource, jobs api.JobStatusApplication, cancel api.JobCancellationApplication, retry api.JobRetryApplication, ask api.PartnerAskApplication, feedback api.PartnerFeedbackApplication, ready func(context.Context) error) http.Handler {
	return newMuxWithJobControlAskFeedbackParseAndReady(search, read, mcpHandler, deletion, deletionProfile, jobs, cancel, retry, nil, nil, nil, nil, nil, nil, ask, feedback, ready)
}

func newMuxWithJobControlAskFeedbackParseAndReady(search api.PartnerSearchApplication, read api.CurrentReadApplication, mcpHandler http.Handler, deletion api.DocumentDeletionApplication, deletionProfile api.ProfileIdentitySource, jobs api.JobStatusApplication, cancel api.JobCancellationApplication, retry api.JobRetryApplication, parseJobs api.JobStatusApplication, parseCancel api.JobCancellationApplication, parseRetry api.JobRetryApplication, publicationJobs api.JobStatusApplication, publicationCancel api.JobCancellationApplication, publicationRetry api.PublicationJobRetryApplication, ask api.PartnerAskApplication, feedback api.PartnerFeedbackApplication, ready func(context.Context) error) http.Handler {
	return newMuxWithJobControlAskFeedbackParseAndReviewAndReady(search, read, mcpHandler, deletion, deletionProfile, jobs, cancel, retry, parseJobs, parseCancel, parseRetry, publicationJobs, publicationCancel, publicationRetry, ask, feedback, nil, ready)
}

func newMuxWithJobControlAskFeedbackParseAndReviewAndReady(search api.PartnerSearchApplication, read api.CurrentReadApplication, mcpHandler http.Handler, deletion api.DocumentDeletionApplication, deletionProfile api.ProfileIdentitySource, jobs api.JobStatusApplication, cancel api.JobCancellationApplication, retry api.JobRetryApplication, parseJobs api.JobStatusApplication, parseCancel api.JobCancellationApplication, parseRetry api.JobRetryApplication, publicationJobs api.JobStatusApplication, publicationCancel api.JobCancellationApplication, publicationRetry api.PublicationJobRetryApplication, ask api.PartnerAskApplication, feedback api.PartnerFeedbackApplication, review api.ReviewDecisionApplication, ready func(context.Context) error) http.Handler {
	return newMuxWithJobControlAskFeedbackParseReviewApprovalAndReady(search, read, mcpHandler, deletion, deletionProfile, jobs, cancel, retry, parseJobs, parseCancel, parseRetry, publicationJobs, publicationCancel, publicationRetry, ask, feedback, review, nil, ready)
}

func newMuxWithJobControlAskFeedbackParseReviewApprovalAndReady(search api.PartnerSearchApplication, read api.CurrentReadApplication, mcpHandler http.Handler, deletion api.DocumentDeletionApplication, deletionProfile api.ProfileIdentitySource, jobs api.JobStatusApplication, cancel api.JobCancellationApplication, retry api.JobRetryApplication, parseJobs api.JobStatusApplication, parseCancel api.JobCancellationApplication, parseRetry api.JobRetryApplication, publicationJobs api.JobStatusApplication, publicationCancel api.JobCancellationApplication, publicationRetry api.PublicationJobRetryApplication, ask api.PartnerAskApplication, feedback api.PartnerFeedbackApplication, review api.ReviewDecisionApplication, approvals api.ApprovalRequestListApplication, ready func(context.Context) error) http.Handler {
	return newMuxWithJobControlAskFeedbackParseReviewApprovalDraftAndReady(search, read, mcpHandler, deletion, deletionProfile, jobs, cancel, retry, parseJobs, parseCancel, parseRetry, publicationJobs, publicationCancel, publicationRetry, ask, feedback, review, approvals, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, ready)
}

func newMuxWithJobControlAskFeedbackParseReviewApprovalDraftAndReady(search api.PartnerSearchApplication, read api.CurrentReadApplication, mcpHandler http.Handler, deletion api.DocumentDeletionApplication, deletionProfile api.ProfileIdentitySource, jobs api.JobStatusApplication, cancel api.JobCancellationApplication, retry api.JobRetryApplication, parseJobs api.JobStatusApplication, parseCancel api.JobCancellationApplication, parseRetry api.JobRetryApplication, publicationJobs api.JobStatusApplication, publicationCancel api.JobCancellationApplication, publicationRetry api.PublicationJobRetryApplication, ask api.PartnerAskApplication, feedback api.PartnerFeedbackApplication, review api.ReviewDecisionApplication, approvals api.ApprovalRequestListApplication, feedbackList api.FeedbackListApplication, revisions api.RevisionReadApplication, drafts api.DraftReadApplication, draftEdits api.DraftEditApplication, draftSubmissions api.DraftSubmitApplication, publicationStarts api.PublicationStartApplication, publicationWithdraws api.PublicationWithdrawApplication, history api.PublicationHistoryListApplication, documents api.DocumentListApplication, documentUploads api.DocumentUploadApplication, draftParses api.DraftParseApplication, draftCreates api.DraftCreateApplication, publicationOperations api.PublicationOperationReadApplication, draftSources api.DraftSourceApplication, knowledgeBases api.KnowledgeBaseListApplication, knowledgeBaseCreates api.KnowledgeBaseCreateApplication, draftCompare api.DraftCompareApplication, feedbackProposals api.FeedbackProposalApplication, ready func(context.Context) error) http.Handler {
	var grants api.GrantApplication
	if candidate, ok := feedbackList.(api.GrantApplication); ok {
		grants = candidate
	}
	var grantList api.GrantListApplication
	if candidate, ok := feedbackList.(api.GrantListApplication); ok {
		grantList = candidate
	}
	var jobList api.JobListApplication
	if candidate, ok := search.(api.JobListApplication); ok {
		jobList = candidate
	}
	var documentRead api.DocumentReadApplication
	if candidate, ok := documents.(api.DocumentReadApplication); ok {
		documentRead = candidate
	}
	var parsePause api.JobPauseApplication
	if candidate, ok := parseCancel.(api.JobPauseApplication); ok {
		parsePause = candidate
	}
	var publicationPause api.JobPauseApplication
	if candidate, ok := publicationCancel.(api.JobPauseApplication); ok {
		publicationPause = candidate
	}
	mux, err := bootstrap.New(bootstrap.Config{Search: search, Read: read, Ask: ask, Feedback: feedback, Review: review, ApprovalRequests: approvals, FeedbackList: feedbackList, FeedbackProposals: feedbackProposals, Grants: grants, GrantList: grantList, JobList: jobList, Revisions: revisions, Drafts: drafts, DraftEdits: draftEdits, DraftSubmissions: draftSubmissions, PublicationStarts: publicationStarts, PublicationWithdraws: publicationWithdraws, History: history, Documents: documents, DocumentRead: documentRead, DocumentUploads: documentUploads, DraftParses: draftParses, DraftCreates: draftCreates, PublicationOperations: publicationOperations, DraftSources: draftSources, KnowledgeBases: knowledgeBases, KnowledgeBaseCreates: knowledgeBaseCreates, DraftCompare: draftCompare, MCP: mcpHandler, Deletion: deletion, DeletionProfile: deletionProfile, Jobs: jobs, Cancel: cancel, Retry: retry, ParseJobs: parseJobs, ParseCancel: parseCancel, ParsePause: parsePause, ParseRetry: parseRetry, PublicationJobs: publicationJobs, PublicationCancel: publicationCancel, PublicationPause: publicationPause, PublicationRetry: publicationRetry, ReadyCheck: ready})
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "invalid bootstrap", http.StatusServiceUnavailable)
		})
	}
	return mux
}
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	settings, err := loadSearchSettings(os.LookupEnv)
	if err != nil {
		return err
	}
	var db *sql.DB
	var ready func(context.Context) error
	var search api.PartnerSearchApplication
	var read api.CurrentReadApplication
	var ask api.PartnerAskApplication
	var feedback api.PartnerFeedbackApplication
	var jobs api.JobStatusApplication
	var cancel api.JobCancellationApplication
	var retry api.JobRetryApplication
	var parseJobs api.JobStatusApplication
	var parseCancel api.JobCancellationApplication
	var parseRetry api.JobRetryApplication
	var publicationJobs api.JobStatusApplication
	var publicationCancel api.JobCancellationApplication
	var publicationRetry api.PublicationJobRetryApplication
	var review api.ReviewDecisionApplication
	var approvals api.ApprovalRequestListApplication
	var feedbackList api.FeedbackListApplication
	var feedbackProposals api.FeedbackProposalApplication
	var drafts api.DraftReadApplication
	var draftEdits api.DraftEditApplication
	var revisions api.RevisionReadApplication
	var draftSubmissions api.DraftSubmitApplication
	var publicationStarts api.PublicationStartApplication
	var publicationWithdraws api.PublicationWithdrawApplication
	var history api.PublicationHistoryListApplication
	var documents api.DocumentListApplication
	var documentUploads api.DocumentUploadApplication
	var draftParses api.DraftParseApplication
	var draftCreates api.DraftCreateApplication
	var publicationOperations api.PublicationOperationReadApplication
	var draftSources api.DraftSourceApplication
	var knowledgeBases api.KnowledgeBaseListApplication
	var knowledgeBaseCreates api.KnowledgeBaseCreateApplication
	var draftCompare api.DraftCompareApplication
	var mcpHandler http.Handler
	var parseRuntime interface {
		Start(context.Context) error
		Stop() error
	}
	var publicationRuntime interface {
		Start(context.Context) error
		Stop() error
	}
	if raw := strings.TrimSpace(os.Getenv("MEMORYS_DATABASE_URL")); raw != "" {
		db, err = sql.Open("pgx", raw)
		if err != nil {
			return errors.New("database configuration rejected")
		}
		defer db.Close()
		db.SetMaxOpenConns(10)
		db.SetMaxIdleConns(5)
		check, cancel := context.WithTimeout(ctx, 10*time.Second)
		err = db.PingContext(check)
		cancel()
		if err != nil {
			return errors.New("database unavailable")
		}
		ready = func(c context.Context) error {
			c, cancel := context.WithTimeout(c, 2*time.Second)
			defer cancel()
			return db.PingContext(c)
		}
		log.Println("memorys-server database connected")
	}
	if settings.Enabled {
		search, err = assembleSearch(db, settings)
		if err != nil {
			return err
		}
		if native, ok := search.(*localruntime.Search); ok {
			if root := strings.TrimSpace(os.Getenv("MEMORYS_BLOB_ROOT")); root != "" {
				native.WithLocalBlobRoot(root)
			}
			if runtime, runtimeErr := parseassembly.Build(ctx, native); runtimeErr != nil {
				return runtimeErr
			} else {
				parseRuntime = runtime
			}
			if runtime, runtimeErr := publicationassembly.Build(ctx, native); runtimeErr != nil {
				return runtimeErr
			} else {
				publicationRuntime = runtime
			}
			read = native
			reviewApp, reviewErr := native.NewReviewDecisionApplication()
			if reviewErr != nil {
				return errors.New("review service unavailable")
			}
			review = reviewApp
			approvals = native
			feedbackList = native
			feedbackProposals = native
			drafts = native
			draftEdits = native
			revisions = native
			draftSubmissions = native
			publicationStarts = native
			publicationWithdraws = native
			history = native
			documents = native
			documentUploads = native
			draftParses = native
			draftCreates = native
			publicationOperations = native
			draftSources = native
			knowledgeBases = native
			knowledgeBaseCreates = native
			draftCompare = native
			jobApps, jobErr := native.NewJobApplications()
			if jobErr != nil {
				return errors.New("job services unavailable")
			}
			jobs, cancel, retry = jobApps.Status, jobApps.Cancel, jobApps.Retry
			parseJobApps, parseJobErr := native.NewParseJobApplications()
			if parseJobErr != nil {
				return errors.New("parse job services unavailable")
			}
			parseJobs, parseCancel, parseRetry = parseJobApps.Status, parseJobApps.Cancel, parseJobApps.Retry
			publicationJobs, err = native.NewPublicationJobStatusApplication()
			if err != nil {
				return errors.New("publication job services unavailable")
			}
			publicationCancel, err = native.NewPublicationJobCancellationApplication()
			if err != nil {
				return errors.New("publication job control unavailable")
			}
			publicationRetry, err = native.NewPublicationJobRetryApplication()
			if err != nil {
				return errors.New("publication job retry unavailable")
			}
			if strings.TrimSpace(os.Getenv("MEMORYS_FEEDBACK_ENABLED")) == "1" {
				feedbackService, feedbackErr := native.NewFeedback()
				if feedbackErr != nil {
					return errors.New("feedback service unavailable")
				}
				feedback = api.KnowledgeFeedbackApplication{Service: feedbackService}
			}
			if key := strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY")); key != "" {
				model := strings.TrimSpace(os.Getenv("MEMORYS_CHAT_MODEL"))
				if model == "" {
					model = "deepseek-flash"
				}
				version, _, identityErr := native.ProfileIdentity()
				if identityErr != nil {
					return errors.New("answer profile unavailable")
				}
				generator, generatorErr := modelclient.NewDeepSeek(key, model, version)
				if generatorErr != nil {
					return errors.New("answer model configuration unavailable")
				}
				answer, answerErr := native.NewAnswer(generator)
				if answerErr != nil {
					return errors.New("answer service unavailable")
				}
				ask = api.KnowledgeAnswerApplication{Service: answer}
			}
			gateway, gatewayErr := mcpgateway.New(mcpgateway.Config{
				Auth:     native.Authenticator(),
				Search:   native,
				Read:     native,
				Ask:      ask,
				Feedback: feedback,
			})
			if gatewayErr != nil {
				return errors.New("MCP configuration unavailable")
			}
			mcpHandler = gateway
		}
		log.Println("memorys-server authenticated PostgreSQL keyword search configured (revision text only)")
	}
	if parseRuntime != nil {
		if err := parseRuntime.Start(ctx); err != nil {
			return errors.New("parse worker unavailable")
		}
		defer parseRuntime.Stop()
	}
	if publicationRuntime != nil {
		if err := publicationRuntime.Start(ctx); err != nil {
			if parseRuntime != nil {
				_ = parseRuntime.Stop()
			}
			return errors.New("publication worker unavailable")
		}
		defer publicationRuntime.Stop()
	}
	server := &http.Server{Addr: "127.0.0.1:8080", Handler: newMuxWithJobControlAskFeedbackParseReviewApprovalDraftAndReady(search, read, mcpHandler, nil, nil, jobs, cancel, retry, parseJobs, parseCancel, parseRetry, publicationJobs, publicationCancel, publicationRetry, ask, feedback, review, approvals, feedbackList, revisions, drafts, draftEdits, draftSubmissions, publicationStarts, publicationWithdraws, history, documents, documentUploads, draftParses, draftCreates, publicationOperations, draftSources, knowledgeBases, knowledgeBaseCreates, draftCompare, feedbackProposals, ready), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	stopped := make(chan struct{})
	shutdownDone := make(chan struct{})
	defer close(stopped)
	go func() {
		defer close(shutdownDone)
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if server.Shutdown(shutdown) != nil {
				_ = server.Close()
			}
		case <-stopped:
		}
	}()
	log.Println("memorys-server listening on 127.0.0.1:8080")
	err = server.ListenAndServe()
	if ctx.Err() != nil {
		<-shutdownDone // Drain handlers before the deferred database pool close.
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return errors.New("HTTP listener unavailable")
	}
	return nil
}

func assembleSearch(db *sql.DB, settings searchSettings) (*localruntime.Search, error) {
	return localruntime.NewSearchFromFiles(db, settings.Issuer, settings.Audience, settings.SubjectType, settings.JWKSFile, settings.ProfileFile, settings.MaxTokenLifetime)
}

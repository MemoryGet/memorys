package localruntime

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/postgres"
)

type jobListCursor struct {
	Subject       string `json:"subject"`
	After         string `json:"after"`
	Kind          string `json:"kind,omitempty"`
	State         string `json:"state,omitempty"`
	DocumentID    string `json:"document_id,omitempty"`
	CreatedAfter  string `json:"created_after,omitempty"`
	CreatedBefore string `json:"created_before,omitempty"`
}

func encodeJobListCursor(value jobListCursor) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 4096 || value.Subject == "" || value.After == "" {
		return "", errors.New("invalid job cursor")
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeJobListCursor(raw, subject string, input contracts.JobListInput) (string, error) {
	if raw == "" || len(raw) > 10000 {
		return "", errors.New("invalid job cursor")
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(data) > 4096 {
		return "", errors.New("invalid job cursor")
	}
	var value jobListCursor
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || value.Subject != subject || value.After == "" || len(value.After) > 200 || value.Kind != input.Kind || value.State != input.State || value.DocumentID != string(input.DocumentID) || value.CreatedAfter != jobTimeText(input.CreatedAfter) || value.CreatedBefore != jobTimeText(input.CreatedBefore) {
		return "", errors.New("invalid job cursor")
	}
	return value.After, nil
}

func jobTimeText(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func jobStateValid(state string) bool {
	switch state {
	case "queued", "running", "paused", "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func (s *Search) ListJobs(ctx context.Context, input contracts.JobListInput) (contracts.JobPage, error) {
	var empty contracts.JobPage
	if s == nil || s.db == nil || s.auth == nil || ctx == nil || input.PageSize < 1 || input.PageSize > 100 || input.Request.Deadline.IsZero() || input.Request.ProfileDigest == "" || input.Credential.Kind != "access_token" || len(input.Credential.Secret) == 0 || input.Kind != "" && input.Kind != "parse" && input.Kind != "publication" && input.Kind != "deletion" || input.State != "" && !jobStateValid(input.State) || !input.CreatedAfter.IsZero() && !input.CreatedBefore.IsZero() && !input.CreatedAfter.Before(input.CreatedBefore) {
		return empty, errors.New("jobs unavailable")
	}
	ctx, cancel := context.WithDeadline(ctx, input.Request.Deadline)
	defer cancel()
	secret := contracts.Credential{Kind: input.Credential.Kind, Secret: append([]byte(nil), input.Credential.Secret...)}
	defer clear(secret.Secret)
	who, err := s.auth.Authenticate(ctx, secret)
	if err != nil || ctx.Err() != nil {
		return empty, errors.New("jobs unauthenticated")
	}
	version, digest, err := s.ProfileIdentity()
	if err != nil || version == 0 || digest != input.Request.ProfileDigest {
		return empty, errors.New("jobs profile changed")
	}
	authorizer := postgres.Authorizer{DB: s.db, ScopeLifetime: time.Minute}
	after := ""
	if input.Cursor != "" {
		after, err = decodeJobListCursor(input.Cursor, string(who.SubjectID), input)
		if err != nil {
			return empty, err
		}
	}
	items := make([]contracts.JobListItem, 0, 300)
	if input.Kind == "" || input.Kind == "parse" {
		if err := s.listParseJobs(ctx, authorizer, who, input, after, &items); err != nil {
			return empty, err
		}
	}
	if input.Kind == "" || input.Kind == "publication" {
		if err := s.listPublicationJobs(ctx, authorizer, who, input, after, &items); err != nil {
			return empty, err
		}
	}
	if input.Kind == "" || input.Kind == "deletion" {
		if err := s.listDeletionJobs(ctx, authorizer, who, input, after, &items); err != nil {
			return empty, err
		}
	}
	if ctx.Err() != nil {
		return empty, errors.New("jobs deadline exceeded")
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Job.ID > items[j].Job.ID })
	if len(items) > input.PageSize {
		items = items[:input.PageSize]
		next, cursorErr := encodeJobListCursor(jobListCursor{Subject: string(who.SubjectID), After: string(items[len(items)-1].Job.ID), Kind: input.Kind, State: input.State, DocumentID: string(input.DocumentID), CreatedAfter: jobTimeText(input.CreatedAfter), CreatedBefore: jobTimeText(input.CreatedBefore)})
		if cursorErr != nil {
			return empty, errors.New("jobs cursor unavailable")
		}
		return contracts.JobPage{Items: items, NextCursor: next}, nil
	}
	return contracts.JobPage{Items: items}, nil
}

func (s *Search) admitJob(ctx context.Context, authorizer postgres.Authorizer, who contracts.Principal, kind string, input contracts.JobListInput, after string, space, kb, document string, job contracts.Job) (contracts.JobListItem, bool, error) {
	if job.ID == "" || job.SpaceID != contracts.ID(space) || !jobStateValid(job.State) || job.RowVersion == 0 || job.Stage == "" || document == "" || after != "" && string(job.ID) >= after || input.State != "" && job.State != input.State || input.DocumentID != "" && contracts.ID(document) != input.DocumentID {
		return contracts.JobListItem{}, false, nil
	}
	scope, err := authorizer.Authorize(ctx, contracts.AuthorizeRequest{Principal: who, Action: "resource.view_job", SpaceID: contracts.ID(space), KnowledgeBaseIDs: []contracts.ID{contracts.ID(kb)}, ResourceIDs: []contracts.ID{contracts.ID(document)}})
	if err != nil || scope.PolicyVersion == 0 {
		return contracts.JobListItem{}, false, nil
	}
	return contracts.JobListItem{Kind: kind, ResourceJob: contracts.ResourceJob{ResourceID: contracts.ID(document), KnowledgeBaseID: contracts.ID(kb), Job: job}}, true, nil
}

func (s *Search) listParseJobs(ctx context.Context, authorizer postgres.Authorizer, who contracts.Principal, input contracts.JobListInput, after string, out *[]contracts.JobListItem) error {
	query := `SELECT space_id,kb_id,id,payload,created_at FROM akos_withdraw_v1.parse_task_heads WHERE 1=1`
	args := []any{}
	if !input.CreatedAfter.IsZero() {
		args = append(args, input.CreatedAfter)
		query += ` AND created_at >= $` + strconv.Itoa(len(args))
	}
	if !input.CreatedBefore.IsZero() {
		args = append(args, input.CreatedBefore)
		query += ` AND created_at < $` + strconv.Itoa(len(args))
	}
	query += ` ORDER BY id DESC LIMIT 1001`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return errors.New("parse jobs unavailable")
	}
	defer rows.Close()
	for rows.Next() {
		var space, kb, id string
		var raw []byte
		if rows.Scan(&space, &kb, &id, &raw, new(time.Time)) != nil || len(raw) == 0 || len(raw) > 262144 {
			return errors.New("parse job unavailable")
		}
		var envelope struct {
			Job     contracts.Job `json:"Job"`
			Command struct {
				DocumentID contracts.ID `json:"DocumentID"`
			} `json:"Command"`
		}
		if json.Unmarshal(raw, &envelope) != nil || envelope.Job.ID != contracts.ID(id) {
			continue
		}
		item, ok, err := s.admitJob(ctx, authorizer, who, "parse", input, after, space, kb, string(envelope.Command.DocumentID), envelope.Job)
		if err != nil {
			return err
		}
		if ok {
			*out = append(*out, item)
		}
	}
	return rows.Err()
}

func (s *Search) listPublicationJobs(ctx context.Context, authorizer postgres.Authorizer, who contracts.Principal, input contracts.JobListInput, after string, out *[]contracts.JobListItem) error {
	query := `SELECT h.space_id,h.kb_id,h.id,h.payload,i.document_id,a.occurred_at FROM akos_withdraw_v1.publication_task_heads h JOIN akos_withdraw_v1.publication_intents i ON i.space_id=h.space_id AND i.kb_id=h.kb_id AND i.job_id=h.id JOIN akos_withdraw_v1.publication_start_audit a ON a.space_id=i.space_id AND a.kb_id=i.kb_id AND a.operation_id=i.id WHERE 1=1`
	args := []any{}
	if !input.CreatedAfter.IsZero() {
		args = append(args, input.CreatedAfter)
		query += ` AND a.occurred_at >= $` + strconv.Itoa(len(args))
	}
	if !input.CreatedBefore.IsZero() {
		args = append(args, input.CreatedBefore)
		query += ` AND a.occurred_at < $` + strconv.Itoa(len(args))
	}
	query += ` ORDER BY h.id DESC LIMIT 1001`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return errors.New("publication jobs unavailable")
	}
	defer rows.Close()
	for rows.Next() {
		var space, kb, id, document string
		var raw []byte
		if rows.Scan(&space, &kb, &id, &raw, &document, new(time.Time)) != nil || len(raw) == 0 || len(raw) > 32768 {
			return errors.New("publication job unavailable")
		}
		var job contracts.Job
		if json.Unmarshal(raw, &job) != nil || job.ID != contracts.ID(id) {
			continue
		}
		item, ok, err := s.admitJob(ctx, authorizer, who, "publication", input, after, space, kb, document, job)
		if err != nil {
			return err
		}
		if ok {
			*out = append(*out, item)
		}
	}
	return rows.Err()
}

func (s *Search) listDeletionJobs(ctx context.Context, authorizer postgres.Authorizer, who contracts.Principal, input contracts.JobListInput, after string, out *[]contracts.JobListItem) error {
	query := `SELECT j.space_id,j.kb_id,j.id,j.document_id,j.job,p.state,p.attempt::text,p.row_version::text,p.completed_targets::text,p.total_targets::text,p.error_request_id,h.occurred_at FROM akos_withdraw_v1.document_delete_jobs j JOIN akos_withdraw_v1.document_delete_job_progress p ON p.job_id=j.id JOIN akos_withdraw_v1.publication_history h ON h.space_id=j.space_id AND h.kb_id=j.kb_id AND h.document_id=j.document_id AND h.id=j.history_id WHERE 1=1`
	args := []any{}
	if !input.CreatedAfter.IsZero() {
		args = append(args, input.CreatedAfter)
		query += ` AND h.occurred_at >= $` + strconv.Itoa(len(args))
	}
	if !input.CreatedBefore.IsZero() {
		args = append(args, input.CreatedBefore)
		query += ` AND h.occurred_at < $` + strconv.Itoa(len(args))
	}
	query += ` ORDER BY j.id DESC LIMIT 1001`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return errors.New("deletion jobs unavailable")
	}
	defer rows.Close()
	for rows.Next() {
		var space, kb, id, document, state, attempt, rowVersion, completed, total string
		var raw []byte
		var errorRequest sql.NullString
		if rows.Scan(&space, &kb, &id, &document, &raw, &state, &attempt, &rowVersion, &completed, &total, &errorRequest, new(time.Time)) != nil || len(raw) == 0 || len(raw) > 65536 {
			return errors.New("deletion job unavailable")
		}
		var job contracts.Job
		if json.Unmarshal(raw, &job) != nil || job.ID != contracts.ID(id) {
			continue
		}
		job.State = state
		job.Attempt, _ = parseUint(attempt)
		job.RowVersion, _ = parseUint(rowVersion)
		complete, _ := parseUint(completed)
		all, _ := parseUint(total)
		if all > 0 {
			job.Progress = float64(complete) / float64(all)
		} else if state == "completed" {
			job.Progress = 1
		}
		if state == "failed" && errorRequest.Valid {
			job.Error = &contracts.PublicError{Code: "dependency_unavailable", SafeMessage: "Cleanup requires reconciliation", RequestID: contracts.ID(errorRequest.String)}
		}
		if state == "cancelled" && errorRequest.Valid {
			job.Error = &contracts.PublicError{Code: "conflict", SafeMessage: "Cleanup canceled", RequestID: contracts.ID(errorRequest.String)}
		}
		item, ok, err := s.admitJob(ctx, authorizer, who, "deletion", input, after, space, kb, document, job)
		if err != nil {
			return err
		}
		if ok {
			*out = append(*out, item)
		}
	}
	return rows.Err()
}

func parseUint(raw string) (uint64, error) {
	var value uint64
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0, errors.New("invalid integer")
		}
		value = value*10 + uint64(r-'0')
	}
	return value, nil
}

var _ contracts.JobListApplication = (*Search)(nil)

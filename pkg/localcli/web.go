package localcli

import (
	"bytes"
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/document"
	"github.com/MemoryGet/memorys/pkg/knowledge"
)

// The explicit web command is a local owner control plane. The normal serve
// command continues to hold public keys only and cannot approve documents.
type webHost struct {
	db              *sql.DB
	s               *state
	address, cookie string
	expires         time.Time
	mutations       sync.Mutex
	session         sync.Mutex
	slots           chan struct{}
	metrics         webMetrics
	parseMu         sync.Mutex
	parseTasks      map[contracts.ID]parseTask
	parseCancels    map[contracts.ID]context.CancelFunc
}

var assetName = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9_.-]*\.(js|css|svg|png|woff2)$`)

func newWebHandler(db *sql.DB, s *state, address, directory string) (http.Handler, error) {
	if s == nil || s.key == nil || !s.m.Ready {
		return nil, errState
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" {
		return nil, errState
	}
	directory, err = filepath.Abs(directory)
	if err != nil {
		return nil, errState
	}
	index, err := os.ReadFile(filepath.Join(directory, "index.html"))
	if err != nil || len(index) > 1<<20 {
		return nil, errors.New("build dashboard first: npm --prefix dashboard run build; or set --ui-dir")
	}
	base, err := handler(db, s)
	if err != nil {
		return nil, err
	}
	parseTasks, err := loadParseTasks(s.root)
	if err != nil {
		return nil, err
	}
	if err := persistParseTasks(s.root, parseTasks); err != nil {
		return nil, err
	}
	h := &webHost{db: db, s: s, address: address, cookie: nonce() + nonce(), expires: time.Now().Add(2 * time.Hour), slots: make(chan struct{}, 4), parseTasks: parseTasks, parseCancels: map[contracts.ID]context.CancelFunc{}}
	mux := http.NewServeMux()
	mux.Handle("/", base)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/local/", http.StatusSeeOther) })
	mux.HandleFunc("GET /local/{$}", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" || r.Header.Get("Origin") != "" && r.Header.Get("Origin") != "http://"+address {
			http.Error(w, "open the local address directly", 403)
			return
		}
		h.session.Lock()
		if !h.expires.After(time.Now()) {
			h.cookie = nonce() + nonce()
			h.expires = time.Now().Add(2 * time.Hour)
		}
		value := h.cookie
		h.session.Unlock()
		http.SetCookie(w, &http.Cookie{Name: "memorys_local", Value: value, Path: "/local/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 7200})
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	})
	mux.HandleFunc("GET /local/assets/{file}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("file")
		if !assetName.MatchString(name) {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(directory, "assets", name))
	})
	for path, action := range map[string]func(context.Context, http.ResponseWriter, *http.Request, *authority) error{
		"GET /local/api/status":                 h.status,
		"GET /local/api/metrics":                h.metricsList,
		"GET /local/api/metrics/prometheus":     h.metricsPrometheus,
		"GET /local/api/documents":              h.documents,
		"GET /local/api/tags":                   h.tagsList,
		"GET /local/api/jobs/{draft}":           h.jobStatus,
		"GET /local/api/jobs":                   h.jobsList,
		"GET /local/api/jobs/events":            h.jobEvents,
		"GET /local/api/parse-jobs/{id}":        h.parseJobStatus,
		"GET /local/api/feedback":               h.feedbackList,
		"GET /local/api/feedback/export":        h.exportFeedback,
		"GET /local/api/evaluation":             h.evaluationReport,
		"GET /local/api/evaluation-sets":        h.evaluationSets,
		"GET /local/api/evaluation-sets/{id}":   h.evaluationSet,
		"GET /local/api/audit":                  h.auditList,
		"GET /local/api/delete-jobs/{document}": h.deleteJobStatus,
		"GET /local/api/cleanup/{document}":     h.cleanupStatus,
		"POST /local/api/delete-jobs/retry":     h.retryDeleteJob,
		"POST /local/api/delete-jobs/cancel":    h.cancelDeleteJob,
		"POST /local/api/import":                h.importDocument,
		"POST /local/api/import/batch":          h.importBatchDocument,
		"POST /local/api/parse-jobs/retry":      h.retryParseJob,
		"POST /local/api/parse-jobs/cancel":     h.cancelParseJob,
		"POST /local/api/parse-jobs/pause":      h.pauseParseJob,
		"POST /local/api/edit":                  h.editDocument,
		"POST /local/api/tags":                  h.setTags,
		"POST /local/api/tags/batch":            h.batchTags,
		"POST /local/api/submit":                h.submitDocument,
		"POST /local/api/review":                h.reviewDocument,
		"POST /local/api/compare":               h.compareDocument,
		"POST /local/api/withdraw":              h.withdrawDocument,
		"POST /local/api/delete":                h.deleteDocument,
		"POST /local/api/retry":                 h.retryDocument,
		"POST /local/api/cancel":                h.cancelPublication,
		"POST /local/api/pause":                 h.cancelPublication,
		"POST /local/api/preview":               h.preview,
		"GET /local/api/export/{draft}":         h.exportDocument,
		"POST /local/api/publish":               h.publishDocument,
		"POST /local/api/search":                h.search,
		"POST /local/api/search/stream":         h.searchStream,
		"POST /local/api/feedback":              h.feedback,
		"POST /local/api/feedback/resolve":      h.resolveFeedback,
		"POST /local/api/feedback/proposal":     h.createFeedbackProposal,
		"POST /local/api/evaluation-sets":       h.createEvaluationSet,
	} {
		mux.HandleFunc(path, h.authorized(action))
	}
	mux.HandleFunc("GET /local/api/original/{draft}", h.authorized(h.original))
	mux.HandleFunc("GET /local/api/original/{document}/{revision}/{blob}", h.authorized(h.originalCitation))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		if r.Host != address {
			http.Error(w, "local host required", 403)
			return
		}
		mux.ServeHTTP(w, r)
	}), nil
}

func (h *webHost) authorized(action func(context.Context, http.ResponseWriter, *http.Request, *authority) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("memorys_local")
		h.session.Lock()
		valid := err == nil && subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(h.cookie)) == 1 && h.expires.After(time.Now())
		h.session.Unlock()
		if !valid || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			webError(w, 403, "会话失效，请重新打开本地工作台。")
			return
		}
		origin := r.Header.Get("Origin")
		if origin != "" && origin != "http://"+h.address || r.Method == http.MethodPost && (origin != "http://"+h.address || r.Header.Get("X-Memorys-Local") != "1") {
			webError(w, 403, "仅接受本地工作台发起的操作。")
			return
		}
		select {
		case h.slots <- struct{}{}:
			defer func() { <-h.slots }()
		default:
			webError(w, 429, "操作较多，请稍后重试。")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()
		if h.s.validateDB(ctx, h.db) != nil {
			webError(w, 503, "本地配置或数据库状态不一致，请运行 doctor。")
			return
		}
		a, err := h.s.identity(ctx, h.db, false)
		if err != nil {
			webError(w, 403, "当前身份不可用，请检查本地权限。")
			return
		}
		defer a.close()
		started := time.Now()
		actionErr := action(ctx, w, r, a)
		h.metrics.observe(r.URL.Path, actionErr, time.Since(started))
		if actionErr != nil {
			webError(w, 400, actionErr.Error())
		}
	}
}
func webError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = output(w, map[string]string{"error": message})
}
func webJSON(w http.ResponseWriter, value any) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	return output(w, value)
}
func readWebJSON(w http.ResponseWriter, r *http.Request, target any) error {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return errors.New("请求须使用 JSON。")
	}
	// JSON control requests are small, but an edited draft may contain the
	// complete local text body (up to maxTextBytes).
	r.Body = http.MaxBytesReader(w, r.Body, maxTextBytes+64<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(target) != nil || d.Decode(new(any)) != io.EOF {
		return errors.New("请求内容不完整或包含未知字段。")
	}
	return nil
}
func (h *webHost) status(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	if _, err := a.scope(ctx, "knowledge.search", ""); err != nil {
		return errAuthority
	}
	// doctor is read-only. Its output never includes keys, tokens or DB URLs.
	var buffer bytes.Buffer
	if doctor(ctx, h.s.root, &buffer) != nil {
		return errDatabase
	}
	var payload map[string]any
	if json.Unmarshal(buffer.Bytes(), &payload) != nil {
		return errDatabase
	}
	h.session.Lock()
	expires := h.expires.UTC().Format(time.RFC3339)
	h.session.Unlock()
	if checks, ok := payload["checks"].(map[string]any); ok {
		checks["session_expires_at"] = expires
	}
	return webJSON(w, payload)
}

type webDocument struct {
	DocumentID  contracts.ID     `json:"document_id"`
	DraftID     contracts.ID     `json:"draft_id"`
	Hash        contracts.Digest `json:"content_hash"`
	Preview     string           `json:"preview"`
	State       string           `json:"state"`
	ReviewState string           `json:"review_state"`
	Format      string           `json:"format"`
	Tags        []string         `json:"tags,omitempty"`
}

type documentCursor struct {
	Space string   `json:"space"`
	KB    string   `json:"kb"`
	After string   `json:"after"`
	Tags  []string `json:"tags,omitempty"`
}

func encodeDocumentCursor(value documentCursor) (string, error) {
	if value.Space == "" || value.KB == "" || value.After == "" || len(value.After) > 200 {
		return "", errDatabase
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 1024 {
		return "", errDatabase
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeDocumentCursor(raw string, space, kb contracts.ID) (string, error) {
	value, err := decodeDocumentCursorValue(raw, space, kb)
	if err != nil {
		return "", err
	}
	return value.After, nil
}

func decodeDocumentCursorValue(raw string, space, kb contracts.ID) (documentCursor, error) {
	if raw == "" || len(raw) > 2048 {
		return documentCursor{}, errors.New("文档分页游标无效，请重新加载列表。")
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return documentCursor{}, errors.New("文档分页游标无效，请重新加载列表。")
	}
	var value documentCursor
	if json.Unmarshal(data, &value) != nil || value.Space != string(space) || value.KB != string(kb) || value.After == "" || len(value.After) > 200 {
		return documentCursor{}, errors.New("文档分页游标无效，请重新加载列表。")
	}
	canonical, err := normalizeTags(value.Tags)
	if err != nil || !sameDocumentTags(canonical, value.Tags) {
		return documentCursor{}, errors.New("文档分页游标无效，请重新加载列表。")
	}
	return value, nil
}

func sameDocumentTags(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func documentTagFilter(r *http.Request) ([]string, error) {
	values := append([]string(nil), r.URL.Query()["tag"]...)
	values = append(values, r.URL.Query()["tags"]...)
	if len(values) == 0 {
		return nil, nil
	}
	raw := make([]string, 0, len(values))
	for _, value := range values {
		raw = append(raw, strings.Split(value, ",")...)
	}
	tags, err := normalizeTags(raw)
	if err != nil {
		return nil, errors.New("文档标签筛选无效：" + err.Error())
	}
	return tags, nil
}

func (h *webHost) documents(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	scope, err := a.scope(ctx, "document.create", "")
	if err != nil {
		return errAuthority
	}
	tags, err := documentTagFilter(r)
	if err != nil {
		return err
	}
	after := ""
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		cursor, cursorErr := decodeDocumentCursorValue(raw, h.s.m.Space, h.s.m.KB)
		if cursorErr != nil {
			return cursorErr
		}
		if !sameDocumentTags(cursor.Tags, tags) {
			return errors.New("文档分页游标与标签筛选不匹配，请重新加载列表。")
		}
		after = cursor.After
		if err != nil {
			return err
		}
	}
	type head struct {
		id        contracts.ID
		state     string
		review    string
		published bool
	}
	heads := []head{}
	for len(heads) < 101 {
		query := `SELECT dh.id,dh.state,COALESCE((SELECT ar.state FROM akos_withdraw_v1.approval_requests ar WHERE ar.space_id=d.space_id AND ar.kb_id=d.kb_id AND ar.document_id=d.id AND ar.revision_id=(SELECT r.revision_id FROM akos_withdraw_v1.submission_receipts r WHERE r.space_id=d.space_id AND r.kb_id=d.kb_id AND r.document_id=d.id AND r.draft_id=dh.id) ORDER BY ar.row_version DESC LIMIT 1),'draft'),EXISTS(SELECT 1 FROM akos_withdraw_v1.approval_requests ar WHERE ar.document_id=d.id AND ar.revision_id=d.active_revision_id AND ar.state='approved') FROM akos_withdraw_v1.draft_heads dh JOIN akos_withdraw_v1.documents d ON d.id=dh.document_id AND d.space_id=dh.space_id AND d.kb_id=dh.kb_id WHERE dh.space_id=$1 AND dh.kb_id=$2 AND NOT d.deleted`
		args := []any{h.s.m.Space, h.s.m.KB}
		if after != "" {
			query += ` AND dh.id > $3`
			args = append(args, after)
		}
		query += ` ORDER BY dh.id LIMIT 101`
		rows, queryErr := h.db.QueryContext(ctx, query, args...)
		if queryErr != nil {
			return errDatabase
		}
		read := 0
		for rows.Next() {
			read++
			var item head
			if rows.Scan(&item.id, &item.state, &item.review, &item.published) != nil {
				rows.Close()
				return errDatabase
			}
			after = string(item.id)
			if tagsMatch(h.tagsFor(item.id), tags) {
				heads = append(heads, item)
				if len(heads) == 101 {
					break
				}
			}
		}
		queryErr = rows.Err()
		rows.Close()
		if queryErr != nil {
			return errDatabase
		}
		if read < 101 {
			break
		}
		if len(heads) == 101 {
			break
		}
	}
	// `after` is the scan position, while the next cursor must point at the
	// final returned match so that a filtered page cannot skip an item.
	if len(heads) > 0 {
		after = string(heads[len(heads)-1].id)
	}
	if len(heads) == 0 {
		after = ""
	}
	more := len(heads) > 100
	if more {
		heads = heads[:100]
	}
	items := []webDocument{}
	for _, head := range heads {
		d, err := h.s.loadDraft(ctx, h.db, head.id)
		if err != nil {
			return err
		}
		preview, err := resultFor(d, false)
		if err != nil {
			return err
		}
		state := head.state
		if head.published {
			state = "published"
		}
		format := "text"
		if len(d.Blobs) > 0 {
			for name, media := range mediaFormats {
				if d.Blobs[0].MediaType == media {
					format = name
				}
			}
		}
		items = append(items, webDocument{DocumentID: d.DocumentID, DraftID: d.ID, Hash: preview.ContentHash, Preview: preview.Preview, State: state, ReviewState: head.review, Format: format, Tags: h.tagsFor(d.DocumentID)})
	}
	if a.verify(ctx, scope) != nil {
		return errAuthority
	}
	next := ""
	if more && len(heads) > 0 {
		next, err = encodeDocumentCursor(documentCursor{Space: string(h.s.m.Space), KB: string(h.s.m.KB), After: string(heads[len(heads)-1].id), Tags: tags})
		if err != nil {
			return errDatabase
		}
	}
	return webJSON(w, map[string]any{"documents": items, "has_more": more, "next_cursor": next})
}
func (h *webHost) importDocument(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	if _, err := a.scope(ctx, "document.create", ""); err != nil {
		return errAuthority
	}
	r.Body = http.MaxBytesReader(w, r.Body, (8<<20)+(64<<10))
	reader, err := r.MultipartReader()
	if err != nil {
		return errors.New("请选择要导入的文件。")
	}
	part, err := reader.NextPart()
	if err != nil {
		return errImport
	}
	defer part.Close()
	if _, err = reader.NextPart(); err != io.EOF {
		return errors.New("每次请选择一个文件。")
	}
	current, processErr := h.importMultipartPart(ctx, a, part)
	if processErr != nil {
		return processErr
	}
	if current.State != "completed" || current.Result == nil {
		if current.Error != "" {
			return errors.New(current.Error)
		}
		return errImport
	}
	return webJSON(w, importResponse(current))
}

// importMultipartPart persists one uploaded part before starting the parser.
// The durable task remains available for retry if parsing or persistence fails.
func (h *webHost) importMultipartPart(ctx context.Context, a *authority, part *multipart.Part) (parseTask, error) {
	if part == nil || part.FormName() != "file" || part.FileName() == "" {
		return parseTask{}, errImport
	}
	ext := strings.ToLower(filepath.Ext(part.FileName()))
	if ext != ".txt" && ext != ".md" && ext != ".markdown" && !supportedOriginalFormat(strings.TrimPrefix(ext, ".")) {
		return parseTask{}, errors.New("支持 TXT、Markdown、PDF、DOCX、XLSX、PPTX、PNG、JPG、WAV、MP3、M4A。")
	}
	f, err := os.CreateTemp(h.s.root, ".web-upload-*"+ext)
	if err != nil {
		return parseTask{}, errImport
	}
	tempPath := f.Name()
	defer os.Remove(tempPath)
	if f.Chmod(0600) != nil || protect(tempPath, false) != nil {
		_ = f.Close()
		return parseTask{}, errImport
	}
	n, err := io.Copy(f, io.LimitReader(part, (8<<20)+1))
	if err != nil || n == 0 || n > 8<<20 || f.Close() != nil {
		_ = f.Close()
		return parseTask{}, errImport
	}
	task, err := h.createParseTask(ext)
	if err != nil {
		return parseTask{}, errDatabase
	}
	if os.Rename(tempPath, task.InputPath) != nil {
		h.parseMu.Lock()
		task.State, task.Error, task.UpdatedAt = "failed", "解析输入保存失败，可重试。", time.Now().UTC()
		task.RowVersion++
		h.parseTasks[task.ID] = task
		_ = persistParseTasks(h.s.root, h.parseTasks)
		h.parseMu.Unlock()
		return task, errImport
	}
	done := h.executeParseTask(ctx, a, task.ID)
	<-done
	current, ok := h.parseTaskByID(task.ID)
	if !ok {
		return parseTask{}, errDatabase
	}
	if current.State != "completed" || current.Result == nil {
		if current.Error != "" {
			return current, errors.New(current.Error)
		}
		return current, errImport
	}
	return current, nil
}

func importResponse(current parseTask) map[string]any {
	result := map[string]any{"task_id": current.ID, "state": current.State, "task": publicParseTask(current)}
	if current.Result != nil {
		result["document_id"] = current.Result.DocumentID
		result["draft_id"] = current.Result.DraftID
		result["content_hash"] = current.Result.ContentHash
		result["preview"] = current.Result.Preview
		result["bytes"] = current.Result.Bytes
		result["reused"] = current.Result.Reused
	}
	return result
}

func (h *webHost) importBatchDocument(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	if _, err := a.scope(ctx, "document.create", ""); err != nil {
		return errAuthority
	}
	r.Body = http.MaxBytesReader(w, r.Body, (64<<20)+(64<<10))
	reader, err := r.MultipartReader()
	if err != nil {
		return errors.New("请选择要导入的文件。")
	}
	type item struct {
		Filename string          `json:"filename"`
		Task     parseTaskPublic `json:"task,omitempty"`
		Error    string          `json:"error,omitempty"`
		Result   *importResult   `json:"result,omitempty"`
	}
	items := make([]item, 0, 20)
	for len(items) < 20 {
		part, nextErr := reader.NextPart()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			return errors.New("上传内容不完整。")
		}
		filename := part.FileName()
		current, processErr := h.importMultipartPart(ctx, a, part)
		_ = part.Close()
		entry := item{Filename: filename, Task: publicParseTask(current)}
		if current.Result != nil {
			entry.Result = current.Result
		}
		if processErr != nil {
			entry.Error = processErr.Error()
		}
		items = append(items, entry)
	}
	if len(items) == 0 {
		return errImport
	}
	if len(items) == 20 {
		if extra, nextErr := reader.NextPart(); nextErr == nil {
			_ = extra.Close()
			return errors.New("批量导入最多支持20个文件。")
		}
	}
	failed := 0
	for _, entry := range items {
		if entry.Error != "" {
			failed++
		}
	}
	return webJSON(w, map[string]any{"items": items, "count": len(items), "failed": failed})
}
func (h *webHost) preview(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	var q struct {
		DraftID contracts.ID `json:"draft_id"`
	}
	if err := readWebJSON(w, r, &q); err != nil {
		return err
	}
	scope, err := a.scope(ctx, "document.create", "")
	if err != nil {
		return errAuthority
	}
	d, err := h.s.loadDraft(ctx, h.db, q.DraftID)
	if err != nil {
		return err
	}
	item, err := resultFor(d, true)
	if err != nil {
		return err
	}
	if a.verify(ctx, scope) != nil {
		return errAuthority
	}
	history := []webRevision{}
	rows, err := h.db.QueryContext(ctx, `SELECT revision_id,publication_epoch::text,action,occurred_at,reason FROM akos_withdraw_v1.publication_history WHERE space_id=$1 AND kb_id=$2 AND document_id=$3 ORDER BY publication_epoch`, h.s.m.Space, h.s.m.KB, d.DocumentID)
	if err != nil {
		return errDatabase
	}
	for rows.Next() {
		var revision, epoch string
		var revisionID sql.NullString
		var v webRevision
		if rows.Scan(&revisionID, &epoch, &v.Action, &v.OccurredAt, &v.Reason) != nil {
			rows.Close()
			return errDatabase
		}
		if revisionID.Valid {
			revision = revisionID.String
			v.RevisionID = contracts.ID(revision)
		}
		value, scanErr := strconv.ParseUint(epoch, 10, 64)
		if scanErr != nil {
			rows.Close()
			return errDatabase
		}
		v.Epoch = value
		history = append(history, v)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return errDatabase
	}
	rows.Close()
	review, decision, err := h.s.reviewDetails(ctx, h.db, d.ID)
	if err != nil {
		return errDatabase
	}
	reviewHistory, err := h.s.reviewHistory(ctx, h.db, d.ID)
	if err != nil {
		return errDatabase
	}
	reviewItems := make([]map[string]any, 0, len(reviewHistory))
	for _, item := range reviewHistory {
		reviewItems = append(reviewItems, map[string]any{"id": item.ID, "revision_id": item.RevisionID, "decision": item.Decision, "reason": item.Reason, "actor_id": item.ActorID, "decided_at": item.DecidedAt})
	}
	job, err := publicationJob(ctx, h.db, h.s.m.Space, h.s.m.KB, d.ID)
	if err != nil {
		return errDatabase
	}
	return webJSON(w, map[string]any{"draft_id": d.ID, "document_id": d.DocumentID, "content_hash": item.ContentHash, "text": d.Blocks[0].Content.Text, "history": history, "review_state": review, "review": decision, "review_history": reviewItems, "job": job, "original_available": len(d.Blobs) == 1})
}

func exportSafeName(value contracts.ID) string {
	var out strings.Builder
	for _, r := range string(value) {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			out.WriteRune(r)
		}
		if out.Len() >= 96 {
			break
		}
	}
	if out.Len() == 0 {
		return "draft"
	}
	return out.String()
}

func (h *webHost) exportDocument(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	draftID := contracts.ID(r.PathValue("draft"))
	if draftID == "" {
		return errors.New("缺少草稿标识。")
	}
	scope, err := a.scope(ctx, "document.create", "")
	if err != nil {
		return errAuthority
	}
	d, err := h.s.loadDraft(ctx, h.db, draftID)
	if err != nil {
		return errors.New("草稿不存在或不可导出。")
	}
	item, err := resultFor(d, false)
	if err != nil {
		return errImport
	}
	if a.verify(ctx, scope) != nil {
		return errAuthority
	}
	format := "text"
	if len(d.Blobs) == 1 {
		for name, media := range mediaFormats {
			if d.Blobs[0].MediaType == media {
				format = name
			}
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="memorys-`+exportSafeName(d.ID)+`.json"`)
	return json.NewEncoder(w).Encode(map[string]any{
		"document_id":  d.DocumentID,
		"draft_id":     d.ID,
		"state":        d.State,
		"format":       format,
		"content_hash": item.ContentHash,
		"text":         d.Blocks[0].Content.Text,
		"blocks":       d.Blocks,
		"blobs":        d.Blobs,
		"tags":         h.tagsFor(d.DocumentID),
	})
}

func (h *webHost) editDocument(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	var q struct {
		DraftID contracts.ID     `json:"draft_id"`
		Text    string           `json:"text"`
		Hash    contracts.Digest `json:"expect_hash"`
	}
	if err := readWebJSON(w, r, &q); err != nil {
		return err
	}
	if q.DraftID == "" {
		return errors.New("缺少要编辑的草稿。")
	}
	if q.Hash != "" && len(q.Hash) != 64 {
		return errors.New("当前草稿摘要无效，请刷新后重试。")
	}
	if a == nil || h.s == nil || h.db == nil {
		return errAuthority
	}
	if q.Hash != "" {
		current, err := h.s.loadDraft(ctx, h.db, q.DraftID)
		if err != nil {
			return err
		}
		frozen, err := document.BuildFrozenContent(current)
		if err != nil || frozen.ContentHash != q.Hash {
			return errors.New("草稿已被其他操作更新，请刷新后再编辑。")
		}
	}
	h.mutations.Lock()
	defer h.mutations.Unlock()
	item, err := h.s.editDraft(ctx, h.db, a, q.DraftID, q.Text)
	if err != nil {
		return err
	}
	return webJSON(w, item)
}

func (h *webHost) submitDocument(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	var q struct {
		DraftID contracts.ID     `json:"draft_id"`
		Hash    contracts.Digest `json:"expect_hash"`
	}
	if err := readWebJSON(w, r, &q); err != nil {
		return err
	}
	if q.DraftID == "" || len(q.Hash) != 64 {
		return errors.New("请先核对全文和内容摘要。")
	}
	h.mutations.Lock()
	defer h.mutations.Unlock()
	result, err := h.s.submitDraft(ctx, h.db, a, q.DraftID, q.Hash)
	if err != nil {
		return err
	}
	return webJSON(w, map[string]any{"state": "submitted", "draft_id": q.DraftID, "revision_id": result.Revision.ID, "approval_request_id": result.ApprovalRequestID})
}

func (h *webHost) reviewDocument(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	var q struct {
		DraftID  contracts.ID `json:"draft_id"`
		Decision string       `json:"decision"`
		Reason   string       `json:"reason"`
	}
	if err := readWebJSON(w, r, &q); err != nil {
		return err
	}
	if q.DraftID == "" || (q.Decision != "approve" && q.Decision != "reject") {
		return errors.New("审核决定只能是 approve 或 reject。")
	}
	h.mutations.Lock()
	defer h.mutations.Unlock()
	approval, err := h.s.reviewDraft(ctx, h.db, a, q.DraftID, q.Decision, q.Reason)
	if err != nil {
		return err
	}
	state := "rejected"
	if approval.Decision == "approve" {
		state = "approved"
	}
	return webJSON(w, map[string]any{"state": state, "draft_id": q.DraftID, "decision_id": approval.ID, "revision_id": approval.RevisionID, "reason": approval.Reason, "actor_id": approval.ActorID, "decided_at": approval.DecidedAt})
}

func (h *webHost) withdrawDocument(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	var q struct {
		DocumentID contracts.ID `json:"document_id"`
		Reason     string       `json:"reason"`
	}
	if err := readWebJSON(w, r, &q); err != nil {
		return err
	}
	if q.DocumentID == "" || q.Reason == "" {
		return errors.New("请提供文档和撤回原因。")
	}
	h.mutations.Lock()
	defer h.mutations.Unlock()
	result, err := h.s.withdraw(ctx, h.db, a, q.DocumentID, q.Reason)
	if err != nil {
		return err
	}
	return webJSON(w, map[string]any{"state": "withdrawn", "document_id": result.DocumentID, "history_id": result.HistoryID, "publication_epoch": result.PublicationEpoch})
}

func (h *webHost) deleteDocument(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	var q struct {
		DocumentID contracts.ID `json:"document_id"`
	}
	if err := readWebJSON(w, r, &q); err != nil {
		return err
	}
	if q.DocumentID == "" {
		return errors.New("请提供要删除的文档。")
	}
	if a == nil || h.s == nil || h.db == nil {
		return errAuthority
	}
	h.mutations.Lock()
	defer h.mutations.Unlock()
	result, err := h.s.deleteDocument(ctx, h.db, a, q.DocumentID)
	if err != nil {
		return err
	}
	return webJSON(w, map[string]any{"state": "deleted", "document_id": result.DocumentID, "history_id": result.HistoryID, "job_id": result.JobID, "publication_epoch": result.PublicationEpoch})
}

func (h *webHost) original(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	draftID := contracts.ID(r.PathValue("draft"))
	d, err := h.s.loadDraft(ctx, h.db, draftID)
	if err != nil || len(d.Blobs) != 1 {
		return errors.New("原件不存在或当前版本不支持下载。")
	}
	return h.streamOriginal(ctx, w, r, a, d.DocumentID, d.Blobs[0])
}

// originalCitation resolves a blob from an immutable published revision. It
// lets search evidence open the exact source revision even after a newer draft
// has been created for the same document.
func (h *webHost) originalCitation(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	documentID := contracts.ID(r.PathValue("document"))
	revisionID := contracts.ID(r.PathValue("revision"))
	blobID := contracts.ID(r.PathValue("blob"))
	if documentID == "" || revisionID == "" || blobID == "" {
		return errors.New("原件引用不完整。")
	}
	var deleted, published bool
	if err := h.db.QueryRowContext(ctx, `SELECT d.deleted,EXISTS(SELECT 1 FROM akos_withdraw_v1.publication_history h WHERE h.space_id=d.space_id AND h.kb_id=d.kb_id AND h.document_id=d.id AND h.revision_id=$4 AND h.action='publish') FROM akos_withdraw_v1.documents d WHERE d.space_id=$1 AND d.kb_id=$2 AND d.id=$3`, h.s.m.Space, h.s.m.KB, documentID, revisionID).Scan(&deleted, &published); err != nil || deleted || !published {
		return errors.New("原件引用不存在或已撤回。")
	}
	var raw []byte
	if err := h.db.QueryRowContext(ctx, `SELECT payload FROM akos_withdraw_v1.revision_contents WHERE space_id=$1 AND kb_id=$2 AND document_id=$3 AND revision_id=$4`, h.s.m.Space, h.s.m.KB, documentID, revisionID).Scan(&raw); err != nil || len(raw) == 0 || len(raw) > 4<<20 {
		return errors.New("原件引用不存在或当前版本不支持下载。")
	}
	var revision contracts.Revision
	if json.Unmarshal(raw, &revision) != nil || revision.ID != revisionID || revision.DocumentID != documentID {
		return errors.New("原件引用校验失败。")
	}
	for _, blob := range revision.Blobs {
		if blob.ID == blobID {
			return h.streamOriginal(ctx, w, r, a, documentID, blob)
		}
	}
	return errors.New("原件引用不存在或当前版本不支持下载。")
}

func (h *webHost) streamOriginal(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority, documentID contracts.ID, blob contracts.BlobRef) error {
	downloadValues, hasDownload := r.URL.Query()["download"]
	if len(r.URL.Query()) > 1 || len(downloadValues) > 1 || hasDownload && len(downloadValues) == 1 && downloadValues[0] != "1" {
		return errors.New("原件请求参数无效。")
	}
	scope, err := a.scope(ctx, "original.read", documentID)
	if err != nil {
		return errAuthority
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		return errAuthority
	}
	if scope.ExpiresAt.Before(deadline) {
		deadline = scope.ExpiresAt
	}
	request := contracts.BlobReadRequest{Request: contracts.RequestContext{RequestID: contracts.ID(nonce()), ProfileDigest: h.s.m.ProfileDigest, Deadline: deadline}, Scope: scope, Blob: blob}
	store := h.s.blobStore(a, "original.read", blob)
	reader, err := store.Open(ctx, request)
	if err != nil {
		return errors.New("原件读取失败，请重试。")
	}
	defer reader.Close()
	format := "bin"
	for name, media := range mediaFormats {
		if blob.MediaType == media {
			format = name
			break
		}
	}
	filename := "original." + format
	if hasDownload {
		w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	} else {
		w.Header().Set("Content-Disposition", `inline; filename="`+filename+`"`)
	}
	w.Header().Set("Content-Type", blob.MediaType)
	w.Header().Set("Content-Length", strconv.FormatInt(blob.Size, 10))
	if _, err = io.CopyN(w, reader, blob.Size); err != nil {
		return errors.New("原件读取未完成。")
	}
	return nil
}
func (h *webHost) publishDocument(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	var q struct {
		DraftID  contracts.ID     `json:"draft_id"`
		Hash     contracts.Digest `json:"expect_hash"`
		Approved bool             `json:"approved"`
	}
	if err := readWebJSON(w, r, &q); err != nil {
		return err
	}
	if !q.Approved || len(q.Hash) != 64 {
		return errors.New("请先核对全文和摘要，再确认批准发布。")
	}
	h.mutations.Lock()
	defer h.mutations.Unlock()
	result, err := h.s.publish(ctx, h.db, a, q.DraftID, q.Hash, nil)
	if err != nil {
		return err
	}
	return webJSON(w, map[string]any{"state": "published", "document_id": result.Operation.DocumentID, "revision_id": result.Operation.RevisionID})
}

type webCitation struct {
	DocumentID  contracts.ID        `json:"document_id"`
	RevisionID  contracts.ID        `json:"revision_id"`
	BlockID     contracts.ID        `json:"block_id"`
	Kind        string              `json:"source_kind"`
	BlobID      contracts.ID        `json:"blob_id,omitempty"`
	Locator     webLocator          `json:"locator"`
	DerivedFrom []webOriginalAnchor `json:"derived_from,omitempty"`
}

type webOriginalAnchor struct {
	BlobID  contracts.ID `json:"blob_id"`
	Locator webLocator   `json:"locator"`
}

type webLocator struct {
	Precision     string                    `json:"precision,omitempty"`
	Page          *int                      `json:"page,omitempty"`
	Slide         *int                      `json:"slide,omitempty"`
	StartMS       *int64                    `json:"start_ms,omitempty"`
	EndMS         *int64                    `json:"end_ms,omitempty"`
	Confidence    *float64                  `json:"confidence,omitempty"`
	Worksheet     string                    `json:"worksheet,omitempty"`
	CellRange     string                    `json:"cell_range,omitempty"`
	ParagraphPath []int                     `json:"paragraph_path,omitempty"`
	Characters    *contracts.CharacterRange `json:"characters,omitempty"`
	Box           *contracts.BoundingBox    `json:"box,omitempty"`
}

type webRevision struct {
	RevisionID contracts.ID `json:"revision_id"`
	Epoch      uint64       `json:"publication_epoch"`
	Action     string       `json:"action"`
	OccurredAt time.Time    `json:"occurred_at"`
	Reason     string       `json:"reason"`
}
type webEvidence struct {
	Citation         webCitation `json:"citation"`
	Excerpt          string      `json:"source_excerpt"`
	Rank             int         `json:"rank"`
	Score            *float64    `json:"score,omitempty"`
	SourceStatus     string      `json:"source_status"`
	PublicationEpoch uint64      `json:"publication_epoch"`
	UpdatedAt        time.Time   `json:"updated_at"`
	SourceTitle      string      `json:"source_title"`
	Routes           []string    `json:"retrieval_routes,omitempty"`
	Tags             []string    `json:"tags,omitempty"`
}

type searchCursor struct {
	Query         string `json:"query"`
	Mode          string `json:"mode"`
	Source        string `json:"source_kind"`
	Status        string `json:"source_status"`
	Sort          string `json:"sort"`
	UpdatedAfter  string `json:"updated_after"`
	UpdatedBefore string `json:"updated_before"`
	Tags          string `json:"tags"`
	Page          int    `json:"page"`
	PageSize      int    `json:"page_size"`
}

func encodeSearchCursor(value searchCursor) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 2048 {
		return "", errDatabase
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func webCite(c contracts.Citation) webCitation {
	l := c.Source.Locator
	result := webCitation{DocumentID: c.DocumentID, RevisionID: c.RevisionID, BlockID: c.BlockID, Kind: c.Source.Kind, BlobID: c.Source.BlobID, Locator: webLocator{Precision: l.Precision, Page: l.Page, Slide: l.Slide, StartMS: l.StartMS, EndMS: l.EndMS, Confidence: l.Confidence, Worksheet: l.Worksheet, CellRange: l.CellRange, ParagraphPath: append([]int(nil), l.ParagraphPath...), Characters: l.Characters, Box: l.Box}}
	if len(c.Source.DerivedFrom) > 0 {
		result.DerivedFrom = make([]webOriginalAnchor, 0, len(c.Source.DerivedFrom))
		for _, origin := range c.Source.DerivedFrom {
			ol := origin.Locator
			result.DerivedFrom = append(result.DerivedFrom, webOriginalAnchor{BlobID: origin.BlobID, Locator: webLocator{Precision: ol.Precision, Page: ol.Page, Slide: ol.Slide, StartMS: ol.StartMS, EndMS: ol.EndMS, Confidence: ol.Confidence, Worksheet: ol.Worksheet, CellRange: ol.CellRange, ParagraphPath: append([]int(nil), ol.ParagraphPath...), Characters: ol.Characters, Box: ol.Box}})
		}
	}
	return result
}
func (h *webHost) webEvidence(ctx context.Context, citation contracts.Citation, excerpt string, rank int, score *float64, origins []contracts.FusionOrigin) (webEvidence, error) {
	var deleted bool
	var active sql.NullString
	var epoch, occurred string
	err := h.db.QueryRowContext(ctx, `SELECT d.deleted,d.active_revision_id,d.publication_epoch::text,h.occurred_at::text
FROM akos_withdraw_v1.documents d
LEFT JOIN LATERAL (SELECT occurred_at FROM akos_withdraw_v1.publication_history h WHERE h.space_id=d.space_id AND h.kb_id=d.kb_id AND h.document_id=d.id AND h.action='publish' AND h.revision_id=$4 ORDER BY h.publication_epoch DESC LIMIT 1) h ON true
WHERE d.space_id=$1 AND d.kb_id=$2 AND d.id=$3`, h.s.m.Space, h.s.m.KB, citation.DocumentID, citation.RevisionID).Scan(&deleted, &active, &epoch, &occurred)
	if err != nil {
		return webEvidence{}, errDatabase
	}
	value, err := strconv.ParseUint(epoch, 10, 64)
	if err != nil || value == 0 || occurred == "" {
		return webEvidence{}, errDatabase
	}
	updated, err := time.Parse(time.RFC3339Nano, occurred)
	if err != nil {
		return webEvidence{}, errDatabase
	}
	status := "historical"
	if !deleted && active.Valid && active.String == string(citation.RevisionID) {
		status = "published"
	}
	title := string(citation.DocumentID)
	_ = h.db.QueryRowContext(ctx, `SELECT title FROM akos_withdraw_v1.document_metadata WHERE space_id=$1 AND kb_id=$2 AND document_id=$3`, h.s.m.Space, h.s.m.KB, citation.DocumentID).Scan(&title)
	routes := make([]string, 0, len(origins))
	seen := map[string]bool{}
	for _, origin := range origins {
		if origin.RouteID != "" && !seen[string(origin.RouteID)] {
			seen[string(origin.RouteID)] = true
			routes = append(routes, string(origin.RouteID))
		}
	}
	return webEvidence{Citation: webCite(citation), Excerpt: excerpt, Rank: rank, Score: score, SourceStatus: status, PublicationEpoch: value, UpdatedAt: updated.UTC(), SourceTitle: title, Routes: routes, Tags: h.tagsFor(citation.DocumentID)}, nil
}
func (h *webHost) search(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	var q struct {
		Query         string   `json:"query"`
		Mode          string   `json:"mode"`
		Page          int      `json:"page"`
		PageSize      int      `json:"page_size"`
		Source        string   `json:"source_kind"`
		Status        string   `json:"source_status"`
		Sort          string   `json:"sort"`
		Cursor        string   `json:"cursor"`
		UpdatedAfter  string   `json:"updated_after"`
		UpdatedBefore string   `json:"updated_before"`
		Tags          []string `json:"tags"`
	}
	if err := readWebJSON(w, r, &q); err != nil {
		return err
	}
	if strings.TrimSpace(q.Query) == "" || len(q.Query) > 4096 {
		return errors.New("请输入不超过 4096 字节的问题。")
	}
	if q.Sort == "" {
		q.Sort = "rank"
	}
	tags, err := normalizeTags(q.Tags)
	if err != nil {
		return err
	}
	tagKey := strings.Join(tags, "\x00")
	page, pageSize := q.Page, q.PageSize
	if q.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(q.Cursor)
		if err != nil || len(raw) > 2048 {
			return errors.New("分页游标无效，请重新开始检索。")
		}
		var cursor searchCursor
		if json.Unmarshal(raw, &cursor) != nil || cursor.Page < 1 || cursor.PageSize < 1 || cursor.PageSize > 50 || cursor.Query != q.Query || cursor.Mode != q.Mode || cursor.Source != q.Source || cursor.Status != q.Status || cursor.Sort != q.Sort || cursor.UpdatedAfter != q.UpdatedAfter || cursor.UpdatedBefore != q.UpdatedBefore || cursor.Tags != tagKey {
			return errors.New("分页游标与当前检索条件不匹配，请重新开始检索。")
		}
		page, pageSize = cursor.Page, cursor.PageSize
	}
	if page == 0 {
		page = 1
	}
	if pageSize == 0 {
		pageSize = 10
	}
	if page < 1 || page > 100 || pageSize < 1 || pageSize > 50 {
		return errors.New("分页参数无效：页码范围为 1-100，每页 1-50 条。")
	}
	if q.Source != "" && q.Source != "original" && q.Source != "revision" {
		return errors.New("来源筛选只能是 original 或 revision。")
	}
	if q.Status != "" && q.Status != "published" && q.Status != "historical" {
		return errors.New("发布状态筛选只能是 published 或 historical。")
	}
	if q.Sort != "rank" && q.Sort != "score" {
		return errors.New("排序方式只能是 rank 或 score。")
	}
	var updatedAfter, updatedBefore time.Time
	if q.UpdatedAfter != "" {
		updatedAfter, err = time.Parse(time.RFC3339, q.UpdatedAfter)
		if err != nil {
			return errors.New("更新时间起点必须是 RFC3339 时间。")
		}
	}
	if q.UpdatedBefore != "" {
		updatedBefore, err = time.Parse(time.RFC3339, q.UpdatedBefore)
		if err != nil {
			return errors.New("更新时间终点必须是 RFC3339 时间。")
		}
	}
	if !updatedAfter.IsZero() && !updatedBefore.IsZero() && !updatedAfter.Before(updatedBefore) {
		return errors.New("更新时间范围无效。")
	}
	offset := (page - 1) * pageSize
	if offset >= 100 {
		return errors.New("页码超出本地检索的有界范围，请缩小页码或每页数量。")
	}
	fetchLimit := offset + pageSize + 1
	if q.Mode != "answer" {
		// Fetch the bounded candidate window before filtering so a page does not
		// appear short merely because the first candidates were filtered out.
		fetchLimit = 100
	}
	if fetchLimit > 100 {
		return errors.New("页码超出本地检索的有界范围，请缩小页码或每页数量。")
	}
	if q.Mode == "answer" && page != 1 {
		return errors.New("知识问答只支持第一页，引用已随答案完整返回。")
	}
	items := []webEvidence{}
	cites := []webCitation{}
	response := map[string]any{"mode": q.Mode, "page": page, "page_size": pageSize, "source_kind": q.Source, "source_status": q.Status, "sort": q.Sort, "updated_after": q.UpdatedAfter, "updated_before": q.UpdatedBefore, "tags": tags, "has_more": false, "next_cursor": "", "evidence": items, "citations": cites}
	if q.Mode == "keyword" {
		service, err := h.s.searchService(h.db)
		if err != nil {
			return err
		}
		deadline, _ := ctx.Deadline()
		evidence, err := service.Search(ctx, knowledge.SearchInput{Credential: a.credential, RequestID: contracts.ID(nonce()), Deadline: deadline, SpaceID: h.s.m.Space, KnowledgeBaseIDs: []contracts.ID{h.s.m.KB}, Text: q.Query, Limit: fetchLimit})
		if err != nil {
			return err
		}
		for _, e := range evidence {
			item, err := h.webEvidence(ctx, e.Citation, e.Text, e.Rank, nil, nil)
			if err != nil {
				return err
			}
			items = append(items, item)
		}
	} else if q.Mode == "hybrid" {
		list, err := h.s.hybridCandidatesLimit(ctx, h.db, a, q.Query, fetchLimit)
		if err != nil {
			return err
		}
		for _, e := range list {
			item, err := h.webEvidence(ctx, e.Citation, e.Text, e.Rank, &e.Score, e.FusionOrigins)
			if err != nil {
				return err
			}
			items = append(items, item)
		}
	} else if q.Mode == "semantic" || q.Mode == "answer" {
		var result map[string]any
		if q.Mode == "answer" {
			result, err = h.s.aiWithLimitFiltered(ctx, h.db, a, q.Query, true, fetchLimit, answerFilter{Source: q.Source, Status: q.Status, Tags: tags, UpdatedAfter: updatedAfter, UpdatedBefore: updatedBefore})
		} else {
			result, err = h.s.aiWithLimit(ctx, h.db, a, q.Query, false, fetchLimit)
		}
		if err != nil {
			return err
		}
		if list, ok := result["evidence"].([]contracts.Candidate); ok {
			for _, e := range list {
				score := e.Score
				item, err := h.webEvidence(ctx, e.Citation, e.Text, e.Rank, &score, nil)
				if err != nil {
					return err
				}
				items = append(items, item)
			}
		}
		if list, ok := result["evidence"].([]contracts.Evidence); ok {
			for _, e := range list {
				item, err := h.webEvidence(ctx, e.Citation, e.SourceExcerpt, e.Rank, nil, nil)
				if err != nil {
					return err
				}
				items = append(items, item)
			}
		}
		if list, ok := result["citations"].([]contracts.Citation); ok {
			for _, c := range list {
				cites = append(cites, webCite(c))
			}
		}
		response["answer"], response["status"], response["model"] = result["answer"], result["status"], result["answer_model"]
	} else {
		return errors.New("未知检索方式。")
	}
	if q.Mode != "answer" {
		filtered := items[:0]
		for _, item := range items {
			if q.Source != "" && item.Citation.Kind != q.Source || q.Status != "" && item.SourceStatus != q.Status || !updatedAfter.IsZero() && item.UpdatedAt.Before(updatedAfter) || !updatedBefore.IsZero() && !item.UpdatedAt.Before(updatedBefore) || !tagsMatch(item.Tags, tags) {
				continue
			}
			filtered = append(filtered, item)
		}
		items = filtered
		if q.Sort == "score" {
			sort.SliceStable(items, func(i, j int) bool {
				left, right := 0.0, 0.0
				if items[i].Score != nil {
					left = *items[i].Score
				}
				if items[j].Score != nil {
					right = *items[j].Score
				}
				if left == right {
					return items[i].Rank < items[j].Rank
				}
				return left > right
			})
		}
	}
	if q.Mode != "answer" {
		if offset >= len(items) {
			items = []webEvidence{}
		} else {
			end := offset + pageSize
			if end > len(items) {
				end = len(items)
			}
			hasMore := end < len(items)
			response["has_more"] = hasMore
			if hasMore {
				next, err := encodeSearchCursor(searchCursor{Query: q.Query, Mode: q.Mode, Source: q.Source, Status: q.Status, Sort: q.Sort, UpdatedAfter: q.UpdatedAfter, UpdatedBefore: q.UpdatedBefore, Tags: tagKey, Page: page + 1, PageSize: pageSize})
				if err != nil {
					return errDatabase
				}
				response["next_cursor"] = next
			}
			items = items[offset:end]
		}
	}
	response["evidence"], response["citations"] = items, cites
	return webJSON(w, response)
}

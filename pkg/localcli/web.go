package localcli

import (
	"bytes"
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
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
	h := &webHost{db: db, s: s, address: address, cookie: nonce() + nonce(), expires: time.Now().Add(2 * time.Hour), slots: make(chan struct{}, 4)}
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
		"GET /local/api/status":    h.status,
		"GET /local/api/documents": h.documents,
		"POST /local/api/import":   h.importDocument,
		"POST /local/api/preview":  h.preview,
		"POST /local/api/publish":  h.publishDocument,
		"POST /local/api/search":   h.search,
	} {
		mux.HandleFunc(path, h.authorized(action))
	}
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
		if err = action(ctx, w, r, a); err != nil {
			webError(w, 400, err.Error())
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
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
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
	return webJSON(w, json.RawMessage(buffer.Bytes()))
}

type webDocument struct {
	DocumentID contracts.ID     `json:"document_id"`
	DraftID    contracts.ID     `json:"draft_id"`
	Hash       contracts.Digest `json:"content_hash"`
	Preview    string           `json:"preview"`
	State      string           `json:"state"`
	Format     string           `json:"format"`
}

func (h *webHost) documents(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	scope, err := a.scope(ctx, "document.create", "")
	if err != nil {
		return errAuthority
	}
	rows, err := h.db.QueryContext(ctx, `SELECT dh.id,dh.state,EXISTS(SELECT 1 FROM akos_withdraw_v1.approval_requests ar WHERE ar.document_id=d.id AND ar.revision_id=d.active_revision_id AND ar.state='approved') FROM akos_withdraw_v1.draft_heads dh JOIN akos_withdraw_v1.documents d ON d.id=dh.document_id AND d.space_id=dh.space_id AND d.kb_id=dh.kb_id WHERE dh.space_id=$1 AND dh.kb_id=$2 AND NOT d.deleted ORDER BY dh.id LIMIT 101`, h.s.m.Space, h.s.m.KB)
	if err != nil {
		return errDatabase
	}
	type head struct {
		id        contracts.ID
		state     string
		published bool
	}
	heads := []head{}
	for rows.Next() {
		var item head
		if rows.Scan(&item.id, &item.state, &item.published) != nil {
			rows.Close()
			return errDatabase
		}
		heads = append(heads, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return errDatabase
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
		items = append(items, webDocument{d.DocumentID, d.ID, preview.ContentHash, preview.Preview, state, format})
	}
	if a.verify(ctx, scope) != nil {
		return errAuthority
	}
	return webJSON(w, map[string]any{"documents": items, "has_more": more})
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
	if part.FormName() != "file" || part.FileName() == "" {
		return errImport
	}
	ext := strings.ToLower(filepath.Ext(part.FileName()))
	if ext != ".txt" && ext != ".md" && ext != ".markdown" && mediaFormats[strings.TrimPrefix(ext, ".")] == "" {
		return errors.New("支持 TXT、Markdown、PDF、DOCX、XLSX、PPTX。")
	}
	f, err := os.CreateTemp(h.s.root, ".web-upload-*"+ext)
	if err != nil {
		return errImport
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if f.Chmod(0600) != nil || protect(f.Name(), false) != nil {
		return errImport
	}
	n, err := io.Copy(f, io.LimitReader(part, (8<<20)+1))
	if err != nil || n == 0 || n > 8<<20 || f.Close() != nil {
		return errImport
	}
	if _, err = reader.NextPart(); err != io.EOF {
		return errors.New("每次请选择一个文件。")
	}
	h.mutations.Lock()
	defer h.mutations.Unlock()
	item, err := h.s.importFile(ctx, h.db, a, f.Name())
	if err != nil {
		return err
	}
	return webJSON(w, item)
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
	return webJSON(w, map[string]any{"draft_id": d.ID, "document_id": d.DocumentID, "content_hash": item.ContentHash, "text": d.Blocks[0].Content.Text})
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
	DocumentID contracts.ID `json:"document_id"`
	RevisionID contracts.ID `json:"revision_id"`
	BlockID    contracts.ID `json:"block_id"`
	Kind       string       `json:"source_kind"`
}
type webEvidence struct {
	Citation webCitation `json:"citation"`
	Excerpt  string      `json:"source_excerpt"`
	Rank     int         `json:"rank"`
	Score    *float64    `json:"score,omitempty"`
}

func webCite(c contracts.Citation) webCitation {
	return webCitation{c.DocumentID, c.RevisionID, c.BlockID, c.Source.Kind}
}
func (h *webHost) search(ctx context.Context, w http.ResponseWriter, r *http.Request, a *authority) error {
	var q struct {
		Query string `json:"query"`
		Mode  string `json:"mode"`
	}
	if err := readWebJSON(w, r, &q); err != nil {
		return err
	}
	if strings.TrimSpace(q.Query) == "" || len(q.Query) > 4096 {
		return errors.New("请输入不超过 4096 字节的问题。")
	}
	items := []webEvidence{}
	cites := []webCitation{}
	response := map[string]any{"mode": q.Mode, "evidence": items, "citations": cites}
	if q.Mode == "keyword" {
		service, err := h.s.searchService(h.db)
		if err != nil {
			return err
		}
		deadline, _ := ctx.Deadline()
		evidence, err := service.Search(ctx, knowledge.SearchInput{Credential: a.credential, RequestID: contracts.ID(nonce()), Deadline: deadline, SpaceID: h.s.m.Space, KnowledgeBaseIDs: []contracts.ID{h.s.m.KB}, Text: q.Query, Limit: 10})
		if err != nil {
			return err
		}
		for _, e := range evidence {
			items = append(items, webEvidence{webCite(e.Citation), e.Text, e.Rank, nil})
		}
	} else if q.Mode == "semantic" || q.Mode == "answer" {
		result, err := h.s.ai(ctx, h.db, a, q.Query, q.Mode == "answer")
		if err != nil {
			return err
		}
		if list, ok := result["evidence"].([]contracts.Candidate); ok {
			for _, e := range list {
				score := e.Score
				items = append(items, webEvidence{webCite(e.Citation), e.Text, e.Rank, &score})
			}
		}
		if list, ok := result["evidence"].([]contracts.Evidence); ok {
			for _, e := range list {
				items = append(items, webEvidence{webCite(e.Citation), e.SourceExcerpt, e.Rank, nil})
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
	response["evidence"], response["citations"] = items, cites
	return webJSON(w, response)
}

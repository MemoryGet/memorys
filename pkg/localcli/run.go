package localcli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/bootstrap"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/modelclient"
)

type options struct {
	command, root, address, hash, uiDir string
	samples                             bool
	positional                          []string
}

func parse(args []string) (options, error) {
	o := options{root: filepath.Join(".local", "runtime"), address: "127.0.0.1:8080", uiDir: filepath.Join("dashboard", "dist")}
	bad := errors.New("usage: memorys-local init [--with-samples] | enable-ai | serve | web [--ui-dir DIR] | import FILE | publish DRAFT --expect-hash HASH | search QUERY | semantic-search QUERY | ask QUESTION | doctor; options: --state DIR --address 127.0.0.1:PORT")
	if len(args) == 0 {
		return o, bad
	}
	o.command = args[0]
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--with-samples":
			if o.command != "init" || o.samples {
				return o, bad
			}
			o.samples = true
		case "--state", "--address", "--expect-hash", "--ui-dir":
			flag := args[i]
			i++
			if i >= len(args) || args[i] == "" {
				return o, bad
			}
			switch flag {
			case "--state":
				o.root = args[i]
			case "--address":
				if o.command != "serve" && o.command != "web" && o.command != "search" {
					return o, bad
				}
				o.address = args[i]
			case "--expect-hash":
				if o.command != "publish" || o.hash != "" {
					return o, bad
				}
				o.hash = args[i]
			case "--ui-dir":
				if o.command != "web" {
					return o, bad
				}
				o.uiDir = args[i]
			}
		default:
			if strings.HasPrefix(args[i], "--") {
				return o, bad
			}
			o.positional = append(o.positional, args[i])
		}
	}
	count := 0
	switch o.command {
	case "init", "enable-ai", "serve", "web", "doctor":
	case "import", "publish", "search", "semantic-search", "ask":
		count = 1
	default:
		return o, bad
	}
	if len(o.positional) != count || o.command == "publish" && len(o.hash) != 64 {
		return o, bad
	}
	host, port, err := net.SplitHostPort(o.address)
	n, e := strconv.Atoi(port)
	if err != nil || e != nil || host != "127.0.0.1" || n < 1 || n > 65535 {
		return o, errors.New("local HTTP address must be 127.0.0.1:PORT")
	}
	return o, nil
}

func output(w io.Writer, v any) error {
	if json.NewEncoder(w).Encode(v) != nil {
		return errors.New("cannot write command output")
	}
	return nil
}

// Run keeps all credentials out of command arguments, normal output and errors.
// A database URL is supplied by the process environment; pgpass is supported.
func Run(ctx context.Context, args []string, w io.Writer) error {
	o, err := parse(args)
	if err != nil {
		return err
	}
	if o.command != "serve" && o.command != "web" {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
	}
	if o.command == "doctor" {
		return doctor(ctx, o.root, w)
	}
	var s *state
	if o.command == "init" {
		s, err = createState(o.root)
	} else {
		s, err = loadState(o.root, o.command != "serve")
	}
	if err != nil {
		return err
	}
	db, err := openDB(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	if o.command == "enable-ai" {
		if !s.m.Ready {
			return errState
		}
		if err = s.enableAI(ctx, db); err != nil {
			return err
		}
		return output(w, map[string]any{"state": "ai-enabled", "formats": []string{"txt", "md", "pdf", "docx", "xlsx", "pptx"}, "embedding": s.m.Embedding, "answer_provider": "DeepSeek"})
	}
	if o.command == "init" {
		if err = s.initialize(ctx, db); err != nil {
			return err
		}
		result := map[string]any{"state": "ready", "runtime_directory": s.root, "samples_published": false}
		if o.samples {
			a, err := s.identity(ctx, db, false)
			if err != nil {
				return err
			}
			defer a.close()
			imported, err := s.importText(ctx, db, a, "Memorys 本地示例\n\nmemoryslocalsample\n这是通过提交、所有者审批、关键词索引和发布形成的示例正文。\n")
			if err != nil {
				return err
			}
			published, err := s.publish(ctx, db, a, imported.DraftID, imported.ContentHash, nil)
			if err != nil {
				return err
			}
			result["samples_published"] = true
			result["sample_document_id"] = published.Operation.DocumentID
			result["sample_query"] = "memoryslocalsample"
		}
		return output(w, result)
	}
	if !s.m.Ready || s.validateDB(ctx, db) != nil {
		return errDatabase
	}
	if o.command == "serve" {
		return serve(ctx, db, s, o.address, w)
	}
	if o.command == "web" {
		h, err := newWebHandler(db, s, o.address, o.uiDir)
		if err != nil {
			return err
		}
		return serveHTTP(ctx, h, o.address, w)
	}
	a, err := s.identity(ctx, db, false)
	if err != nil {
		return err
	}
	defer a.close()
	switch o.command {
	case "import":
		result, err := s.importFile(ctx, db, a, o.positional[0])
		if err != nil {
			return err
		}
		return output(w, result)
	case "publish":
		result, err := s.publish(ctx, db, a, contracts.ID(o.positional[0]), contracts.Digest(o.hash), nil)
		if err != nil {
			return err
		}
		return output(w, map[string]any{"state": "published", "document_id": result.Operation.DocumentID, "revision_id": result.Operation.RevisionID, "publication_epoch": result.History.PublicationEpoch})
	case "search":
		raw, err := searchHTTP(ctx, s, a, o.address, o.positional[0])
		if err != nil {
			return err
		}
		_, err = w.Write(append(raw, '\n'))
		return err
	case "semantic-search", "ask":
		result, err := s.ai(ctx, db, a, o.positional[0], o.command == "ask")
		if err != nil {
			return err
		}
		return output(w, result)
	}
	return errors.New("unsupported command")
}

func handler(db *sql.DB, s *state) (http.Handler, error) {
	search, err := s.searchService(db)
	if err != nil {
		return nil, err
	}
	return bootstrap.New(bootstrap.Config{Search: search, ReadyCheck: func(ctx context.Context) error {
		c, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		return db.PingContext(c)
	}})
}

func serve(ctx context.Context, db *sql.DB, s *state, address string, w io.Writer) error {
	h, err := handler(db, s)
	if err != nil {
		return err
	}
	return serveHTTP(ctx, h, address, w)
}

func serveHTTP(ctx context.Context, h http.Handler, address string, w io.Writer) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return errors.New("local HTTP port unavailable")
	}
	defer listener.Close()
	server := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 130 * time.Second, WriteTimeout: 130 * time.Second, IdleTimeout: time.Minute}
	stop, done := make(chan struct{}), make(chan struct{})
	defer close(stop)
	go func() {
		defer close(done)
		select {
		case <-ctx.Done():
			c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if server.Shutdown(c) != nil {
				_ = server.Close()
			}
		case <-stop:
		}
	}()
	if err = output(w, map[string]any{"state": "listening", "address": address}); err != nil {
		return err
	}
	err = server.Serve(listener)
	if ctx.Err() != nil {
		<-done
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return errors.New("local HTTP service stopped unexpectedly")
	}
	return nil
}

func searchHTTP(ctx context.Context, s *state, a *authority, address, query string) ([]byte, error) {
	if !utf8.ValidString(query) || len(query) > 4096 || strings.TrimSpace(query) == "" || strings.ContainsRune(query, 0) {
		return nil, errors.New("query must be nonempty UTF-8 up to 4096 bytes")
	}
	raw := encode(map[string]any{"query": query, "space_id": s.m.Space, "kb_ids": []contracts.ID{s.m.KB}, "top_k": 10})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+address+"/api/v1/knowledge-os/search", bytes.NewReader(raw))
	if err != nil {
		return nil, errors.New("search request rejected")
	}
	request.Header.Set("Authorization", "Bearer "+string(a.credential.Secret))
	request.Header.Set("Content-Type", "application/json")
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New("local search server unavailable; start memorys-local serve")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, errors.New("search rejected with HTTP " + strconv.Itoa(response.StatusCode) + "; run doctor and check identity/permissions")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 4<<20+1))
	if err != nil || len(body) > 4<<20 || !json.Valid(body) {
		return nil, errors.New("search response rejected")
	}
	return body, nil
}

func doctor(ctx context.Context, root string, w io.Writer) error {
	checks := map[string]any{"database": false, "schema": false, "identity": false, "permissions": false, "configuration": false, "published_documents": 0, "searchable": false}
	defer func() { _ = output(w, map[string]any{"checks": checks, "production_acceptance": false}) }()
	s, err := loadState(root, false)
	if err != nil {
		checks["action"] = "restore the matching runtime directory; init requires an empty business database"
		return err
	}
	checks["local_state"] = s.m.Ready
	if _, e := s.readSigningKey(); e == nil {
		checks["signing_key_file"] = true
	} else {
		checks["action"] = "restore private.pem from the matching protected backup"
		return errState
	}
	db, err := openDB(ctx)
	if err != nil {
		checks["action"] = "check PostgreSQL service, MEMORYS_DATABASE_URL and pgpass"
		return err
	}
	defer db.Close()
	checks["database"] = true
	if checkSchema(ctx, db) != nil {
		checks["action"] = "verify withdraw_contract_v1 migration ledger"
		return errDatabase
	}
	checks["schema"] = true
	rows := s.rows()
	for i, group := range [][]configRow{rows[:5], s.grants(), rows[5:]} {
		ok := true
		for _, r := range group {
			var match bool
			if db.QueryRowContext(ctx, r.check, r.args...).Scan(&match) != nil || !match {
				ok = false
			}
		}
		checks[[]string{"identity", "permissions", "configuration"}[i]] = ok
	}
	var aclOK bool
	if db.QueryRowContext(ctx, s.aclCheck(), s.m.Space).Scan(&aclOK) != nil || !aclOK {
		checks["permissions"] = false
	}
	if now := time.Now().Unix(); now < s.m.KeyFrom || now+900 > s.m.KeyUntil {
		checks["identity"] = false
	}
	var count int
	if db.QueryRowContext(ctx, `SELECT count(*) FROM akos_withdraw_v1.documents d JOIN akos_withdraw_v1.publication_intents i ON i.document_id=d.id AND i.revision_id=d.active_revision_id JOIN akos_withdraw_v1.approval_requests a ON a.document_id=d.id AND a.revision_id=d.active_revision_id AND a.current_decision_id=i.approval_id AND a.state='approved' JOIN akos_withdraw_v1.index_generations g ON g.generation_set_id=d.active_generation_set_id AND g.channel='keyword' AND g.profile_digest=$3 WHERE d.space_id=$1 AND d.kb_id=$2 AND NOT d.deleted AND EXISTS(SELECT 1 FROM akos_withdraw_v1.index_projection_rows p WHERE p.generation_id=g.id) AND NOT EXISTS(SELECT 1 FROM akos_withdraw_v1.index_deletion_fences f WHERE f.generation_id=g.id)`, s.m.Space, s.m.KB, s.profile.Index.Digest).Scan(&count) != nil {
		return errDatabase
	}
	checks["published_documents"] = count
	if s.validateDB(ctx, db) != nil || !s.m.Ready || checks["identity"] != true {
		checks["action"] = "restore matching state/database or inspect current identity, ACL and profile conflicts; nothing was changed"
		return errDatabase
	}
	checks["searchable"] = count > 0
	checks["ai_enabled"] = s.m.Features == 1
	if s.m.Features == 1 {
		checks["embedding"] = s.m.Embedding
		checks["answer_key_configured"] = strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY")) != ""
		c, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		p, e := converter()
		checks["office_parser"] = e == nil && p.Health(c) == nil
		model, e := modelclient.NewOllama(ollamaURL(), *s.m.Embedding)
		checks["embedding_model"] = e == nil && model.Health(c) == nil
		if checks["office_parser"] != true || checks["embedding_model"] != true || checks["answer_key_configured"] != true {
			checks["ai_action"] = "check pinned parser, Ollama model and DEEPSEEK_API_KEY; keyword search can still be used"
		}
	}
	if count == 0 {
		checks["action"] = "import and explicitly publish a document, or init --with-samples"
	} else {
		checks["action"] = "start serve and use search; document count is not an index integrity test"
	}
	return nil
}

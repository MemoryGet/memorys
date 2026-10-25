// Package localcli is the explicit, single-owner development control plane.
// It provisions no enterprise identities and never runs implicitly on startup.
package localcli

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/MemoryGet/memorys/pkg/configprofile"
	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/identitytoken"
	jose "github.com/go-jose/go-jose/v4"
)

var errState = errors.New("local state missing or inconsistent; restore the matching runtime directory or run doctor")

// Retain only a fixed operation label and numeric OS code. PathError.Error and
// third-party error text can contain user paths, content or credentials.
type stateIOError struct {
	operation string
	code      syscall.Errno
}

func (e stateIOError) Error() string {
	message := errState.Error() + " (" + e.operation
	if e.code != 0 {
		message += "; system code " + strconv.FormatUint(uint64(e.code), 10)
	}
	return message + ")"
}
func (e stateIOError) Unwrap() error { return errState }
func stateFailure(operation string, cause error) error {
	var prior stateIOError
	if errors.As(cause, &prior) {
		return prior
	}
	var code syscall.Errno
	_ = errors.As(cause, &code)
	return stateIOError{operation: operation, code: code}
}

type manifest struct {
	Format                                                       string
	Installation, Space, KB, Owner, Worker, Client, KeyID, Store contracts.ID
	Issuer, Audience                                             string
	PublicKeyDigest                                              string
	KeyFrom, KeyUntil                                            int64
	ProfileDigest                                                contracts.Digest
	Ready                                                        bool
	Features                                                     uint64                   `json:",omitempty"`
	Embedding                                                    *contracts.EmbeddingSpec `json:",omitempty"`
}

type state struct {
	root    string
	m       manifest
	profile configprofile.Snapshot
	key     *rsa.PrivateKey // Explicit owner commands (including web); serve uses public keys only.
	vectors vectorMemo
}

func digest(raw []byte) string { s := sha256.Sum256(raw); return hex.EncodeToString(s[:]) }
func nonce() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func encode(v any) []byte { b, _ := json.Marshal(v); return b }

func localProfile() (configprofile.Snapshot, error) {
	writer := contracts.ModuleBinding{InstanceID: "postgres-keyword-preparation", Capability: contracts.IndexWriting, ContractVersion: "1.0", RequiredFeatures: []string{"attempt.isolated", "lease.fenced"}}
	splitter := contracts.ModuleBinding{InstanceID: "native-character-splitter", Capability: contracts.Splitting, ContractVersion: "1.0"}
	reader := contracts.ModuleBinding{InstanceID: "postgres-keyword-reader", Capability: contracts.Retrieval, ContractVersion: "1.0"}
	return configprofile.Seal(configprofile.Snapshot{Format: "akos.profile-snapshot/1",
		Pipeline: contracts.PipelineProfile{ID: "local-pipeline", Version: 1, IndexProfile: contracts.ProfileReference{ID: "local-index", Version: 1}, QueryProfile: contracts.ProfileReference{ID: "local-query", Version: 1}},
		Index:    contracts.IndexProfile{ID: "local-index", Version: 1, Mode: "independent", Bindings: map[contracts.Capability]contracts.ModuleBinding{contracts.IndexWriting: writer, contracts.Splitting: splitter}, Routes: []contracts.IndexRoute{{ID: "keyword", Channels: []contracts.Channel{contracts.KeywordChannel}, Binding: writer}}, Policy: contracts.IndexPolicy{SchemaVersion: 1, Chunking: contracts.ChunkPolicy{Strategy: "characters", MaxCodepoints: 1024}, RequiredChannels: []contracts.Channel{contracts.KeywordChannel}, ProjectionFormatVersion: "akos.chunks/1"}},
		Query:    contracts.QueryProfile{ID: "local-query", Version: 1, Bindings: map[contracts.Capability]contracts.ModuleBinding{contracts.Retrieval: reader}, Routes: []contracts.RetrievalRoute{{ID: "keyword-route", Binding: reader, Channels: []contracts.Channel{contracts.KeywordChannel}, CandidateLimit: 1000, Required: true}}, Policy: contracts.QueryPolicy{SchemaVersion: 1, CandidateLimit: 1000, AnswerEvidenceLimit: 10, FusionMethod: "rrf", RankConstant: 60, RetrievalTimeoutMS: 5000}},
	})
}

func writeAtomic(root, name string, raw []byte) error {
	f, err := os.CreateTemp(root, ".pending-")
	if err != nil {
		return stateFailure("create temporary state file", err)
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return stateFailure("set state file mode", err)
	}
	if _, err = f.Write(raw); err != nil {
		f.Close()
		return stateFailure("write state file", err)
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return stateFailure("sync state file", err)
	}
	if err = f.Close(); err != nil {
		return stateFailure("close state file", err)
	}
	if err = protect(tmp, false); err != nil {
		return stateFailure("protect state file", err)
	}
	if err = os.Rename(tmp, filepath.Join(root, name)); err != nil {
		return stateFailure("commit state file", err)
	}
	return nil
}

func commitStateDirectory(stage, target string) error {
	// Both paths must stay in the explicitly selected runtime parent. The
	// source is the complete, privately staged directory created by this call.
	if !filepath.IsAbs(stage) || !filepath.IsAbs(target) || filepath.Dir(stage) != filepath.Dir(target) || !strings.HasPrefix(filepath.Base(stage), ".runtime-pending-") || stage == target {
		return stateFailure("check staged state directory", errState)
	}
	delay := 10 * time.Millisecond
	for attempt := 0; ; attempt++ {
		info, err := os.Lstat(stage)
		if err != nil {
			return stateFailure("inspect staged state directory", err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return stateFailure("inspect staged state directory", errState)
		}
		if _, err = os.Lstat(target); !os.IsNotExist(err) {
			if err == nil {
				err = os.ErrExist
			}
			return stateFailure("state destination already exists or is inaccessible", err)
		}
		err = renameStateDirectory(stage, target)
		if err == nil {
			return nil
		}
		if attempt >= 7 || !transientStateDirectoryRename(err) {
			return stateFailure("commit staged state directory", err)
		}
		// Windows readers that deny delete sharing can temporarily block a
		// directory rename. Bound the delay; permanent denial remains an error.
		time.Sleep(delay)
		if delay < 160*time.Millisecond {
			delay *= 2
		}
	}
}

// Creation uses an owned staging directory and a single directory rename, so
// power loss cannot expose a manifest without its matching key/profile/JWKS.
func createState(root string) (_ *state, resultErr error) {
	operation := "resolve state directory"
	defer func() {
		if resultErr != nil {
			resultErr = stateFailure(operation, resultErr)
		}
	}()
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	operation = "inspect state directory"
	if _, err = os.Stat(abs); err == nil {
		operation = "validate existing state"
		return loadState(abs, true)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	operation = "create state parent"
	if err = os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		return nil, err
	}
	operation = "create staged state directory"
	stage, err := os.MkdirTemp(filepath.Dir(abs), ".runtime-pending-")
	if err != nil {
		return nil, err
	}
	// Retain an incomplete owned stage on failure for diagnosis; never delete a user directory.
	operation = "protect staged state directory"
	if err = protect(stage, true); err != nil {
		return nil, err
	}
	operation = "generate signing key"
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	operation = "encode signing public key"
	pub, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return nil, err
	}
	operation = "seal local profile"
	profile, err := localProfile()
	if err != nil {
		return nil, err
	}
	id := nonce()
	now := time.Now().Unix()
	m := manifest{Format: "memorys.local/1", Installation: contracts.ID(id), Space: contracts.ID("local-" + id), KB: contracts.ID("kb-" + id), Owner: "local-owner", Worker: "local-worker", Client: "local-cli", KeyID: "local-key", Store: contracts.ID("store-" + id), Issuer: "https://memorys.local/" + id, Audience: "memorys-search", PublicKeyDigest: digest(pub), KeyFrom: now - 60, KeyUntil: now + 365*24*3600, ProfileDigest: profile.Pipeline.Digest}
	operation = "encode signing private key"
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	defer clear(private)
	operation = "encode sealed profile"
	profileRaw, err := configprofile.Encode(profile)
	if err != nil {
		return nil, err
	}
	for name, raw := range map[string][]byte{"manifest.json": encode(m), "profile.json": profileRaw, "jwks.json": encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: string(m.KeyID), Algorithm: "RS256", Use: "sig"}}}), "private.pem": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})} {
		if err = writeAtomic(stage, name, raw); err != nil {
			return nil, err
		}
	}
	operation = "commit staged state directory"
	if err = commitStateDirectory(stage, abs); err != nil {
		return nil, err
	}
	operation = "validate created state"
	return loadState(abs, true)
}

func decodeFile(path string, v any) error {
	f, err := os.Open(path)
	if err != nil {
		return errState
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 1<<20+1))
	if err != nil || len(b) > 1<<20 {
		return errState
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(new(any)) != io.EOF {
		return errState
	}
	return nil
}

func loadState(root string, private bool) (*state, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, errState
	}
	info, err := os.Lstat(abs)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errState
	}
	for _, name := range []string{"manifest.json", "profile.json", "jwks.json"} {
		info, err := os.Lstat(filepath.Join(abs, name))
		if err != nil || !info.Mode().IsRegular() {
			return nil, errState
		}
	}
	s := &state{root: abs}
	if decodeFile(filepath.Join(abs, "manifest.json"), &s.m) != nil || s.m.Format != "memorys.local/1" || len(s.m.Installation) != 32 {
		return nil, errState
	}
	if s.m.Features > 1 || s.m.Features == 0 && s.m.Embedding != nil || s.m.Features == 1 && s.m.Embedding == nil {
		return nil, errState
	}
	id := string(s.m.Installation)
	if _, err := hex.DecodeString(id); err != nil || s.m.Space != contracts.ID("local-"+id) || s.m.KB != contracts.ID("kb-"+id) || s.m.Store != contracts.ID("store-"+id) || s.m.Owner != "local-owner" || s.m.Worker != "local-worker" || s.m.Client != "local-cli" || s.m.KeyID != "local-key" || s.m.Issuer != "https://memorys.local/"+id || s.m.Audience != "memorys-search" || s.m.KeyFrom < 0 || s.m.KeyUntil <= s.m.KeyFrom {
		return nil, errState
	}
	s.profile, err = configprofile.LoadFile(filepath.Join(abs, "profile.json"))
	if err != nil {
		return nil, errState
	}
	expected, err := localProfile()
	if err != nil || !reflect.DeepEqual(s.profile, expected) || s.m.ProfileDigest != s.profile.Pipeline.Digest {
		return nil, errState
	}
	keys, err := identitytoken.LoadRS256PublicKeys(filepath.Join(abs, "jwks.json"))
	if err != nil || len(keys) != 1 || keys[string(s.m.KeyID)] == nil {
		return nil, errState
	}
	pub, _ := x509.MarshalPKIXPublicKey(keys[string(s.m.KeyID)])
	if digest(pub) != s.m.PublicKeyDigest {
		return nil, errState
	}
	if private {
		path := filepath.Join(abs, "private.pem")
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || protect(abs, true) != nil || protect(path, false) != nil {
			return nil, errState
		}
		s.key, err = s.readSigningKey()
		if err != nil {
			return nil, err
		}
	}
	return s, nil
}

// Also used by doctor without changing DACLs, registering tokens or signing.
func (s *state) readSigningKey() (*rsa.PrivateKey, error) {
	path := filepath.Join(s.root, "private.pem")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errState
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errState
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 16385))
	if err != nil || len(raw) > 16384 {
		return nil, errState
	}
	defer clear(raw)
	block, rest := pem.Decode(raw)
	if block == nil || block.Type != "PRIVATE KEY" || len(bytes.TrimSpace(rest)) != 0 {
		return nil, errState
	}
	defer clear(block.Bytes)
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errState
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok || key.Validate() != nil {
		return nil, errState
	}
	pub, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil || digest(pub) != s.m.PublicKeyDigest {
		return nil, errState
	}
	return key, nil
}

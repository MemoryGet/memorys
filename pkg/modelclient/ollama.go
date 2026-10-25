// Package modelclient implements bounded model ports without database access.
package modelclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

var ErrModel = errors.New("model unavailable or response invalid; check model configuration and connectivity")

type Ollama struct {
	endpoint string
	spec     contracts.EmbeddingSpec
	http     *http.Client
}

// The web host creates model clients per request. Their connection ownership
// must be shared and bounded: an abandoned per-client Transport otherwise
// retains idle sockets and its read/write goroutines indefinitely.
var modelHTTPTransport = &http.Transport{
	Proxy:               nil,
	MaxIdleConns:        8,
	MaxIdleConnsPerHost: 4,
	MaxConnsPerHost:     8,
	IdleConnTimeout:     30 * time.Second,
	TLSHandshakeTimeout: 10 * time.Second,
}

func httpClient() *http.Client {
	return &http.Client{Timeout: 90 * time.Second, Transport: modelHTTPTransport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func localEndpoint(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" {
		return "", ErrModel
	}
	host, _, err := net.SplitHostPort(u.Host)
	if err != nil || host != "127.0.0.1" {
		return "", ErrModel
	}
	return strings.TrimSuffix(raw, "/"), nil
}
func decodeResponse(resp *http.Response, target any, limit int64) error {
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return ErrModel
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || int64(len(raw)) > limit {
		return ErrModel
	}
	if _, err = canonical.JSON(raw); err != nil || json.Unmarshal(raw, target) != nil {
		return ErrModel
	}
	return nil
}
func (c *Ollama) modelDigest(ctx context.Context, model string) (string, error) {
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/api/tags", nil)
	if err != nil {
		return "", ErrModel
	}
	response, err := c.http.Do(r)
	if err != nil {
		return "", ErrModel
	}
	var out struct {
		Models []struct{ Name, Digest string }
	}
	if decodeResponse(response, &out, 1<<20) != nil {
		return "", ErrModel
	}
	for _, m := range out.Models {
		if m.Name == model && len(m.Digest) == 64 {
			return m.Digest, nil
		}
	}
	return "", ErrModel
}

// DiscoverOllama pins the exact locally installed model digest and dimension.
// No model is pulled and no document is sent by this discovery call.
func DiscoverOllama(ctx context.Context, endpoint, model string) (*Ollama, error) {
	base, err := localEndpoint(endpoint)
	if err != nil || model == "" || len(model) > 200 {
		return nil, ErrModel
	}
	if !strings.Contains(model, ":") {
		model += ":latest"
	}
	c := &Ollama{endpoint: base, http: httpClient()}
	revision, err := c.modelDigest(ctx, model)
	if err != nil {
		return nil, ErrModel
	}
	c.spec = contracts.EmbeddingSpec{ProviderModel: "ollama/" + model, ModelRevision: revision, Distance: "cosine", Normalization: "l2", PreprocessVersion: "utf8-exact/1"}
	vectors, err := c.embedBatch(ctx, model, []string{"模型连通性检查"})
	if err != nil || len(vectors) != 1 {
		return nil, ErrModel
	}
	c.spec.Dimension = len(vectors[0])
	if c.spec.Dimension < 16 || c.spec.Dimension > 4096 {
		return nil, ErrModel
	}
	return c, nil
}
func NewOllama(endpoint string, spec contracts.EmbeddingSpec) (*Ollama, error) {
	base, err := localEndpoint(endpoint)
	if err != nil || !strings.HasPrefix(spec.ProviderModel, "ollama/") || len(spec.ModelRevision) != 64 || spec.Dimension < 16 || spec.Dimension > 4096 || spec.Distance != "cosine" || spec.Normalization != "l2" || spec.PreprocessVersion != "utf8-exact/1" {
		return nil, ErrModel
	}
	return &Ollama{endpoint: base, spec: spec, http: httpClient()}, nil
}
func (c *Ollama) Spec() contracts.EmbeddingSpec { return c.spec }
func (c *Ollama) Health(ctx context.Context) error {
	got, err := c.modelDigest(ctx, strings.TrimPrefix(c.spec.ProviderModel, "ollama/"))
	if err != nil || got != c.spec.ModelRevision {
		return ErrModel
	}
	return nil
}

func normalize(v contracts.Vector) error {
	var norm float64
	for _, f := range v {
		if math.IsNaN(float64(f)) || math.IsInf(float64(f), 0) {
			return ErrModel
		}
		norm += float64(f) * float64(f)
	}
	if norm <= 1e-20 || math.IsInf(norm, 0) {
		return ErrModel
	}
	norm = math.Sqrt(norm)
	for i := range v {
		v[i] = float32(float64(v[i]) / norm)
	}
	return nil
}
func (c *Ollama) embedBatch(ctx context.Context, model string, texts []string) ([]contracts.Vector, error) {
	raw, _ := json.Marshal(map[string]any{"model": model, "input": texts, "truncate": false, "keep_alive": "5m"})
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/api/embed", bytes.NewReader(raw))
	if err != nil {
		return nil, ErrModel
	}
	r.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(r)
	if err != nil {
		return nil, ErrModel
	}
	var out struct {
		Model      string
		Embeddings []contracts.Vector
	}
	if decodeResponse(response, &out, 8<<20) != nil || out.Model != model || len(out.Embeddings) != len(texts) {
		return nil, ErrModel
	}
	for _, v := range out.Embeddings {
		if c.spec.Dimension != 0 && len(v) != c.spec.Dimension || normalize(v) != nil {
			return nil, ErrModel
		}
	}
	return out.Embeddings, nil
}
func (c *Ollama) Embed(ctx context.Context, q contracts.EmbedRequest) (contracts.EmbedResult, error) {
	if ctx == nil || q.Spec != c.spec || len(q.Texts) == 0 || len(q.Texts) > 513 || q.Request.Deadline.IsZero() {
		return contracts.EmbedResult{}, ErrModel
	}
	ctx, cancel := context.WithDeadline(ctx, q.Request.Deadline)
	defer cancel()
	if c.Health(ctx) != nil {
		return contracts.EmbedResult{}, ErrModel
	}
	result := contracts.EmbedResult{Spec: c.spec}
	total := 0
	for _, text := range q.Texts {
		total += len(text)
		if text == "" || len(text) > 8192 || total > 1<<20 {
			return contracts.EmbedResult{}, ErrModel
		}
	}
	for start := 0; start < len(q.Texts); start += 16 {
		end := start + 16
		if end > len(q.Texts) {
			end = len(q.Texts)
		}
		batch, err := c.embedBatch(ctx, strings.TrimPrefix(c.spec.ProviderModel, "ollama/"), q.Texts[start:end])
		if err != nil {
			return contracts.EmbedResult{}, ErrModel
		}
		result.Vectors = append(result.Vectors, batch...)
	}
	if c.Health(ctx) != nil {
		return contracts.EmbedResult{}, ErrModel
	}
	return result, nil
}

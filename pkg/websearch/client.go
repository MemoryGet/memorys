// Package websearch provides a small, dependency-free client for DeepSeek's
// Search and Fetch APIs. It deliberately stays below the application and
// retrieval layers so callers can apply their own authorization and evidence
// policy before using web content.
package websearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	DefaultBaseURL  = "https://api.deepseek.com"
	DefaultTimeout  = 30 * time.Second
	maxResponseBody = 16 << 20
)

var (
	ErrInvalidConfig  = errors.New("invalid websearch configuration")
	ErrInvalidRequest = errors.New("invalid websearch request")
	ErrUnauthorized   = errors.New("websearch unauthorized")
	ErrRateLimited    = errors.New("websearch rate limited")
	ErrUpstream       = errors.New("websearch upstream error")
)

type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
	maxRetries int
	sleep      func(context.Context, time.Duration) error
}

type Option func(*Client) error

func WithBaseURL(raw string) Option {
	return func(c *Client) error {
		u, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" && u.Path != "/" {
			return fmt.Errorf("%w: base URL", ErrInvalidConfig)
		}
		c.baseURL = strings.TrimRight(raw, "/")
		return nil
	}
}

func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) error {
		if h == nil {
			return fmt.Errorf("%w: nil HTTP client", ErrInvalidConfig)
		}
		c.httpClient = h
		return nil
	}
}

func WithMaxRetries(n int) Option {
	return func(c *Client) error {
		if n < 0 || n > 5 {
			return fmt.Errorf("%w: max retries", ErrInvalidConfig)
		}
		c.maxRetries = n
		return nil
	}
}

// NewClient constructs a client. The key is retained only in memory.
func NewClient(apiKey string, options ...Option) (*Client, error) {
	if strings.TrimSpace(apiKey) == "" || strings.ContainsAny(apiKey, "\r\n") {
		return nil, fmt.Errorf("%w: API key", ErrInvalidConfig)
	}
	c := &Client{apiKey: apiKey, baseURL: DefaultBaseURL, httpClient: &http.Client{Timeout: DefaultTimeout}, maxRetries: 2, sleep: sleepContext}
	for _, option := range options {
		if option == nil {
			continue
		}
		if err := option(c); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// NewClientFromEnv reads DEEPSEEK_API_KEY without exposing it in logs or errors.
func NewClientFromEnv(options ...Option) (*Client, error) {
	return NewClient(os.Getenv("DEEPSEEK_API_KEY"), options...)
}

type SearchRequest struct {
	Query            string  `json:"query"`
	Count            int     `json:"count,omitempty"`
	OriginalQuestion *string `json:"ori_question,omitempty"`
	Localization     string  `json:"localization,omitempty"`
	NeedContent      bool    `json:"need_content,omitempty"`
}

type SearchResult struct {
	RerankScore float64    `json:"rerank_score"`
	Title       string     `json:"title"`
	URL         string     `json:"url"`
	Snippet     string     `json:"snippet"`
	Summary     string     `json:"summary"`
	Content     string     `json:"content"`
	SiteName    string     `json:"site_name"`
	SiteIcon    string     `json:"site_icon"`
	Date        *time.Time `json:"date"`
	Authority   int        `json:"authority"`
}

type SearchResponse struct {
	Results []SearchResult `json:"results"`
}

type FetchRequest struct {
	URL string `json:"url"`
}
type FetchResponse struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

func (c *Client) Search(ctx context.Context, req SearchRequest) (SearchResponse, error) {
	if err := validateSearch(req); err != nil {
		return SearchResponse{}, err
	}
	var out SearchResponse
	if err := c.doJSON(ctx, "/v1/tools/search", req, &out); err != nil {
		return SearchResponse{}, err
	}
	if out.Results == nil {
		out.Results = []SearchResult{}
	}
	return out, nil
}

func (c *Client) Fetch(ctx context.Context, req FetchRequest) (FetchResponse, error) {
	if err := validateFetch(req); err != nil {
		return FetchResponse{}, err
	}
	var out FetchResponse
	if err := c.doJSON(ctx, "/v1/tools/fetch", req, &out); err != nil {
		return FetchResponse{}, err
	}
	return out, nil
}

func validateSearch(req SearchRequest) error {
	if n := utf8.RuneCountInString(req.Query); n < 1 || n > 128 || strings.TrimSpace(req.Query) == "" {
		return fmt.Errorf("%w: query must contain 1-128 non-whitespace characters", ErrInvalidRequest)
	}
	if req.Count != 0 && (req.Count < 1 || req.Count > 50) {
		return fmt.Errorf("%w: count must be between 1 and 50", ErrInvalidRequest)
	}
	if req.OriginalQuestion != nil && (utf8.RuneCountInString(*req.OriginalQuestion) < 1 || utf8.RuneCountInString(*req.OriginalQuestion) > 128 || strings.TrimSpace(*req.OriginalQuestion) == "") {
		return fmt.Errorf("%w: ori_question must contain 1-128 non-whitespace characters", ErrInvalidRequest)
	}
	return nil
}

func validateFetch(req FetchRequest) error {
	if len(req.URL) == 0 || len(req.URL) > 8192 {
		return fmt.Errorf("%w: URL must be 1-8192 bytes", ErrInvalidRequest)
	}
	u, err := url.Parse(req.URL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return fmt.Errorf("%w: URL must be an HTTP(S) URL without credentials", ErrInvalidRequest)
	}
	return nil
}

func (c *Client) doJSON(ctx context.Context, path string, request any, response any) error {
	if ctx == nil {
		return fmt.Errorf("%w: nil context", ErrInvalidRequest)
	}
	body, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("%w: encode request: %v", ErrInvalidRequest, err)
	}
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if attempt < c.maxRetries {
				if err := c.sleep(ctx, backoff(attempt)); err != nil {
					return err
				}
				continue
			}
			return fmt.Errorf("%w: %v", ErrUpstream, err)
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody+1))
		resp.Body.Close()
		if readErr != nil {
			return fmt.Errorf("%w: read response: %v", ErrUpstream, readErr)
		}
		if len(data) > maxResponseBody {
			return fmt.Errorf("%w: response too large", ErrUpstream)
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if err := json.Unmarshal(data, response); err != nil {
				return fmt.Errorf("%w: invalid response", ErrUpstream)
			}
			return nil
		}
		if resp.StatusCode == http.StatusUnauthorized {
			return ErrUnauthorized
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			if attempt < c.maxRetries {
				if err := c.sleep(ctx, retryAfter(resp.Header.Get("Retry-After"), attempt)); err != nil {
					return err
				}
				continue
			}
			return ErrRateLimited
		}
		if resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode >= 500 {
			if attempt < c.maxRetries {
				if err := c.sleep(ctx, backoff(attempt)); err != nil {
					return err
				}
				continue
			}
		}
		return fmt.Errorf("%w: HTTP %d", ErrUpstream, resp.StatusCode)
	}
}

func backoff(attempt int) time.Duration { return time.Duration(1<<attempt) * 100 * time.Millisecond }
func retryAfter(raw string, attempt int) time.Duration {
	if n, err := time.ParseDuration(strings.TrimSpace(raw) + "s"); err == nil && n > 0 && n <= 10*time.Second {
		return n
	}
	return backoff(attempt)
}
func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

package moduletransport_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
	"github.com/MemoryGet/memorys/pkg/moduletransport"
	"github.com/MemoryGet/memorys/pkg/modulewire"
)

func streamFixture(t *testing.T, ending string) (string, modulewire.GenerationExpectation) {
	t.Helper()
	citation := contracts.Citation{DocumentID: "doc-1", RevisionID: "rev-1", BlockID: "block-1", Source: contracts.SourceAnchor{Kind: "original", BlobID: "blob-1", Locator: contracts.Locator{Precision: "coarse"}}}
	expected := modulewire.GenerationExpectation{RequestID: "r", ProfileVersion: 1, ProfileDigest: contracts.Digest(strings.Repeat("a", 64)), Citations: []contracts.Citation{citation}}
	result := contracts.AnswerResult{RequestID: "r", Answer: "维修😀", Citations: []contracts.Citation{citation}, Status: "answered", ProfileVersion: 1, ProfileDigest: expected.ProfileDigest, TimingsMS: map[string]float64{}, Degraded: []contracts.Degradation{}}
	var text strings.Builder
	for _, event := range []contracts.AnswerEvent{{Kind: "meta"}, {Kind: "answer_delta", Text: result.Answer}, {Kind: "done", Result: &result}} {
		data, err := modulewire.EncodeAnswerEvent(event, "r")
		if err != nil {
			t.Fatal(err)
		}
		text.WriteString("event: " + event.Kind + ending + "data: " + string(data) + ending + ending)
	}
	return text.String(), expected
}
func limits() moduletransport.AnswerStreamLimits {
	return moduletransport.AnswerStreamLimits{MaxLineBytes: 4096, MaxFrameBytes: 8192, MaxWireBytes: 65536, Answer: modulewire.AnswerLimits{MaxEvents: 20, MaxTextBytes: 4096}}
}
func setHeaders(h http.Header) {
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("X-AKOS-Request-Id", "r")
	h.Set("X-AKOS-Invocation-Id", "i")
}

type observedBody struct {
	io.ReadCloser
	closed atomic.Int32
}

func (body *observedBody) Close() error { body.closed.Add(1); return body.ReadCloser.Close() }
func memoryResponse(text string) (*http.Response, *observedBody) {
	body := &observedBody{ReadCloser: io.NopCloser(strings.NewReader(text))}
	response := &http.Response{StatusCode: 200, Header: http.Header{}, Body: body}
	setHeaders(response.Header)
	return response, body
}
func bounded(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func openGate(context.Context) error { return nil } // Synthetic callback, not an authorization service.

func TestSSELineEndingsBOMCommentsAndMultilineJSON(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n", "\r"} {
		text, expected := streamFixture(t, ending)
		text = "\xef\xbb\xbf: keepalive" + ending + ending + "id: ignored" + ending + "retry: 0" + ending + "future: ignored" + ending + text
		text = strings.Replace(text, `data: {"request_id":"r","type":"meta"}`, "data: {"+ending+`data: "request_id":"r",`+ending+`data: "type":"meta"}`, 1)
		response, body := memoryResponse(text)
		seen := []string{}
		gates := 0
		err := moduletransport.ConsumeAnswerStream(bounded(t), response, "i", expected, limits(), func(context.Context) error { gates++; return nil }, func(event contracts.AnswerEvent) error { seen = append(seen, event.Kind); return nil })
		if err != nil || strings.Join(seen, ",") != "meta,answer_delta,done" || gates != 3 || body.closed.Load() != 1 {
			t.Fatal("framing failed", ending, err, seen, gates)
		}
	}
}
func TestSSERejectsTruncationTypeMismatchAndPostTerminalEvents(t *testing.T) {
	text, expected := streamFixture(t, "\n")
	for name, bad := range map[string]string{"unfinished-frame": strings.TrimSuffix(text, "\n"), "no-terminal": text[:strings.Index(text, "event: done")], "type-mismatch": strings.Replace(text, "event: meta", "event: citation", 1), "extra-event": text + "event: meta\ndata: {\"type\":\"meta\",\"request_id\":\"r\"}\n\n", "invalid-utf8": string([]byte{0xff}) + text} {
		response, body := memoryResponse(bad)
		terminals := 0
		if err := moduletransport.ConsumeAnswerStream(bounded(t), response, "i", expected, limits(), openGate, func(event contracts.AnswerEvent) error {
			if event.Kind == "done" || event.Kind == "error" {
				terminals++
			}
			return nil
		}); err == nil || body.closed.Load() != 1 || terminals != 0 {
			t.Fatal(name, "accepted incomplete stream", err)
		}
	}
}
func TestSSEAccessSinkAndByteLimitsFailClosed(t *testing.T) {
	text, expected := streamFixture(t, "\n")
	response, body := memoryResponse(text)
	emitted := 0
	err := moduletransport.ConsumeAnswerStream(bounded(t), response, "i", expected, limits(), func(context.Context) error { return errors.New("SECRET policy backend") }, func(contracts.AnswerEvent) error { emitted++; return nil })
	if !errors.Is(err, moduletransport.ErrAccess) || emitted != 0 || strings.Contains(err.Error(), "SECRET") || body.closed.Load() != 1 {
		t.Fatal("gate did not fail closed", err)
	}
	response, body = memoryResponse(text)
	err = moduletransport.ConsumeAnswerStream(bounded(t), response, "i", expected, limits(), openGate, func(contracts.AnswerEvent) error { return errors.New("SECRET socket") })
	if !errors.Is(err, moduletransport.ErrSink) || body.closed.Load() != 1 {
		t.Fatal("sink did not close stream", err)
	}
	for _, change := range []func(*moduletransport.AnswerStreamLimits){func(v *moduletransport.AnswerStreamLimits) { v.MaxLineBytes = 8 }, func(v *moduletransport.AnswerStreamLimits) { v.MaxWireBytes = 8 }} {
		budget := limits()
		change(&budget)
		response, _ = memoryResponse(text)
		if err := moduletransport.ConsumeAnswerStream(bounded(t), response, "i", expected, budget, openGate, func(contracts.AnswerEvent) error { return nil }); err == nil {
			t.Fatal("byte budget ignored")
		}
	}
	budget := limits()
	budget.MaxLineBytes = 64
	budget.MaxFrameBytes = 128
	response, _ = memoryResponse(strings.Repeat(": comment\n", 30) + text)
	if err := moduletransport.ConsumeAnswerStream(bounded(t), response, "i", expected, budget, openGate, func(contracts.AnswerEvent) error { return nil }); err == nil {
		t.Fatal("short ignored lines bypassed frame limit")
	}
}
func TestSSEHeadersAndMandatoryGateDeadline(t *testing.T) {
	text, expected := streamFixture(t, "\n")
	for _, mutate := range []func(*http.Response){func(r *http.Response) { r.StatusCode = 503 }, func(r *http.Response) { r.Header.Set("X-AKOS-Invocation-Id", "other") }, func(r *http.Response) { r.Header.Set("Content-Type", "application/json") }, func(r *http.Response) { r.Header.Set("Content-Type", "text/event-stream; charset=latin1") }, func(r *http.Response) { r.Header["x-akos-request-id"] = []string{"r"} }} {
		response, body := memoryResponse(text)
		mutate(response)
		if err := moduletransport.ConsumeAnswerStream(bounded(t), response, "i", expected, limits(), openGate, func(contracts.AnswerEvent) error { return nil }); err == nil || body.closed.Load() != 1 {
			t.Fatal("bad headers accepted")
		}
	}
	response, body := memoryResponse(text)
	if moduletransport.ConsumeAnswerStream(context.Background(), response, "i", expected, limits(), openGate, func(contracts.AnswerEvent) error { return nil }) == nil || body.closed.Load() != 1 {
		t.Fatal("unbounded context accepted")
	}
	response, _ = memoryResponse(text)
	if moduletransport.ConsumeAnswerStream(bounded(t), response, "i", expected, limits(), nil, func(contracts.AnswerEvent) error { return nil }) == nil {
		t.Fatal("missing current access gate accepted")
	}
}
func TestActualHTTPChunkBoundariesAndGateRevocation(t *testing.T) {
	text, expected := streamFixture(t, "\r\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setHeaders(w.Header())
		for _, part := range strings.Split(text, "\r\n") {
			_, _ = io.WriteString(w, part+"\r\n")
			w.(http.Flusher).Flush()
		}
	}))
	defer server.Close()
	ctx := bounded(t)
	request, err := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	if err := moduletransport.ConsumeAnswerStream(ctx, response, "i", expected, limits(), openGate, func(contracts.AnswerEvent) error { seen++; return nil }); err != nil || seen != 3 {
		t.Fatal("HTTP frames failed", err, seen)
	}
	request, err = http.NewRequestWithContext(ctx, "GET", server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err = server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	checks := 0
	seen = 0
	err = moduletransport.ConsumeAnswerStream(ctx, response, "i", expected, limits(), func(context.Context) error {
		checks++
		if checks == 2 {
			return errors.New("revoked")
		}
		return nil
	}, func(contracts.AnswerEvent) error { seen++; return nil })
	if !errors.Is(err, moduletransport.ErrAccess) || seen != 1 {
		t.Fatal("revoked stream emitted another batch", err, seen)
	}
}

type signaledBody struct {
	io.ReadCloser
	reads   int
	blocked chan struct{}
}

func (body *signaledBody) Read(p []byte) (int, error) {
	body.reads++
	if body.reads == 2 {
		close(body.blocked)
	}
	return body.ReadCloser.Read(p)
}
func TestActualHTTPBlockedReadCancelledAndSinkFailureClosesPeer(t *testing.T) {
	_, expected := streamFixture(t, "\n")
	for _, sinkFailure := range []bool{false, true} {
		peerStopped := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			setHeaders(w.Header())
			_, _ = io.WriteString(w, "event: meta\ndata: {\"type\":\"meta\",\"request_id\":\"r\"}\n\n")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			close(peerStopped)
		}))
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer server.Close()
		defer cancel()
		request, err := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body := &signaledBody{ReadCloser: response.Body, blocked: make(chan struct{})}
		response.Body = body
		result := make(chan error, 1)
		go func() {
			result <- moduletransport.ConsumeAnswerStream(ctx, response, "i", expected, limits(), openGate, func(contracts.AnswerEvent) error {
				if sinkFailure {
					return errors.New("sink closed")
				}
				return nil
			})
		}()
		if !sinkFailure {
			select {
			case <-body.blocked:
				cancel()
			case <-ctx.Done():
				t.Fatal("did not reach blocked HTTP read")
			}
		}
		select {
		case err := <-result:
			if sinkFailure {
				if !errors.Is(err, moduletransport.ErrSink) {
					t.Fatal(err)
				}
			} else if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("stream failed to stop")
		}
		select {
		case <-peerStopped:
		case <-time.After(time.Second):
			t.Fatal("server peer remained active")
		}
		cancel()
		server.Close()
	}
}

func TestActualHTTPDeadlineEndsSilentPeerAndErrorTerminalIsNotSuccess(t *testing.T) {
	_, expected := streamFixture(t, "\n")
	peerStopped := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setHeaders(w.Header())
		_, _ = io.WriteString(w, "event: meta\ndata: {\"type\":\"meta\",\"request_id\":\"r\"}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(peerStopped)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	err = moduletransport.ConsumeAnswerStream(ctx, response, "i", expected, limits(), openGate, func(contracts.AnswerEvent) error { return nil })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("silent peer did not expire", err)
	}
	select {
	case <-peerStopped:
	case <-time.After(time.Second):
		t.Fatal("deadline did not close peer")
	}
	failure := contracts.PublicError{Code: "dependency_unavailable", SafeMessage: "SECRET provider details", RequestID: "r"}
	data, err := modulewire.EncodeAnswerEvent(contracts.AnswerEvent{Kind: "error", Error: &failure}, "r")
	if err != nil {
		t.Fatal(err)
	}
	response, _ = memoryResponse("event: meta\ndata: {\"type\":\"meta\",\"request_id\":\"r\"}\n\nevent: error\ndata: " + string(data) + "\n\n")
	err = moduletransport.ConsumeAnswerStream(bounded(t), response, "i", expected, limits(), openGate, func(contracts.AnswerEvent) error { return nil })
	var safe contracts.PublicError
	if !errors.As(err, &safe) || safe.Code != "dependency_unavailable" || strings.Contains(err.Error(), "SECRET") {
		t.Fatal("error terminal became success or leaked", err)
	}
}

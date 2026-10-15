package parserprocess

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/parser"
)

var ErrConversion = errors.New("document conversion unavailable or rejected")

type MarkItDown struct{ Python, Script string }

func (p MarkItDown) Health(ctx context.Context) error {
	return p.process().Health(ctx)
}
func (p MarkItDown) Convert(ctx context.Context, format string, raw []byte) (parser.MarkdownConversion, error) {
	return p.process().Convert(ctx, format, raw)
}
func (p MarkItDown) process() workerProcess {
	return workerProcess{p.Python, p.Script, "markitdown/0.1.3", []string{"docx", "xlsx", "pptx", "pdf"}}
}

// Provider identity and supported formats come from trusted host wiring only.
type workerProcess struct {
	Python, Script, version string
	formats                 []string
}

func (p workerProcess) Health(ctx context.Context) error {
	if ctx == nil || !filepath.IsAbs(p.Python) || !filepath.IsAbs(p.Script) {
		return ErrConversion
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrConversion
	}
	command := exec.CommandContext(ctx, p.Python, "-I", p.Script, "--health")
	command.WaitDelay = time.Second
	out, diagnostic := &boundedOutput{limit: 1024}, &boundedOutput{limit: 4096}
	command.Stdout = out
	command.Stderr = diagnostic
	if command.Run() != nil || ctx.Err() != nil {
		return ErrConversion
	}
	var result struct {
		Version string `json:"version"`
		Healthy bool   `json:"healthy"`
	}
	if decodeResponse(out.buffer.Bytes(), &result) != nil || !result.Healthy || result.Version != p.version {
		return ErrConversion
	}
	return nil
}

type boundedOutput struct {
	buffer bytes.Buffer
	limit  int
}

func (w *boundedOutput) Write(raw []byte) (int, error) {
	if len(raw) > w.limit-w.buffer.Len() {
		return 0, ErrConversion
	}
	return w.buffer.Write(raw)
}
func (p workerProcess) Convert(ctx context.Context, format string, raw []byte) (parser.MarkdownConversion, error) {
	if ctx == nil || !filepath.IsAbs(p.Python) || !filepath.IsAbs(p.Script) || len(raw) == 0 || len(raw) > 8<<20 {
		return parser.MarkdownConversion{}, ErrConversion
	}
	allowed := false
	for _, supported := range p.formats {
		allowed = allowed || supported == format
	}
	if !allowed {
		return parser.MarkdownConversion{}, ErrConversion
	}
	if _, ok := ctx.Deadline(); !ok {
		return parser.MarkdownConversion{}, ErrConversion
	}
	h := sha256.Sum256(raw)
	input, err := json.Marshal(map[string]string{"format": format, "data_base64": base64.StdEncoding.EncodeToString(raw), "sha256": hex.EncodeToString(h[:])})
	if err != nil {
		return parser.MarkdownConversion{}, ErrConversion
	}
	command := exec.CommandContext(ctx, p.Python, "-I", p.Script)
	command.WaitDelay = time.Second
	command.Stdin = bytes.NewReader(input)
	out, diagnostic := &boundedOutput{limit: 5 << 20}, &boundedOutput{limit: 4096}
	command.Stdout = out
	command.Stderr = diagnostic
	if command.Run() != nil || ctx.Err() != nil {
		return parser.MarkdownConversion{}, ErrConversion
	}
	var result struct {
		Version  string `json:"version"`
		Markdown string `json:"markdown"`
	}
	if decodeResponse(out.buffer.Bytes(), &result) != nil || result.Version != p.version || len(result.Markdown) == 0 || len(result.Markdown) > 4<<20 {
		return parser.MarkdownConversion{}, ErrConversion
	}
	return parser.MarkdownConversion{Text: result.Markdown, Version: result.Version}, nil
}

// Worker responses are flat objects. Reject ambiguous keys before struct decoding,
// since encoding/json otherwise silently accepts the last duplicate value.
func decodeResponse(raw []byte, target any) error {
	if !utf8.Valid(raw) {
		return ErrConversion
	}
	scan := json.NewDecoder(bytes.NewReader(raw))
	first, err := scan.Token()
	if err != nil || first != json.Delim('{') {
		return ErrConversion
	}
	seen := map[string]bool{}
	for scan.More() {
		key, err := scan.Token()
		name, ok := key.(string)
		if err != nil || !ok || seen[name] {
			return ErrConversion
		}
		seen[name] = true
		value, err := scan.Token()
		if _, nested := value.(json.Delim); err != nil || nested {
			return ErrConversion
		}
	}
	if end, err := scan.Token(); err != nil || end != json.Delim('}') {
		return ErrConversion
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil || decoder.Decode(new(any)) != io.EOF {
		return ErrConversion
	}
	return nil
}

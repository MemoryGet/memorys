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
	"os"
	"os/exec"
	"path/filepath"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/parser"
)

var ErrConversion = errors.New("document conversion unavailable or rejected")

type MarkItDown struct {
	Python, Script string
	environment    func() []string
}

// OCR is an optional local converter backed by the pinned OCR worker. It uses
// the same bounded, stdin/stdout-only protocol as the Office converter, while
// keeping OCR dependencies out of the normal document parser environment.
type OCR struct {
	Python, Script string
	environment    func() []string
}

// Transcription is an optional local audio converter. The worker returns a
// bounded text projection, which the parser binds back to the original blob.
type Transcription struct {
	Python, Script string
	environment    func() []string
}

func (p Transcription) Health(ctx context.Context) error { return p.process().Health(ctx) }

func (p Transcription) Convert(ctx context.Context, format string, raw []byte) (parser.MarkdownConversion, error) {
	return p.process().Convert(ctx, format, raw)
}

func (p Transcription) process() workerProcess {
	return workerProcess{Python: p.Python, Script: p.Script, version: "transcription/whisper-1", formats: []string{"wav", "mp3", "m4a"}, audioAware: true, environment: p.environment}
}

func (p OCR) Health(ctx context.Context) error {
	return p.process().Health(ctx)
}

func (p OCR) Convert(ctx context.Context, format string, raw []byte) (parser.MarkdownConversion, error) {
	return p.process().Convert(ctx, format, raw)
}

func (p OCR) process() workerProcess {
	return workerProcess{Python: p.Python, Script: p.Script, version: "ocr/tesseract-5", formats: []string{"png", "jpg", "jpeg", "pdf"}, pageAware: true, ocrAware: true, environment: p.environment}
}

func (p MarkItDown) Health(ctx context.Context) error {
	return p.process().Health(ctx)
}
func (p MarkItDown) Convert(ctx context.Context, format string, raw []byte) (parser.MarkdownConversion, error) {
	return p.process().Convert(ctx, format, raw)
}
func (p MarkItDown) process() workerProcess {
	return workerProcess{Python: p.Python, Script: p.Script, version: "markitdown/0.1.3", formats: []string{"docx", "xlsx", "pptx", "pdf"}, pageAware: true, environment: p.environment}
}

// Provider identity and supported formats come from trusted host wiring only.
type workerProcess struct {
	Python, Script, version string
	formats                 []string
	pageAware               bool
	audioAware              bool
	ocrAware                bool
	environment             func() []string
}

type audioSegmentWire struct {
	StartMS int64  `json:"start_ms"`
	EndMS   int64  `json:"end_ms"`
	Text    string `json:"text"`
}

func (p workerProcess) env() []string {
	if p.environment != nil {
		return p.environment()
	}
	return converterEnvironment()
}

func (p workerProcess) Health(ctx context.Context) error {
	if ctx == nil || !filepath.IsAbs(p.Python) || !filepath.IsAbs(p.Script) {
		return ErrConversion
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrConversion
	}
	command := exec.CommandContext(ctx, p.Python, "-I", p.Script, "--health")
	command.Env = p.env()
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
	command.Env = p.env()
	command.WaitDelay = time.Second
	command.Stdin = bytes.NewReader(input)
	out, diagnostic := &boundedOutput{limit: 5 << 20}, &boundedOutput{limit: 4096}
	command.Stdout = out
	command.Stderr = diagnostic
	if command.Run() != nil || ctx.Err() != nil {
		return parser.MarkdownConversion{}, ErrConversion
	}
	var result struct {
		Version       string                  `json:"version"`
		Markdown      string                  `json:"markdown"`
		Pages         []string                `json:"pages"`
		Blocks        []parser.ConvertedBlock `json:"blocks"`
		Segments      []audioSegmentWire      `json:"segments"`
		OCRConfidence []float64               `json:"ocr_confidence"`
	}
	if decodeConversionResponse(out.buffer.Bytes(), p.pageAware, p.audioAware, p.ocrAware, &result) != nil || result.Version != p.version || len(result.Markdown) == 0 || len(result.Markdown) > 4<<20 {
		return parser.MarkdownConversion{}, ErrConversion
	}
	segments := make([]parser.AudioSegment, 0, len(result.Segments))
	for _, segment := range result.Segments {
		segments = append(segments, parser.AudioSegment{StartMS: segment.StartMS, EndMS: segment.EndMS, Text: segment.Text})
	}
	return parser.MarkdownConversion{Text: result.Markdown, Version: result.Version, Pages: result.Pages, Blocks: result.Blocks, AudioSegments: segments, OCRConfidence: result.OCRConfidence}, nil
}

// Untrusted document decoders do not need database credentials, model API
// keys, tokens or user-supplied Python import paths from the parent process.
func converterEnvironment() []string {
	var out []string
	for _, key := range []string{"SystemRoot", "WINDIR", "PATH", "TEMP", "TMP", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "HOME", "LANG", "LC_ALL", "MEMORYS_OCR_LANG", "MEMORYS_TRANSCRIPTION_MODEL", "MEMORYS_TRANSCRIPTION_LANG"} {
		if value, ok := os.LookupEnv(key); ok {
			out = append(out, key+"="+value)
		}
	}
	return out
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

// decodeConversionResponse keeps the flat worker protocol strict while
// allowing the pinned PDF worker's optional page array. MarkItDown remains
// coarse and cannot opt into page claims merely by returning an extra field.
func decodeConversionResponse(raw []byte, pageAware, audioAware, ocrAware bool, target *struct {
	Version       string                  `json:"version"`
	Markdown      string                  `json:"markdown"`
	Pages         []string                `json:"pages"`
	Blocks        []parser.ConvertedBlock `json:"blocks"`
	Segments      []audioSegmentWire      `json:"segments"`
	OCRConfidence []float64               `json:"ocr_confidence"`
}) error {
	if !utf8.Valid(raw) {
		return ErrConversion
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return ErrConversion
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		name, ok := token.(string)
		if err != nil || !ok || seen[name] {
			return ErrConversion
		}
		seen[name] = true
		switch name {
		case "version":
			if decoder.Decode(&target.Version) != nil {
				return ErrConversion
			}
		case "markdown":
			if decoder.Decode(&target.Markdown) != nil {
				return ErrConversion
			}
		case "pages":
			if !pageAware || decoder.Decode(&target.Pages) != nil {
				return ErrConversion
			}
		case "blocks":
			if !pageAware || decoder.Decode(&target.Blocks) != nil {
				return ErrConversion
			}
		case "segments":
			if !audioAware || decoder.Decode(&target.Segments) != nil {
				return ErrConversion
			}
		case "ocr_confidence":
			if !ocrAware || decoder.Decode(&target.OCRConfidence) != nil {
				return ErrConversion
			}
		default:
			return ErrConversion
		}
	}
	if end, err := decoder.Token(); err != nil || end != json.Delim('}') {
		return ErrConversion
	}
	if decoder.Decode(new(any)) != io.EOF {
		return ErrConversion
	}
	return nil
}

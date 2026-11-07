package parser

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

type audioConverterFixture struct{}

func (audioConverterFixture) Convert(context.Context, string, []byte) (MarkdownConversion, error) {
	return MarkdownConversion{Text: "第一段\n\n第二段", Version: "transcription/whisper-1", AudioSegments: []AudioSegment{{StartMS: 0, EndMS: 1200, Text: "第一段"}, {StartMS: 1200, EndMS: 2400, Text: "第二段"}}}, nil
}

type audioBlobFixture struct {
	contracts.BlobStore
	raw []byte
}

func (b audioBlobFixture) Open(context.Context, contracts.BlobReadRequest) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(string(b.raw))), nil
}

func TestAudioParserPreservesSegmentTimeLocators(t *testing.T) {
	raw := []byte("audio fixture")
	hash := sha256.Sum256(raw)
	profile := sha256.Sum256([]byte("profile"))
	deadline := time.Now().Add(time.Minute)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	result, err := (AudioParser{Store: audioBlobFixture{raw: raw}, Converter: audioConverterFixture{}, MaxBytes: 1 << 20}).Parse(ctx, contracts.ParseRequest{
		Request: contracts.RequestContext{RequestID: "request", Deadline: deadline, ProfileDigest: contracts.Digest(fmt.Sprintf("%x", profile))},
		Scope:   contracts.AccessScope{ExpiresAt: deadline}, DraftID: "draft", Input: contracts.BlobRef{ID: "blob", Size: int64(len(raw)), SHA256: contracts.Digest(fmt.Sprintf("%x", hash))}, Format: "wav", Options: contracts.ParseOptions{EnableOCR: true},
	})
	if err != nil || len(result.Blocks) != 2 {
		t.Fatalf("audio parse: blocks=%d err=%v", len(result.Blocks), err)
	}
	locator := result.Blocks[1].Content.Source.Locator
	if locator.StartMS == nil || locator.EndMS == nil || *locator.StartMS != 1200 || *locator.EndMS != 2400 {
		t.Fatalf("segment locator missing: %#v", locator)
	}
	if locator.Characters == nil || locator.Characters.Start != 5 || locator.Characters.End != 8 {
		t.Fatalf("segment character range missing: %#v", locator.Characters)
	}
}

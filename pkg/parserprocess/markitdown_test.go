package parserprocess

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Execute the test binary as a controlled child, without requiring Python or
// converter dependencies. Real format conversion is covered by the runtime probe.
func TestMain(m *testing.M) {
	if mode := os.Getenv("AKOS_PROCESS_TEST_CHILD"); mode != "" {
		switch mode {
		case "sleep":
			time.Sleep(10 * time.Second)
		case "overflow":
			fmt.Print(strings.Repeat("x", (5<<20)+1))
		case "oversized":
			fmt.Print(`{"version":"markitdown/0.1.3","markdown":"` + strings.Repeat("x", (4<<20)+1) + `"}`)
		case "invalid-utf8":
			os.Stdout.Write([]byte("{\"version\":\"markitdown/0.1.3\",\"markdown\":\"\xff\"}"))
		case "failure":
			fmt.Fprint(os.Stderr, "private vendor diagnostic")
			os.Exit(2)
		case "request":
			raw, _ := io.ReadAll(os.Stdin)
			if !strings.Contains(string(raw), `"format":"docx"`) || !strings.Contains(string(raw), `"sha256":`) || !strings.Contains(string(raw), `"data_base64":"eA=="`) {
				os.Exit(3)
			}
			fmt.Print(`{"version":"markitdown/0.1.3","markdown":"中文知识"}`)
		default:
			fmt.Print(os.Getenv("AKOS_PROCESS_TEST_OUTPUT"))
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func processFixture(t *testing.T, mode, output string) MarkItDown {
	t.Helper()
	t.Setenv("AKOS_PROCESS_TEST_CHILD", mode)
	t.Setenv("AKOS_PROCESS_TEST_OUTPUT", output)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return MarkItDown{Python: executable, Script: filepath.Join(t.TempDir(), "worker.py"), environment: func() []string {
		return append(converterEnvironment(), "AKOS_PROCESS_TEST_CHILD="+os.Getenv("AKOS_PROCESS_TEST_CHILD"), "AKOS_PROCESS_TEST_OUTPUT="+os.Getenv("AKOS_PROCESS_TEST_OUTPUT"))
	}}
}

func TestConverterEnvironmentDoesNotInheritCredentials(t *testing.T) {
	for _, key := range []string{"DEEPSEEK_API_KEY", "PGPASSWORD", "MEMORYS_DATABASE_URL", "OPENAI_API_KEY", "PYTHONPATH"} {
		t.Setenv(key, "private-parent-value")
	}
	for _, value := range converterEnvironment() {
		if strings.Contains(value, "private-parent-value") {
			t.Fatal("converter inherited a parent secret")
		}
	}
}

func TestConvertProcessBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, mode, output string
		valid              bool
	}{
		{"valid request", "request", "", true},
		{"duplicate", "output", `{"version":"wrong","version":"markitdown/0.1.3","markdown":"x"}`, false},
		{"unknown", "output", `{"version":"markitdown/0.1.3","markdown":"x","secret":"x"}`, false},
		{"trailing", "output", `{"version":"markitdown/0.1.3","markdown":"x"}{}`, false},
		{"empty", "output", `{"version":"markitdown/0.1.3","markdown":""}`, false},
		{"version", "output", `{"version":"markitdown/0.2","markdown":"x"}`, false},
		{"invalid UTF8", "invalid-utf8", "", false},
		{"oversized text", "oversized", "", false},
		{"nested", "output", `{"version":"markitdown/0.1.3","markdown":[]}`, false},
		{"overflow", "overflow", "", false},
		{"exit failure", "failure", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := processFixture(t, tc.mode, tc.output)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result, err := p.Convert(ctx, "docx", []byte("x"))
			if tc.valid {
				if err != nil || result.Text != "中文知识" {
					t.Fatalf("conversion: %+v %v", result, err)
				}
			} else if !errors.Is(err, ErrConversion) || result.Text != "" {
				t.Fatalf("unsafe result: %+v %v", result, err)
			}
		})
	}
}

func TestProcessDeadlineAndEntryValidation(t *testing.T) {
	p := processFixture(t, "sleep", "")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := p.Convert(ctx, "pdf", []byte("x")); !errors.Is(err, ErrConversion) || time.Since(started) > 3*time.Second {
		t.Fatalf("deadline: %v", err)
	}
	if _, err := p.Convert(context.Background(), "pdf", []byte("x")); !errors.Is(err, ErrConversion) {
		t.Fatal("unbounded call accepted")
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	p.Python = "python"
	if _, err := p.Convert(ctx2, "pdf", []byte("x")); !errors.Is(err, ErrConversion) {
		t.Fatal("relative entry accepted")
	}
	p = processFixture(t, "failure", "")
	if _, err := p.Convert(ctx2, "url", []byte("x")); !errors.Is(err, ErrConversion) {
		t.Fatal("unsupported format accepted")
	}
}

func TestHealthResponseBoundary(t *testing.T) {
	for _, output := range []string{`{"version":"markitdown/0.1.3","healthy":true}`, `{"version":"markitdown/0.1.3","healthy":false,"healthy":true}`, `{"version":"markitdown/0.1.3","healthy":false}`} {
		p := processFixture(t, "output", output)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := p.Health(ctx)
		cancel()
		if (err == nil) != (output == `{"version":"markitdown/0.1.3","healthy":true}`) {
			t.Fatalf("health response accepted incorrectly: %s %v", output, err)
		}
	}
}

func TestPyPDFPinnedProtocolAndFormat(t *testing.T) {
	p := processFixture(t, "output", `{"version":"pypdf/6.19.0","markdown":"PDF knowledge"}`)
	pdf := PyPDF{Python: p.Python, Script: p.Script, environment: p.environment}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := pdf.Convert(ctx, "pdf", []byte("x"))
	if err != nil || result.Version != "pypdf/6.19.0" {
		t.Fatalf("PDF protocol: %+v %v", result, err)
	}
	if _, err := pdf.Convert(ctx, "docx", []byte("x")); err == nil {
		t.Fatal("Office format admitted")
	}
	t.Setenv("AKOS_PROCESS_TEST_OUTPUT", `{"version":"markitdown/0.1.3","markdown":"x"}`)
	if _, err := pdf.Convert(ctx, "pdf", []byte("x")); err == nil {
		t.Fatal("wrong backend identity admitted")
	}
	t.Setenv("AKOS_PROCESS_TEST_OUTPUT", `{"version":"pypdf/6.19.0","healthy":true}`)
	if err := pdf.Health(ctx); err != nil {
		t.Fatal(err)
	}
}

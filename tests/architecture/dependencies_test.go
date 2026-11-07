package architecture_test

import (
	"crypto/sha256"
	"encoding/hex"
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

const module = "github.com/MemoryGet/memorys/"

// Explicit package inventory: new packages need a reviewed dependency policy.
// Tests/fakes can compose implementations; only non-test Go source is gated.
var localDependencies = map[string][]string{
	"pkg/mcpgateway":                   {"pkg/api", "pkg/canonical", "pkg/contracts"},
	"pkg/knowledge":                    {"pkg/contracts", "pkg/configprofile", "pkg/queryread", "pkg/fusion", "pkg/canonical", "pkg/publicationprojection", "pkg/clockhealth"},
	"pkg/clockhealth":                  {},
	"pkg/events":                       {"pkg/contracts"},
	"pkg/bootstrap":                    {"pkg/api", "pkg/events", "pkg/identitytoken", "pkg/configprofile", "pkg/kernel", "pkg/buildlock"},
	"pkg/buildlock":                    {"pkg/canonical"},
	"pkg/profileassembly":              {"pkg/contracts", "pkg/configprofile", "pkg/kernel"},
	"pkg/configprofile":                {"pkg/contracts", "pkg/canonical", "pkg/preparation"},
	"pkg/identitytoken":                {"pkg/contracts", "pkg/canonical"},
	"pkg/preparation":                  {"pkg/contracts", "pkg/document", "pkg/canonical", "pkg/indexsnapshot"},
	"cmd/architecture-freeze-draft":    {"pkg/canonical", "pkg/contracts", "pkg/document", "pkg/modulewire"},
	"cmd/architecture-office-parser":   {"pkg/contracts", "pkg/adapters", "pkg/kernel", "pkg/parserprocess", "pkg/storage", "pkg/document", "pkg/contentverify"},
	"pkg/parserprocess":                {"pkg/parser"},
	"cmd/architecture-pg-repositories": {"pkg/contracts", "pkg/postgres", "pkg/withdrawal", "pkg/cleanup", "pkg/adapters", "pkg/index", "pkg/kernel", "pkg/document", "pkg/ingestion", "pkg/parser", "pkg/storage", "pkg/preparation", "pkg/chunk", "pkg/modulewire", "pkg/queryread", "pkg/publicationprojection", "pkg/contentverify", "pkg/identitytoken", "pkg/configprofile", "pkg/profileassembly", "pkg/api", "pkg/knowledge", "pkg/fusion", "pkg/mcpgateway", "pkg/localruntime"},
	"pkg/cleanup":                      {"pkg/contracts", "pkg/canonical"},
	"pkg/withdrawal":                   {"pkg/contracts", "pkg/canonical"},
	"pkg/postgres":                     {"pkg/cleanup", "pkg/contracts", "pkg/indexsnapshot", "pkg/document", "pkg/preparation", "pkg/queryread", "pkg/publicationprojection", "pkg/contentverify", "pkg/identitytoken", "pkg/configprofile", "pkg/ingestion"},
	"pkg/indexsnapshot":                {"pkg/contracts", "pkg/canonical"},
	"pkg/adapters":                     {"pkg/canonical", "pkg/chunk", "pkg/contracts", "pkg/kernel", "pkg/parser", "pkg/cleanup", "pkg/index", "pkg/contentverify"},
	"cmd/memorys-server":               {"pkg/api", "pkg/bootstrap", "pkg/configprofile", "pkg/identitytoken", "pkg/localruntime", "pkg/postgres", "pkg/mcpgateway", "pkg/modelclient", "pkg/parseassembly", "pkg/publicationassembly"},
	"pkg/publicationassembly":          {"pkg/contracts", "pkg/localruntime", "pkg/preparation"},
	"pkg/parseassembly":                {"pkg/contracts", "pkg/ingestion", "pkg/localruntime", "pkg/parser", "pkg/parserprocess"},
	"pkg/localruntime":                 {"pkg/clockhealth", "pkg/configprofile", "pkg/contracts", "pkg/knowledge", "pkg/postgres", "pkg/publicationprojection", "pkg/queryread", "pkg/identitytoken", "pkg/contentverify", "pkg/document", "pkg/withdrawal", "pkg/ingestion", "pkg/storage", "pkg/chunk", "pkg/preparation"},
	"pkg/localcli":                     {"pkg/bootstrap", "pkg/localruntime", "pkg/contracts", "pkg/document", "pkg/configprofile", "pkg/identitytoken", "pkg/postgres", "pkg/preparation", "pkg/chunk", "pkg/adapters", "pkg/cleanup", "pkg/withdrawal", "pkg/contentverify", "pkg/parser", "pkg/parserprocess", "pkg/storage", "pkg/knowledge", "pkg/modelclient", "pkg/semantic", "pkg/fusion", "pkg/modulewire", "pkg/migration"},
	"pkg/modelclient":                  {"pkg/contracts", "pkg/canonical"},
	"pkg/semantic":                     {"pkg/contracts"},
	"cmd/memorys-local":                {"pkg/localcli"},
	"cmd/memorys-migrate":              {"pkg/migration"},
	"pkg/api":                          {"pkg/document", "pkg/contracts", "pkg/ingestion", "pkg/knowledge", "pkg/canonical", "pkg/websearch"},
	"pkg/chunk":                        {"pkg/contracts"},
	"pkg/document":                     {"pkg/contracts", "pkg/canonical"},
	"pkg/index":                        {"pkg/contracts", "pkg/indexsnapshot"},
	"pkg/ingestion":                    {"pkg/contracts", "pkg/document"},
	"pkg/parser":                       {"pkg/contracts"},
	"pkg/retrieval":                    {"pkg/contracts", "pkg/index"},
	"pkg/storage":                      {"pkg/contracts"},
	"pkg/websearch":                    {"pkg/contracts"},
	"pkg/migration":                    {},

	"cmd/architecture-graph-cleanup-digest": {"pkg/modulewire"},
	"cmd/architecture-memory-record":        {"pkg/modulewire"},
	"pkg/contracts":                         {},
	"pkg/canonical":                         {},
	"pkg/kernel":                            {"pkg/contracts"},
	"pkg/fusion":                            {"pkg/contracts", "pkg/kernel"}, // Local implementation's factory.
	"pkg/retrievalplan":                     {"pkg/contracts"},
	"pkg/reflection":                        {"pkg/contracts"},
	"pkg/queryread":                         {"pkg/contracts"},
	"pkg/contentverify":                     {"pkg/contracts", "pkg/canonical"},
	"pkg/publicationprojection":             {"pkg/contracts", "pkg/canonical"},
	"pkg/modulewire":                        {"pkg/contracts", "pkg/canonical"},
	"pkg/moduletransport":                   {"pkg/contracts", "pkg/modulewire"},
	"pkg/accessgrant":                       {"pkg/canonical"},
	"cmd/architecture-parser-wire":          {"pkg/contracts", "pkg/canonical", "pkg/modulewire"},
	"cmd/architecture-store-binding":        {"pkg/modulewire"},
	"cmd/architecture-store-probe":          {"pkg/modulewire"},
	"cmd/architecture-trust-record":         {"pkg/modulewire"},
	"cmd/architecture-projection-digest":    {"pkg/publicationprojection"},
}

var purePackages = map[string]bool{
	"pkg/knowledge":     true,
	"pkg/preparation":   true,
	"pkg/indexsnapshot": true,
	"pkg/cleanup":       true,
	"pkg/withdrawal":    true,
	"pkg/adapters":      true,
	"pkg/chunk":         true, "pkg/document": true, "pkg/ingestion": true, "pkg/index": true, "pkg/parser": true, "pkg/retrieval": true,
	"pkg/contracts": true, "pkg/canonical": true,
	"pkg/clockhealth":   true,
	"pkg/events":        true,
	"pkg/buildlock":     true,
	"pkg/retrievalplan": true, "pkg/reflection": true, "pkg/queryread": true,
	"pkg/publicationprojection": true,
	"pkg/contentverify":         true,
}

func allowedImport(owner, dependency string) bool {
	allowed, registered := localDependencies[owner]
	if !registered {
		return false
	}
	if strings.HasPrefix(dependency, module) {
		for _, local := range allowed {
			if dependency == module+local {
				return true
			}
		}
		return false
	}
	if (owner == "cmd/architecture-pg-repositories" || owner == "cmd/memorys-migrate" || owner == "cmd/memorys-server" || owner == "pkg/localcli") && dependency == "github.com/jackc/pgx/v5/stdlib" {
		return true
	}
	if (owner == "pkg/accessgrant" || owner == "pkg/identitytoken" || owner == "cmd/architecture-pg-repositories" || owner == "pkg/localcli") && dependency == "github.com/go-jose/go-jose/v4" {
		return true // Isolated cryptographic implementation; never exposed in contracts.
	}
	if owner == "pkg/localcli" && dependency == "golang.org/x/sys/windows" {
		return true
	} // Owner-only Windows filesystem DACL.
	if owner == "pkg/mcpgateway" && (dependency == "github.com/modelcontextprotocol/go-sdk/mcp" || dependency == "github.com/google/jsonschema-go/jsonschema") {
		return true
	}
	if dependency == "C" || dependency == "plugin" || dependency == "unsafe" {
		return false
	}
	if purePackages[owner] && (dependency == "net" || strings.HasPrefix(dependency, "net/") || strings.HasPrefix(dependency, "database/") || dependency == "os/exec") {
		return false
	}
	// Resolve against the actual Go standard library, not a heuristic that treats
	// every dot-free import as stdlib (which would allow arbitrary local paths).
	p, err := build.Default.Import(dependency, "", build.FindOnly)
	return err == nil && p.Goroot
}

// Unsafe remains forbidden by default. This exception permits only the exact
// reviewed Windows QoS wrapper in the local executable. Standard CRLF/LF
// checkout differences are normalized; content changes need another review.
func reviewedWindowsQoSImport(owner, filename, source, dependency string) bool {
	if owner != "cmd/memorys-local" || filepath.Base(filename) != "qos_windows.go" || dependency != "unsafe" {
		return false
	}
	digest := sha256.Sum256([]byte(strings.ReplaceAll(source, "\r\n", "\n")))
	return hex.EncodeToString(digest[:]) == "b0b7a5114a5001465bd1b2503296048630b0de2107ba6a33ce7cedce65705d2f"
}

func sourceViolations(owner, filename, source string) ([]string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), filename, source, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}
	var bad []string
	if _, ok := localDependencies[owner]; !ok {
		bad = append(bad, "unregistered package: "+owner)
	}
	for _, im := range f.Imports {
		path, err := strconv.Unquote(im.Path.Value)
		if err != nil {
			return nil, err
		}
		if !allowedImport(owner, path) && !reviewedWindowsQoSImport(owner, filename, source, path) {
			bad = append(bad, "forbidden import: "+path)
		}
	}
	return bad, nil
}

func TestProductionDependencyBoundaries(t *testing.T) {
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate architecture test")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	files := 0
	for _, tree := range []string{"pkg", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, tree), func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			owner, err := filepath.Rel(root, filepath.Dir(path))
			if err != nil {
				return err
			}
			owner = filepath.ToSlash(owner)
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			bad, err := sourceViolations(owner, path, string(raw))
			if err != nil {
				return err
			}
			files++
			for _, violation := range bad {
				t.Errorf("%s: %s", path, violation)
			}

			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if files == 0 {
		t.Fatal("no production files scanned")
	}
	t.Logf("checked %d non-test Go files including all build tags", files)
}

func TestDependencyGateRejectsBoundaryViolations(t *testing.T) {
	for _, tc := range []struct{ name, owner, source string }{
		{"vendor-type", "pkg/contracts", `package contracts; import sdk "github.com/Tencent/WeKnora/internal/types"`},
		{"sql-in-contract", "pkg/contracts", `package contracts; import "database/sql"`},
		{"http-in-application", "pkg/reflection", `package reflection; import "net/http"`},
		{"concrete-module", "pkg/retrievalplan", `package retrievalplan; import "github.com/MemoryGet/memorys/pkg/fusion"`},
		{"new-unreviewed-package", "pkg/newmodule", `package newmodule`},
		{"dot-free-not-stdlib", "pkg/contracts", `package contracts; import "unreviewed/sdk"`},
		{"blank-import", "pkg/contracts", `package contracts; import _ "github.com/MemoryGet/memorys/pkg/modulewire"`},
		{"crypto-leak", "pkg/contracts", `package contracts; import "github.com/go-jose/go-jose/v4"`},
		{"dynamic-plugin", "pkg/kernel", `package kernel; import "plugin"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad, err := sourceViolations(tc.owner, "fixture.go", tc.source)
			if err != nil || len(bad) == 0 {
				t.Fatalf("gate failed: %v, %v", bad, err)
			}
		})
	}
}

func TestDependencyGateAllowsPortsAndIsolatedSDK(t *testing.T) {
	for _, tc := range []struct{ owner, source string }{
		{"pkg/contracts", `package contracts; import "context"; import "math/big"`},
		{"pkg/reflection", `package reflection; import "github.com/MemoryGet/memorys/pkg/contracts"`},
		{"pkg/accessgrant", `package accessgrant; import "github.com/go-jose/go-jose/v4"`},
		{"pkg/moduletransport", `package moduletransport; import "net/http"; import "github.com/MemoryGet/memorys/pkg/modulewire"`},
	} {
		bad, err := sourceViolations(tc.owner, "fixture.go", tc.source)
		if err != nil || len(bad) != 0 {
			t.Fatalf("valid boundary rejected: %s: %v, %v", tc.owner, bad, err)
		}
	}
}

// Exercise the exception with the reviewed bytes, then mutate its owner,
// filename, body and import. No unrelated native operation becomes admissible.
func TestWindowsQoSExceptionIsExactAndLocal(t *testing.T) {
	const source = "package main\n\nimport (\n\t\"runtime\"\n\t\"syscall\"\n\t\"unsafe\"\n)\n\n// requestWebHighQoS affects only this process. Windows may still choose its\n// processors; failure leaves the OS-managed default and does not prevent start.\nfunc requestWebHighQoS() bool {\n\tproc := syscall.NewLazyDLL(\"kernel32.dll\").NewProc(\"SetProcessInformation\")\n\tif proc.Find() != nil {\n\t\treturn false\n\t}\n\t// PROCESS_POWER_THROTTLING_STATE: disable execution-speed throttling.\n\tstate := struct{ version, controlMask, stateMask uint32 }{1, 1, 0}\n\tresult, _, _ := proc.Call(^uintptr(0), 4, uintptr(unsafe.Pointer(&state)), unsafe.Sizeof(state))\n\truntime.KeepAlive(&state)\n\treturn result != 0\n}\n"
	for _, tc := range []struct {
		name, owner, filename, body, dependency string
		want                                    bool
	}{
		{"reviewed", "cmd/memorys-local", "qos_windows.go", source, "unsafe", true},
		{"windows-line-endings", "cmd/memorys-local", "qos_windows.go", strings.ReplaceAll(source, "\n", "\r\n"), "unsafe", true},
		{"other-file", "cmd/memorys-local", "other_windows.go", source, "unsafe", false},
		{"other-package", "pkg/localcli", "qos_windows.go", source, "unsafe", false},
		{"changed-body", "cmd/memorys-local", "qos_windows.go", source + "\n// change", "unsafe", false},
		{"other-native-import", "cmd/memorys-local", "qos_windows.go", source, "C", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := reviewedWindowsQoSImport(tc.owner, tc.filename, tc.body, tc.dependency); got != tc.want {
				t.Fatalf("native boundary got %v, want %v", got, tc.want)
			}
		})
	}
	if allowedImport("cmd/memorys-local", "unsafe") {
		t.Fatal("unsafe was allowed without its reviewed source boundary")
	}
	bad, err := sourceViolations("cmd/memorys-local", "qos_windows.go", source)
	if err != nil || len(bad) != 0 {
		t.Fatalf("reviewed source rejected: %v, %v", bad, err)
	}
	bad, err = sourceViolations("cmd/memorys-local", "qos_windows.go", source+"\n// change")
	if err != nil || len(bad) == 0 {
		t.Fatalf("changed source accepted: %v, %v", bad, err)
	}
}

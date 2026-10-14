// Offline projection inspector, not an authorization/publication service.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/MemoryGet/memorys/pkg/publicationprojection"
)

func main() {
	validated := len(os.Args) == 2 && os.Args[1] == "--validate"
	if len(os.Args) > 1 && !validated {
		fmt.Fprintln(os.Stderr, "invalid inspector option")
		os.Exit(1)
	}
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, (4<<20)+1))
	if err == nil && len(raw) <= 4<<20 {
		digest, hashErr := publicationprojection.DigestUnchecked(raw)
		if validated {
			_, digest, hashErr = publicationprojection.ValidatedDigest(raw)
		}
		if hashErr == nil {
			if json.NewEncoder(os.Stdout).Encode(map[string]any{"digest": digest, "schema_validated": validated, "authorized": false}) == nil {
				return
			}
		}
	}
	fmt.Fprintln(os.Stderr, "invalid publication projection digest input")
	os.Exit(1)
}

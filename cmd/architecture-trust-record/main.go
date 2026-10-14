// Offline record mapping inspector; does not import metadata or authorize trust.
package main

import (
	"fmt"
	"github.com/MemoryGet/memorys/pkg/modulewire"
	"io"
	"os"
)

func main() {
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, (4<<20)+1))
	if err == nil && len(raw) <= 4<<20 {
		row, decodeErr := modulewire.DecodeTrustHeadRecord(raw)
		if decodeErr == nil {
			data, encodeErr := modulewire.EncodeTrustHeadRecord(row)
			if encodeErr == nil {
				if _, writeErr := os.Stdout.Write(data); writeErr == nil {
					return
				}
			}
		}
	}
	fmt.Fprintln(os.Stderr, "invalid trust head record")
	os.Exit(1)
}

// Offline bound-probe codec. now_ns is a test input, not a trusted clock proof.
package main

import (
	"fmt"
	"github.com/MemoryGet/memorys/pkg/modulewire"
	"io"
	"os"
	"strconv"
	"time"
)

func main() {
	if len(os.Args) == 2 {
		n, err := strconv.ParseInt(os.Args[1], 10, 64)
		if err == nil && n > 0 {
			now := time.Unix(0, n)
			raw, err := io.ReadAll(io.LimitReader(os.Stdin, (4<<20)+1))
			if err == nil && len(raw) <= 4<<20 {
				e, r, a, err := modulewire.DecodeBoundStoreProbeReview(raw, now)
				if err == nil {
					encoded, err := modulewire.EncodeBoundStoreProbeReview(e, r, a, now)
					if err == nil {
						if _, err := os.Stdout.Write(encoded); err == nil {
							return
						}
					}
				}
			}
		}
	}
	fmt.Fprintln(os.Stderr, "invalid store probe review")
	os.Exit(1)
}

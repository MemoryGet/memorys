// Offline codec inspector; performs no endpoint probing or activation.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/MemoryGet/memorys/pkg/modulewire"
)

func main() {
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, (4<<20)+1))
	if err == nil && len(raw) <= 4<<20 {
		expected, observations, err := modulewire.DecodeStoreBindingReview(raw)
		if err == nil {
			encoded, err := modulewire.EncodeStoreBindingReview(expected, observations)
			if err == nil {
				if _, err := os.Stdout.Write(encoded); err == nil {
					return
				}
			}
		}
	}
	fmt.Fprintln(os.Stderr, "invalid store binding review")
	os.Exit(1)
}

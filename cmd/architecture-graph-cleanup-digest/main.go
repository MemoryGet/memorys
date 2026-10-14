package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/MemoryGet/memorys/pkg/modulewire"
)

func main() {
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, (4<<20)+1))
	if err == nil {
		value, decodeErr := modulewire.DecodeGraphCleanupSemantic(raw)
		err = decodeErr
		if err == nil {
			digest, digestErr := modulewire.GraphCleanupRequestDigest(value)
			err = digestErr
			if err == nil {
				err = json.NewEncoder(os.Stdout).Encode(map[string]any{"digest": digest, "source_authenticated": false})
			}
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid graph cleanup semantic")
		os.Exit(1)
	}
}

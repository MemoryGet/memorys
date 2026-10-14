package main

import (
	"fmt"
	"io"
	"os"

	"github.com/MemoryGet/memorys/pkg/modulewire"
)

func main() {
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, (4<<20)+1))
	if err == nil {
		var out []byte
		r, decodeErr := modulewire.DecodeMemoryRecord(raw)
		err = decodeErr
		if err == nil {
			out, err = modulewire.EncodeMemoryRecord(r)
		}
		if err == nil {
			_, err = os.Stdout.Write(out)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid memory record")
		os.Exit(1)
	}
}

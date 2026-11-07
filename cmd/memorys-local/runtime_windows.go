package main

import (
	"os"
	"runtime"
)

// The local web host admits at most four active requests. Limit default Go
// parallelism on Windows to avoid extra runtime scheduling and GC workers
// competing with the database. Keep explicit operator settings and smaller
// runtime limits intact.
func configureWebParallelism() {
	if os.Getenv("GOMAXPROCS") == "" && runtime.GOMAXPROCS(0) > 4 {
		runtime.GOMAXPROCS(4)
	}
}

package retrieval

import (
	"context"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

var ErrBenchmark = errors.New("invalid retrieval benchmark")

// BenchmarkObservation is supplied by the measured adapter. CostUnits is an
// opaque platform meter (for example candidate bytes or provider call units);
// the harness never converts it into money.
type BenchmarkObservation struct {
	CandidateBytes uint64
	CostUnits      uint64
}

type BenchmarkFunc func(context.Context, int) (BenchmarkObservation, error)

type BenchmarkConfig struct {
	Requests       int
	Concurrency    int
	Warmup         int
	RequestTimeout time.Duration
}

type BenchmarkReport struct {
	Requests        int
	Completed       int
	Failed          int
	TimedOut        int
	PeakConcurrency int
	CandidateBytes  uint64
	CostUnits       uint64
	P50             time.Duration
	P95             time.Duration
	P99             time.Duration
}

func validBenchmarkConfig(c BenchmarkConfig) bool {
	return c.Requests >= 1 && c.Requests <= 10000 && c.Concurrency >= 1 && c.Concurrency <= 128 && c.Concurrency <= c.Requests && c.Warmup >= 0 && c.Warmup <= 100 && c.RequestTimeout >= time.Millisecond && c.RequestTimeout <= time.Minute
}

func RunBenchmark(ctx context.Context, c BenchmarkConfig, run BenchmarkFunc) (BenchmarkReport, error) {
	var report BenchmarkReport
	if ctx == nil || run == nil || !validBenchmarkConfig(c) {
		return report, ErrBenchmark
	}
	if _, ok := ctx.Deadline(); !ok {
		return report, ErrBenchmark
	}
	for i := 0; i < c.Warmup; i++ {
		warmCtx, cancel := context.WithTimeout(ctx, c.RequestTimeout)
		_, err := run(warmCtx, -1)
		cancel()
		if err != nil || ctx.Err() != nil {
			if ctx.Err() != nil {
				return report, ctx.Err()
			}
			return report, err
		}
	}

	type result struct {
		latency  time.Duration
		obs      BenchmarkObservation
		err      error
		timedOut bool
	}
	jobs := make(chan int)
	results := make(chan result, c.Requests)
	var active, peak atomic.Int32
	var workers sync.WaitGroup
	for worker := 0; worker < c.Concurrency; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case index, ok := <-jobs:
					if !ok {
						return
					}
					started := time.Now()
					current := active.Add(1)
					for old := peak.Load(); current > old && !peak.CompareAndSwap(old, current); old = peak.Load() {
					}
					requestCtx, cancel := context.WithTimeout(ctx, c.RequestTimeout)
					obs, err := run(requestCtx, index)
					timedOut := errors.Is(requestCtx.Err(), context.DeadlineExceeded)
					cancel()
					active.Add(-1)
					results <- result{latency: time.Since(started), obs: obs, err: err, timedOut: timedOut}
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for i := 0; i < c.Requests; i++ {
			select {
			case <-ctx.Done():
				return
			case jobs <- i:
			}
		}
	}()
	go func() {
		workers.Wait()
		close(results)
	}()

	latencies := make([]time.Duration, 0, c.Requests)
	for item := range results {
		if item.timedOut || errors.Is(item.err, context.DeadlineExceeded) {
			report.TimedOut++
		} else if item.err != nil {
			report.Failed++
		} else {
			report.Completed++
			report.CandidateBytes += item.obs.CandidateBytes
			report.CostUnits += item.obs.CostUnits
		}
		latencies = append(latencies, item.latency)
	}
	if ctx.Err() != nil {
		return report, ctx.Err()
	}
	report.Requests = len(latencies)
	report.PeakConcurrency = int(peak.Load())
	if report.Requests == 0 {
		return report, ErrBenchmark
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	report.P50 = percentile(latencies, 50)
	report.P95 = percentile(latencies, 95)
	report.P99 = percentile(latencies, 99)
	return report, nil
}

func percentile(values []time.Duration, p int) time.Duration {
	if len(values) == 0 {
		return 0
	}
	index := (len(values)*p + 99) / 100
	if index < 1 {
		index = 1
	}
	if index > len(values) {
		index = len(values)
	}
	return values[index-1]
}

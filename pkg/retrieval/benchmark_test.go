package retrieval_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/retrieval"
)

func benchmarkContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestRunBenchmarkBoundsConcurrencyAndReportsQuantiles(t *testing.T) {
	var active, peak atomic.Int32
	report, err := retrieval.RunBenchmark(benchmarkContext(t), retrieval.BenchmarkConfig{Requests: 12, Concurrency: 3, RequestTimeout: time.Second}, func(context.Context, int) (retrieval.BenchmarkObservation, error) {
		n := active.Add(1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		time.Sleep(time.Millisecond)
		active.Add(-1)
		return retrieval.BenchmarkObservation{CandidateBytes: 10, CostUnits: 2}, nil
	})
	if err != nil || report.Requests != 12 || report.Completed != 12 || report.Failed != 0 || report.TimedOut != 0 || report.PeakConcurrency > 3 || peak.Load() > 3 || report.CandidateBytes != 120 || report.CostUnits != 24 || report.P50 == 0 || report.P95 == 0 || report.P99 == 0 {
		t.Fatalf("unexpected benchmark report: %+v err=%v peak=%d", report, err, peak.Load())
	}
}

func TestRunBenchmarkSeparatesFailuresAndTimeouts(t *testing.T) {
	index := 0
	report, err := retrieval.RunBenchmark(benchmarkContext(t), retrieval.BenchmarkConfig{Requests: 3, Concurrency: 1, RequestTimeout: 5 * time.Millisecond}, func(ctx context.Context, _ int) (retrieval.BenchmarkObservation, error) {
		index++
		switch index {
		case 1:
			return retrieval.BenchmarkObservation{CandidateBytes: 100, CostUnits: 1}, nil
		case 2:
			return retrieval.BenchmarkObservation{}, errors.New("adapter failure")
		default:
			<-ctx.Done()
			return retrieval.BenchmarkObservation{}, ctx.Err()
		}
	})
	if err != nil || report.Requests != 3 || report.Completed != 1 || report.Failed != 1 || report.TimedOut != 1 || report.CandidateBytes != 100 || report.CostUnits != 1 {
		t.Fatalf("failure/timeout classification wrong: %+v err=%v", report, err)
	}
}

func TestRunBenchmarkRejectsUnboundedOrMissingDeadline(t *testing.T) {
	for _, ctx := range []context.Context{context.Background(), nil} {
		if _, err := retrieval.RunBenchmark(ctx, retrieval.BenchmarkConfig{Requests: 1, Concurrency: 1, RequestTimeout: time.Second}, func(context.Context, int) (retrieval.BenchmarkObservation, error) {
			return retrieval.BenchmarkObservation{}, nil
		}); !errors.Is(err, retrieval.ErrBenchmark) {
			t.Fatal("invalid benchmark context accepted", err)
		}
	}
	if _, err := retrieval.RunBenchmark(benchmarkContext(t), retrieval.BenchmarkConfig{Requests: 0, Concurrency: 1, RequestTimeout: time.Second}, func(context.Context, int) (retrieval.BenchmarkObservation, error) {
		return retrieval.BenchmarkObservation{}, nil
	}); !errors.Is(err, retrieval.ErrBenchmark) {
		t.Fatal("invalid benchmark budget accepted", err)
	}
}

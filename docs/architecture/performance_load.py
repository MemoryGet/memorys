"""Bounded load scheduling and independently checkable performance gates."""
import concurrent.futures as futures
import math
import threading
import time

from run_local_performance import quantile, summarize


def gate(samples, *, overload=False):
    if not samples:
        return 'inconclusive'
    if any(s['outcome'] in ('cancelled', 'client_capacity_exceeded') for s in samples):
        return 'fail'
    if overload:
        return 'pass' if all(s['outcome'] in ('success', 'rejected_429') for s in samples) else 'fail'
    failed = sum(s['outcome'] != 'success' for s in samples)
    return 'pass' if (quantile([s['elapsed_ms'] for s in samples], 95) <= 2000
                      and failed / len(samples) <= .001
                      and not any(s['outcome'] == 'invalid_result' for s in samples)) else 'fail'


def rate_slots(rate, duration):
    if not 0 < rate <= 16 or not 0 < duration <= 8 * 3600:
        raise ValueError('bounded positive rate/duration required')
    # A half-open interval: no extra event at duration itself.
    return math.ceil(rate * duration - 1e-10)


def closed_loop(call, *, count, workers, stop=None, on_sample=None):
    """Issue only the next request per worker; cancellation never invents a queue."""
    if not 1 <= count <= 10000 or not 1 <= workers <= 16:
        raise ValueError('invalid bounded count/workers')
    stop = stop or threading.Event()
    rows = []
    lock = threading.Lock()
    next_sequence = 0
    started = time.perf_counter()

    def worker():
        nonlocal next_sequence
        while True:
            with lock:
                if stop.is_set() or next_sequence >= count:
                    return
                n = next_sequence
                next_sequence += 1
            begun = time.perf_counter()
            try:
                row = call(n)
            except Exception as error:
                row = dict(sequence=n, outcome='transport_error', error_type=type(error).__name__,
                           elapsed_ms=(time.perf_counter() - begun) * 1000)
            with lock:
                rows.append(row)
            if on_sample:
                try:
                    on_sample(row)
                except Exception as error:
                    stop.set()
                    raise RuntimeError('sample persistence failed') from error

    with futures.ThreadPoolExecutor(max_workers=workers) as pool:
        jobs = [pool.submit(worker) for _ in range(workers)]
        for job in jobs:
            job.result()
    return rows, time.perf_counter() - started


def open_loop(call, *, rate, duration, workers=16, max_pending=32, stop=None, on_sample=None):
    """Keep arrival times fixed even if workers fall behind; never hide drops.

    call receives (sequence, scheduled_monotonic). A bounded semaphore covers
    running plus queued work. End-to-end latency is measured from scheduled
    arrival, so client queueing and scheduler lag remain in the distribution.
    """
    if workers < 1 or max_pending < workers or max_pending > 128:
        raise ValueError('invalid bounded workers/pending')
    count = rate_slots(rate, duration)
    stop = stop or threading.Event()
    slots = threading.BoundedSemaphore(max_pending)
    rows = []
    persistence_errors = []
    lock = threading.Lock()
    start = time.perf_counter()

    def store(row):
        with lock:
            rows.append(row)
        if on_sample:
            try:
                on_sample(row)
            except Exception as error:
                with lock:
                    persistence_errors.append(type(error).__name__)
                stop.set()

    def execute(n, scheduled):
        try:
            begun = time.perf_counter()
            try:
                row = call(n, scheduled)
            except Exception as error:
                row = dict(sequence=n, outcome='transport_error', error_type=type(error).__name__)
            row['scheduled_monotonic_seconds'] = scheduled
            row['client_queue_ms'] = max(0, (begun - scheduled) * 1000)
            row['elapsed_ms'] = max(0, (time.perf_counter() - scheduled) * 1000)
            store(row)
        finally:
            slots.release()

    with futures.ThreadPoolExecutor(max_workers=workers) as pool:
        for n in range(count):
            scheduled = start + n / rate
            if stop.wait(max(0, scheduled - time.perf_counter())):
                break
            if not slots.acquire(blocking=False):
                store(dict(sequence=n, outcome='client_capacity_exceeded',
                           scheduled_monotonic_seconds=scheduled, client_queue_ms=0,
                           elapsed_ms=max(0, (time.perf_counter() - scheduled) * 1000)))
                continue
            pool.submit(execute, n, scheduled)
        stop.wait(max(0, start + duration - time.perf_counter()))
    if persistence_errors:
        raise RuntimeError('sample persistence failed: ' + persistence_errors[0])
    return rows, time.perf_counter() - start, count


def session_renewal_wait(stamp, earliest_expiry, latest_expiry):
    """Use bounds observed around session issuance, without changing its TTL."""
    if earliest_expiry > latest_expiry:
        raise ValueError('invalid issuance bounds')
    if stamp < earliest_expiry - 1:
        return None
    return max(0, latest_expiry + .1 - stamp)


def clock_divergence_ms(samples):
    return max((abs((b['wall_epoch'] - a['wall_epoch']) - (b['time'] - a['time'])) * 1000
                for a, b in zip(samples, samples[1:])), default=0)


def describe(samples, elapsed):
    out = summarize(samples, elapsed)
    out['client_capacity_exceeded'] = sum(s['outcome'] == 'client_capacity_exceeded' for s in samples)
    out['client_queue_p95_ms'] = quantile([s.get('client_queue_ms', 0) for s in samples], 95)
    return out


def evidence_valid(data, mode, kind, target, allowed, known):
    if not isinstance(data, dict) or not isinstance(data.get('evidence'), list):
        return False
    items = data['evidence']
    if len(items) > 10:
        return False
    seen, bindings = set(), set()
    for rank, item in enumerate(items, 1):
        if not isinstance(item, dict) or not isinstance(item.get('citation'), dict):
            return False
        cite = item['citation']
        doc = cite.get('document_id')
        if not isinstance(doc, str) or doc not in allowed or doc in seen:
            return False
        expected = known[doc]
        if (item.get('source_excerpt') != expected['text'] or cite.get('source_kind') != 'revision'
                or cite.get('revision_id') != expected['revision'] or cite.get('block_id') != expected['block']
                or type(item.get('rank')) is not int or item['rank'] != rank):
            return False
        if mode == 'semantic':
            score = item.get('score')
            if type(score) not in (int, float) or not math.isfinite(score) or not -1 <= score <= 1:
                return False
        seen.add(doc)
        bindings.add((doc, expected['revision'], expected['block'], 'revision'))
    if mode == 'keyword':
        return (target in seen) if kind < 2 else len(items) == (min(len(allowed), 10) if kind == 2 else 0)
    if mode == 'semantic':
        return len(items) == min(len(allowed), 10)
    cites = data.get('citations')
    if not 1<=len(items)<=4 or data.get('status') != 'answered' or not data.get('answer') or not isinstance(cites, list) or not cites:
        return False
    return all(isinstance(c, dict) and (c.get('document_id'), c.get('revision_id'), c.get('block_id'), c.get('source_kind')) in bindings for c in cites)

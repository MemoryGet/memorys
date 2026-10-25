import threading
import copy
import collections
import time
import unittest

from performance_load import closed_loop, clock_divergence_ms, describe, evidence_valid, gate, open_loop, rate_slots, session_renewal_wait


class PerformanceLoadTests(unittest.TestCase):
    def test_closed_loop_cancellation_does_not_invent_queued_samples(self):
        stop = threading.Event()
        issued = []
        def call(n):
            issued.append(n)
            return dict(sequence=n, outcome='success', elapsed_ms=1)
        rows, _ = closed_loop(call, count=1000, workers=1, stop=stop, on_sample=lambda _: stop.set())
        self.assertEqual(issued, [0])
        self.assertEqual(len(rows), 1)
        rows, _ = closed_loop(call, count=1000, workers=4, stop=stop)
        self.assertEqual(rows, [])

    def test_closed_loop_records_worker_failures_and_rejects_storage_failure(self):
        def fail(_):
            raise OSError('controlled worker failure')
        rows, _ = closed_loop(fail, count=9, workers=3)
        self.assertEqual(sorted(r['sequence'] for r in rows), list(range(9)))
        self.assertTrue(all(r['outcome']=='transport_error' for r in rows))
        def broken(_):
            raise OSError('controlled storage failure')
        with self.assertRaisesRegex(RuntimeError, 'sample persistence failed'):
            closed_loop(lambda n: dict(sequence=n, outcome='success', elapsed_ms=1),
                        count=1000, workers=1, on_sample=broken)

    def test_cancelled_requests_do_not_trigger_consecutive_failure_trip(self):
        from run_formal_performance import Acceptance
        job = Acceptance.__new__(Acceptance)
        job.lock, job.stop = threading.RLock(), threading.Event()
        job.stop.set()
        job.raw = lambda *_: None
        job.current_cell, job.samples_in_cell = 'controlled-stop', 0
        job.outcomes = collections.Counter()
        job.consecutive_failures = job.current_rate = 0
        job.report = dict(decision='running')
        for _ in range(12):
            job.record(dict(outcome='cancelled', elapsed_ms=0))
        self.assertEqual(job.report['decision'], 'running')
        self.assertEqual(job.consecutive_failures, 0)

    def test_gate_never_counts_rejection_as_normal_success(self):
        ok = dict(outcome='success', elapsed_ms=10)
        rejected = dict(outcome='rejected_429', elapsed_ms=1)
        self.assertEqual(gate([ok, rejected]), 'fail')
        self.assertEqual(gate([ok, rejected], overload=True), 'pass')
        self.assertEqual(gate([dict(outcome='transport_error', elapsed_ms=1)], overload=True), 'fail')
        self.assertEqual(gate([dict(outcome='success', elapsed_ms=2100)]), 'fail')

    def test_correctness_failure_is_fatal_even_below_error_budget(self):
        rows = [dict(outcome='success', elapsed_ms=1)] * 1000
        rows.append(dict(outcome='invalid_result', elapsed_ms=1))
        self.assertEqual(gate(rows), 'fail')

    def test_half_open_arrival_schedule(self):
        self.assertEqual(rate_slots(1, 60), 60)
        self.assertEqual(rate_slots(2, 10), 20)
        self.assertEqual(rate_slots(.5, 3), 2)
        for rate, duration in ((0, 1), (17, 1), (1, 0), (1, 28801)):
            with self.assertRaises(ValueError):
                rate_slots(rate, duration)

    def test_slow_workers_do_not_slow_the_arrival_clock(self):
        entered = threading.Event()
        release = threading.Event()
        rows_holder = []

        def call(n, scheduled):
            entered.set()
            release.wait(2)
            return dict(outcome='success', elapsed_ms=0, sequence=n)

        def run():
            rows_holder.append(open_loop(call, rate=16, duration=.3, workers=1, max_pending=1))

        runner = threading.Thread(target=run)
        runner.start()
        self.assertTrue(entered.wait(1))
        time.sleep(.4)
        release.set()
        runner.join(2)
        self.assertFalse(runner.is_alive())
        rows, elapsed, expected = rows_holder[0]
        self.assertEqual(len(rows), expected)
        self.assertEqual(expected, 5)
        self.assertEqual(sum(r['outcome'] == 'client_capacity_exceeded' for r in rows), 4)
        successful = next(r for r in rows if r['outcome'] == 'success')
        self.assertGreater(successful['elapsed_ms'], 300)
        self.assertEqual(gate(rows), 'fail')
        self.assertEqual(describe(rows, elapsed)['client_capacity_exceeded'], 4)

    def test_cancellation_does_not_invent_unissued_samples(self):
        stop = threading.Event()
        stop.set()
        rows, _, expected = open_loop(lambda *_: None, rate=1, duration=1, stop=stop)
        self.assertEqual(rows, [])
        self.assertEqual(expected, 1)

    def test_worker_exception_is_a_failed_sample_not_a_missing_request(self):
        def fail(*_):
            raise OSError('controlled worker failure')
        rows, _, expected = open_loop(fail, rate=16, duration=.01, workers=1, max_pending=1)
        self.assertEqual(expected, 1)
        self.assertEqual(len(rows), 1)
        self.assertEqual(rows[0]['outcome'], 'transport_error')
        self.assertEqual(gate(rows), 'fail')

    def test_sample_storage_failure_cannot_become_a_pass(self):
        def broken_writer(_):
            raise OSError('controlled storage failure')
        with self.assertRaisesRegex(RuntimeError, 'sample persistence failed'):
            open_loop(lambda *_: dict(outcome='success'), rate=16, duration=.01,
                      workers=1, max_pending=1, on_sample=broken_writer)

    def test_session_renewal_waits_until_latest_observed_expiry(self):
        self.assertIsNone(session_renewal_wait(100, 7200, 7200.2))
        self.assertAlmostEqual(session_renewal_wait(7199, 7200, 7200.2), 1.3)
        self.assertEqual(session_renewal_wait(7300, 7200, 7200.2), 0)
        with self.assertRaises(ValueError):
            session_renewal_wait(100, 102, 101)

    def test_wall_step_cannot_be_mistaken_for_a_continuous_soak(self):
        stable = [dict(time=1,wall_epoch=101),dict(time=6,wall_epoch=106)]
        self.assertEqual(clock_divergence_ms(stable),0)
        self.assertEqual(clock_divergence_ms(stable+[dict(time=11,wall_epoch=115)]),4000)

    def test_exact_evidence_binding_and_answer_reference_are_required(self):
        known={'doc':dict(text='真实正文',revision='revision',block='block')}
        cite=dict(document_id='doc',revision_id='revision',block_id='block',source_kind='revision')
        data=dict(evidence=[dict(citation=cite,source_excerpt='真实正文',rank=1,score=.5)],
                  answer='答案[1]',status='answered',citations=[cite])
        valid=lambda d,m='semantic':evidence_valid(d,m,0,'doc',{'doc'},known)
        self.assertTrue(valid(data))
        self.assertTrue(valid(data,'answer'))
        for field in ('document_id','revision_id','block_id','source_kind'):
            bad=copy.deepcopy(data)
            bad['evidence'][0]['citation'][field]='other'
            self.assertFalse(valid(bad))
        for field,value in (('source_excerpt','编造正文'),('rank',True),('score',float('nan'))):
            bad=copy.deepcopy(data)
            bad['evidence'][0][field]=value
            self.assertFalse(valid(bad))
        bad=copy.deepcopy(data)
        bad['citations']=[dict(bad['citations'][0],block_id='invented-block')]
        self.assertFalse(valid(bad,'answer'))
        self.assertFalse(valid(dict(evidence=[]),'keyword'))
        self.assertTrue(evidence_valid(dict(evidence=[]),'keyword',3,'doc',{'doc'},known))


if __name__ == '__main__':
    unittest.main()

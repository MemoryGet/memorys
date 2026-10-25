"""Durable, bounded local retrieval acceptance; only owned disposable resources."""
import argparse
import collections
import datetime as dt
import hashlib
import http.client
import json
import os
from pathlib import Path
import platform
import shutil
import signal
import socket
import statistics
import subprocess
import threading
import time
import urllib.request

import psutil
from check_architecture import ROOT, source_inventory
from performance_load import closed_loop, clock_divergence_ms, describe, evidence_valid, gate, open_loop, session_renewal_wait


class Halt(Exception):
    pass


def now():
    return dt.datetime.now(dt.timezone.utc).isoformat()


def asset_inventory():
    root = ROOT / 'dashboard/dist'
    return {p.relative_to(root).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest()
            for p in root.rglob('*') if p.is_file()}


def application_inventory(inventory):
    return {k: v for k, v in inventory.items() if k.endswith('.go') or k in ('go.mod', 'go.sum')}


class Acceptance:
    def __init__(self, args):
        self.args = args
        self.root = args.output.resolve()
        self.fixture = self.root / 'owned-fixture'
        self.runtime = self.fixture / 'runtime'
        self.binary = self.fixture / 'memorys-local.exe'
        self.source = source_inventory()
        self.assets = asset_inventory()
        self.stop = threading.Event()
        self.started_monotonic = time.perf_counter()
        self.started_wall = time.time()
        self.previous_power_state = None
        self.finished = threading.Event()
        self.lock = threading.RLock()
        self.session_lock = threading.Lock()
        self.web = self.ollama = None
        self.pg_attempted = False
        self.origin = self.cookie = None
        self.session_lower = self.session_upper = 0
        self.known = {}
        self.published = []
        self.scale = 0
        self.samples_in_cell = 0
        self.outcomes = collections.Counter()
        self.consecutive_failures = 0
        self.current_rate = 0
        self.current_cell = None
        self.monitor_samples = []
        self.db_samples = []
        self.active_db = 'postgres'
        self.thread = None
        self.env = {k: v for k, v in os.environ.items() if not k.startswith(('PG', 'MEMORYS_'))}
        self.env.update(PGHOST='127.0.0.1', PGPORT=str(args.port), PGUSER='postgres', PGDATABASE='postgres',
                        MEMORYS_DATABASE_URL=self.url('postgres'), MEMORYS_CHAT_MODEL='deepseek-flash')
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        self.report = dict(schema_version='memorys.formal-retrieval-performance/1',
            started_at=now(), decision='running', smoke=args.smoke,
            production_acceptance=False, local_performance_acceptance=False,
            local_retrieval_performance_acceptance=False,
            formal_load_acceptance=False, nominal_rate_acceptance=False, soak_acceptance=False,
            source_inventory=self.source, frontend_inventory=self.assets,
            environment=dict(platform=platform.platform(), cpu=platform.processor(),
                             ram_bytes=psutil.virtual_memory().total, logical_cpus=psutil.cpu_count(),
                             python=platform.python_version(), psutil=psutil.__version__,
                             http_client='HTTP/1.1 complete-request single write; fresh connection; no retries'),
            plan=dict(scales=[32] if args.smoke else [500, 100, 32],
                      requests_per_cell=4 if args.smoke else 1000,
                      repetitions=1 if args.smoke else 3, modes=['semantic', 'keyword'],
                      concurrency=[4, 2, 1], arrival_rates=[1, 2, 3],
                      rate_seconds=2 if args.smoke else 300,
                      soak_hours=[] if args.smoke else [2, 8],
                      soak_rate=1, remote_answer_calls_max=0 if args.smoke else 6,
                      fail_fast=True, maximum_wall_hours=24),
            thresholds=dict(retrieval_p95_ms=2000, unexpected_error_rate=.001,
                            correctness_errors=0, nominal_429_rate_max=.001,
                            soak_rss_median_growth_bytes=64 << 20, soak_rss_relative_growth=.5,
                            soak_handle_median_growth_max=32, maximum_client_connections=10,
                            maximum_resource_sample_gap_seconds=30, consecutive_unexpected_failures_max=10,
                            maximum_wall_monotonic_divergence_ms=250,
                            resource_comparison_windows='minutes10-20 vs last10minutes'),
            setup=[], cells=[], rate_cells=[], soaks=[], controls=[], session_rollovers=[],
            unverified=['Chinese Recall@10 and semantic/answer factual quality',
                        'production IAM/deployment and production capacity',
                        'full parse format/size matrix and browser render time',
                        'OS disk cold cache and model-not-resident matrix',
                        'provider token usage/cost and statistically qualified answer latency'])

    def url(self, db):
        return f'postgres://postgres@127.0.0.1:{self.args.port}/{db}?sslmode=disable'

    def pg(self, name):
        return self.args.pg_bin / (name + '.exe')

    def save(self):
        with self.lock:
            self.report.update(heartbeat_at=now(), active_cell=self.current_cell,
                               samples_in_active_cell=self.samples_in_cell,
                               outcomes_in_active_cell=dict(self.outcomes))
            payload = json.dumps(self.report, ensure_ascii=False, indent=2) + '\n'
            temporary = self.root / 'report.pending.json'
            temporary.write_text(payload, encoding='utf-8')
            for retry in range(8):
                try:
                    os.replace(temporary, self.root / 'report.json')
                    break
                except PermissionError:
                    if retry == 7:
                        raise
                    time.sleep(.01 * (retry + 1))
            local_time = dt.datetime.now(dt.timezone(dt.timedelta(hours=8))).isoformat(timespec='seconds')
            status = ['# 正式检索性能验收进度', '', f'更新时间：{local_time}（上海）',
                      f'状态：{self.report["decision"]}',
                      f'当前阶段：{self.current_cell or self.report.get("last_message", "准备")}',
                      f'当前阶段样本：{self.samples_in_cell}', f'当前阶段结果：{dict(self.outcomes)}',
                      f'已完成正常检索格：{sum(c["concurrency"]<=4 and c["gate"]!="inconclusive" and c["requests"]==self.report["plan"]["requests_per_cell"] for c in self.report["cells"])}；目标{len(self.report["plan"]["scales"])*6*self.report["plan"]["repetitions"]}格，每格{self.report["plan"]["requests_per_cell"]}次。',
                      f'已完成固定到达率组：{len(self.report["rate_cells"])}；目标3组。',
                      f'已完成稳定性组：{len(self.report["soaks"])}；目标2小时、8小时各一组。', '',
                      '运行中或未完成项目不得判定通过；首次必需门槛失败即停止后续阶段。',
                      '运行期间请求系统保持唤醒（屏幕可关闭），退出时恢复；手动休眠/关机会使本次测量无效。',
                      'production_acceptance=false；完整产品性能验收仍包含其他未验证项目。', '']
            try:
                (self.root / 'STATUS.md').write_text('\n'.join(status), encoding='utf-8')
            except OSError:
                pass  # Machine report is authoritative; an open preview may hold the Markdown file.

    def progress(self, message):
        self.report['last_message'] = message
        print(now(), message, flush=True)
        self.save()

    def raw(self, filename, value):
        with self.lock, (self.root / filename).open('a', encoding='utf-8') as file:
            file.write(json.dumps(value, ensure_ascii=False) + '\n')

    def check(self):
        if (self.root / 'CANCEL').exists():
            self.report['halt_reason'] = 'cancel file requested'
            self.stop.set()
        if self.stop.is_set():
            raise Halt(self.report.get('halt_reason', 'cancelled'))

    def run(self, command, *, timeout=180, check=True, db=None):
        env = dict(self.env)
        if db:
            env['MEMORYS_DATABASE_URL'] = self.url(db)
        p = subprocess.run(list(map(str, command)), cwd=ROOT, env=env, capture_output=True,
                           text=True, encoding='utf-8', errors='replace', timeout=timeout,
                           creationflags=subprocess.CREATE_NO_WINDOW)
        if check and p.returncode:
            # Successful CLI content and credentials are never written to logs.
            detail = p.stderr.strip() if Path(str(command[0])).name == 'memorys-local.exe' else ''
            raise RuntimeError(Path(str(command[0])).name + f' failed exit={p.returncode} ' + detail)
        return p

    def cli(self, command, *arguments):
        self.check()
        return json.loads(self.run([self.binary, command, '--state', self.runtime, *arguments]).stdout)

    def prepare(self):
        if os.name == 'nt':
            import ctypes
            self.previous_power_state = ctypes.windll.kernel32.SetThreadExecutionState(0x80000001)
            self.report['environment']['system_keep_awake_requested'] = bool(self.previous_power_state)
            if not self.previous_power_state:
                raise RuntimeError('could not request continuous system execution for the measurement')
        with socket.socket() as sock:
            if sock.connect_ex(('127.0.0.1', self.args.port)) == 0:
                raise RuntimeError('owned PostgreSQL port occupied; existing instance untouched')
        self.fixture.mkdir()
        (self.fixture / '.memorys-owned-formal').write_text('owned disposable formal performance fixture')
        self.report['controller_pid'] = os.getpid()
        self.progress('Building fixed application and migration binaries')
        reference = self.args.reference_baseline.resolve()
        baseline_raw = reference.read_bytes()
        baseline = json.loads(baseline_raw)
        if (baseline.get('baseline_decision') != 'provisional_pass' or not baseline.get('execution_complete')
                or not baseline.get('sources_unchanged_during_checks') or not baseline.get('frontend_unchanged_during_checks')):
            raise RuntimeError('reference baseline must be a completed unchanged provisional pass')
        self.report['reference_baseline'] = dict(path=str(reference), sha256=hashlib.sha256(baseline_raw).hexdigest())
        if application_inventory(self.source) != application_inventory(baseline['sources']):
            raise RuntimeError('application source differs from accepted optimized baseline')
        self.report['application_sources_match_optimized_baseline'] = True
        go = ROOT / '.tools/go/bin/go.exe'
        self.run([go, 'build', '-o', self.binary, './cmd/memorys-local'])
        migrate = self.fixture / 'migrate.exe'
        self.run([go, 'build', '-o', migrate, './cmd/memorys-migrate'])
        self.report['application_sha256'] = hashlib.sha256(self.binary.read_bytes()).hexdigest()
        self.report['application_binary_matches_optimized_baseline'] = self.report['application_sha256'] == baseline['application_sha256']
        if not self.report['application_binary_matches_optimized_baseline']:
            raise RuntimeError('application binary differs from optimized baseline')
        self.report['environment']['go'] = self.run([go, 'version']).stdout.strip()
        self.report['environment']['postgres'] = self.run([self.pg('postgres'), '--version']).stdout.strip()
        self.report['environment']['postgres_settings'] = dict(shared_buffers='128MB', max_connections=30, jit=False)
        try:
            self.opener.open('http://127.0.0.1:11434/api/tags', timeout=2).close()
        except OSError:
            executable = shutil.which('ollama')
            if not executable:
                raise RuntimeError('Ollama unavailable')
            with (self.fixture / 'ollama.log').open('ab') as log:
                self.ollama = subprocess.Popen([executable, 'serve'], stdout=log, stderr=log,
                                              creationflags=subprocess.CREATE_NO_WINDOW)
            for _ in range(100):
                self.check()
                try:
                    self.opener.open('http://127.0.0.1:11434/api/tags', timeout=2).close()
                    break
                except OSError:
                    self.stop.wait(.2)
            else:
                raise RuntimeError('Ollama startup failed')
        self.run([self.pg('initdb'), '-D', self.fixture / 'data', '-U', 'postgres', '-A', 'trust', '--no-locale', '--encoding=UTF8'])
        self.pg_attempted = True
        with (self.fixture / 'controller.log').open('ab') as log:
            p = subprocess.run([str(self.pg('pg_ctl')), '-D', str(self.fixture / 'data'), '-l', str(self.fixture / 'pg.log'),
                '-w', '-t', '30', '-o', f'-h 127.0.0.1 -p {self.args.port} -c shared_buffers=128MB -c max_connections=30 -c jit=off', 'start'],
                cwd=ROOT, env=self.env, stdout=log, stderr=log, timeout=45, creationflags=subprocess.CREATE_NO_WINDOW)
        if p.returncode:
            raise RuntimeError('owned PostgreSQL startup failed')
        self.run([migrate])
        self.cli('init')
        self.cli('enable-ai')
        manifest = json.loads((self.runtime / 'manifest.json').read_text(encoding='utf-8'))
        self.report['embedding'] = manifest['Embedding']
        if self.report['embedding'] != baseline['embedding']:
            raise RuntimeError('embedding pin differs from baseline')
        self.thread = threading.Thread(target=self.monitor, daemon=True)
        self.thread.start()
        self.progress('Preparing same bounded corpus and 32/100-document database snapshots')
        folder = self.fixture / 'synthetic'
        folder.mkdir()
        maximum = 32 if self.args.smoke else 500
        for n in range(maximum):
            self.check()
            text = (f'perfdoc{n:04d} perfshared 恢复方案编号{n:04d}\n'
                    f'星帆项目第{n:04d}号恢复手册。紧急联络暗号是紫色海豚。'
                    '数据库故障时先停止写入，恢复数据库和匹配的本地状态目录，运行诊断后验证检索。\n'
                    + ' recovery backup verified source' * 18)
            path = folder / f'{n:04d}.txt'
            path.write_bytes(text.encode())
            started = time.perf_counter()
            imported = self.cli('import', path)
            import_ms = (time.perf_counter() - started) * 1000
            started = time.perf_counter()
            published = self.cli('publish', imported['draft_id'], '--expect-hash', imported['content_hash'])
            self.raw('ingestion.jsonl', dict(index=n, import_ms=import_ms, publish_ms=(time.perf_counter() - started) * 1000))
            self.known[imported['document_id']] = dict(text=text, revision=published['revision_id'],
                                                      block='block-' + imported['document_id'].removeprefix('doc-'))
            self.published.append(imported['document_id'])
            count = n + 1
            if count in (32, 100, 500):
                corpus_hash = hashlib.sha256(''.join(self.known[d]['text'] for d in self.published).encode()).hexdigest()
                original = next(s for s in baseline['setup'] if s['documents'] == count)
                if corpus_hash != original['corpus_sha256']:
                    raise RuntimeError('corpus content differs from baseline')
                self.report['setup'].append(dict(documents=count, chunks=count, corpus_sha256=corpus_hash,
                    corpus_utf8_bytes=sum(len(v['text'].encode()) for v in self.known.values())))
                if count < maximum:
                    self.run([self.pg('pg_dump'), '-w', '-d', 'postgres', '-Fc', '-f', self.fixture / f'scale-{count}.dump'])
            if count % 25 == 0:
                self.progress(f'Published {count}/{maximum} synthetic documents')
        self.run([self.pg('psql'), '-X', '-w', '-c', 'ANALYZE'])
        if not self.args.smoke:
            for scale in (100, 32):
                db = f'scale_{scale}'
                self.run([self.pg('createdb'), '-w', db])
                self.run([self.pg('pg_restore'), '-w', '--exit-on-error', '--no-owner', '--no-acl', '-d', db,
                          self.fixture / f'scale-{scale}.dump'])
                self.run([self.pg('psql'), '-X', '-w', '-d', db, '-c', 'ANALYZE'])
        self.progress('Corpus and fixed snapshots ready')
        (self.root / 'corpus-public-manifest.json').write_text(json.dumps({doc:dict(index=n,
            text_sha256=hashlib.sha256(self.known[doc]['text'].encode()).hexdigest(),
            revision_id=self.known[doc]['revision'],block_id=self.known[doc]['block'])
            for n,doc in enumerate(self.published)},ensure_ascii=False,indent=2),encoding='utf-8')

    def start_web(self, scale):
        self.stop_web()
        self.scale = scale
        db = 'postgres' if scale == (32 if self.args.smoke else 500) else f'scale_{scale}'
        self.active_db = db
        self.env['MEMORYS_DATABASE_URL'] = self.url(db)
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', 0))
            address = f'127.0.0.1:{sock.getsockname()[1]}'
        self.origin = 'http://' + address
        sent = time.perf_counter()
        with (self.fixture / 'web.log').open('ab') as log:
            self.web = subprocess.Popen([str(self.binary), 'web', '--state', str(self.runtime), '--address', address,
                '--ui-dir', str(ROOT / 'dashboard/dist')], cwd=ROOT, env=self.env, stdout=log, stderr=log,
                creationflags=subprocess.CREATE_NO_WINDOW)
        for _ in range(100):
            self.check()
            if self.web.poll() is not None:
                raise RuntimeError('owned web process exited')
            try:
                with self.opener.open(self.origin + '/local/', timeout=2) as response:
                    self.cookie = response.headers['Set-Cookie'].split(';')[0]
                self.session_lower, self.session_upper = sent + 7200, time.perf_counter() + 7200
                return
            except OSError:
                self.stop.wait(.2)
        raise RuntimeError('owned web startup timeout')

    def stop_web(self):
        if self.web:
            self.web.terminate()
            self.web.wait(timeout=15)
            self.web = None

    def session(self):
        with self.session_lock:
            started = time.perf_counter()
            wait = session_renewal_wait(started, self.session_lower, self.session_upper)
            if wait is not None:
                if self.stop.wait(wait):
                    raise Halt('cancelled during session maintenance')
                sent = time.perf_counter()
                with self.opener.open(self.origin + '/local/', timeout=5) as response:
                    self.cookie = response.headers['Set-Cookie'].split(';')[0]
                received = time.perf_counter()
                self.session_lower, self.session_upper = sent + 7200, received + 7200
                with self.lock:
                    self.report['session_rollovers'].append(dict(at=now(), wait_ms=(received - started) * 1000))
            return self.cookie

    def request(self, mode, n):
        started = time.perf_counter()
        row = dict(sequence=n, mode=mode, documents=self.scale, outcome='transport_error', http_status=None,
                   started_monotonic_seconds=started)
        if self.stop.is_set():
            return dict(row, outcome='cancelled', elapsed_ms=0)
        kind, index = n % 4, n % self.scale
        query = ((f'perfdoc{index:04d}', f'恢复方案编号{index:04d}', 'perfshared', 'absentperformancemarker')[kind]
                 if mode == 'keyword' else ('星帆项目发生数据库故障时如何恢复？', '紧急联络暗号是什么？', '如何处理数据库备份恢复？')[n % 3])
        row['query_sha256'] = hashlib.sha256(query.encode()).hexdigest()
        try:
            cookie = self.session()
            body = json.dumps(dict(mode=mode, query=query)).encode()
            address = self.origin.removeprefix('http://')
            wire = (f'POST /local/api/search HTTP/1.1\r\nHost: {address}\r\nContent-Type: application/json\r\n'
                    f'Origin: {self.origin}\r\nX-Memorys-Local: 1\r\nCookie: {cookie}\r\n'
                    f'Content-Length: {len(body)}\r\nConnection: close\r\n\r\n').encode() + body
            sent = time.perf_counter()
            with socket.create_connection(('127.0.0.1', int(address.rsplit(':', 1)[1])), timeout=125) as sock:
                sock.sendall(wire)
                with http.client.HTTPResponse(sock) as response:
                    response.begin()
                    raw, status = response.read((4 << 20) + 1), response.status
            row.update(http_status=status, response_bytes=len(raw), service_ms=(time.perf_counter() - sent) * 1000)
            if len(raw) > 4 << 20:
                raise ValueError('response exceeded bound')
            data = json.loads(raw)
            if status != 200:
                row['outcome'] = 'rejected_429' if status == 429 else 'http_error'
            else:
                allowed = set(self.published[:self.scale])
                valid = evidence_valid(data, mode, kind, self.published[index], allowed, self.known)
                items = data.get('evidence') or []
                row['observed_citations'] = [dict(citation=e.get('citation'),rank=e.get('rank'),
                    excerpt_sha256=hashlib.sha256(e.get('source_excerpt','').encode()).hexdigest())
                    for e in items if isinstance(e,dict) and isinstance(e.get('source_excerpt'),str)]
                row['response_sha256'] = hashlib.sha256(raw).hexdigest()
                row.update(outcome='success' if valid else 'invalid_result', evidence_count=len(items or []))
        except (TimeoutError, socket.timeout):
            row['outcome'] = 'timeout'
        except Halt:
            row['outcome'] = 'cancelled'
        except Exception as error:
            row['error_type'] = type(error).__name__
        row['elapsed_ms'] = (time.perf_counter() - started) * 1000
        return row

    def record(self, row):
        row['cell_id'] = self.current_cell
        try:
            self.raw('requests.jsonl', row)
        except Exception:
            self.stop.set()
            raise
        with self.lock:
            self.samples_in_cell += 1
            self.outcomes[row.get('outcome', 'missing')] += 1
            if row.get('outcome') == 'invalid_result':
                self.report['decision'] = 'fail'
                self.report['halt_reason'] = 'correctness failure in ' + str(self.current_cell)
                self.stop.set()
            if row.get('outcome') == 'cancelled':
                return  # Controlled cancellation is incomplete evidence, not a service failure.
            overload = row.get('concurrency', 0) > 4 or self.current_rate > 1
            unexpected = row.get('outcome') != 'success' and not (overload and row.get('outcome') == 'rejected_429')
            self.consecutive_failures = self.consecutive_failures + 1 if unexpected else 0
            if self.consecutive_failures >= 10:
                self.report.update(decision='fail', halt_reason='10 consecutive unexpected failures')
                self.stop.set()

    def warm(self):
        for mode in ('keyword', 'semantic'):
            result = self.request(mode, 0)
            self.raw('controls.jsonl', dict(category='warmup', **result))
            if result['outcome'] != 'success':
                raise RuntimeError('warmup failed: ' + mode + '/' + result['outcome'])

    def closed(self, mode, concurrency, repetition, count):
        self.current_cell = f'closed-{self.scale}-{mode}-c{concurrency}-r{repetition}'
        self.samples_in_cell = 0
        self.outcomes.clear()
        self.consecutive_failures = 0
        self.current_rate = 0
        self.progress('Starting ' + self.current_cell + f' count={count}')
        def call(n):
            row = self.request(mode, n)
            row.update(concurrency=concurrency, repetition=repetition, load_model='closed_loop')
            return row
        rows, elapsed = closed_loop(call, count=count, workers=concurrency, stop=self.stop, on_sample=self.record)
        result = dict(cell_id=self.current_cell, mode=mode, documents=self.scale, concurrency=concurrency,
                      repetition=repetition, cache='warm', **describe(rows, elapsed))
        result['gate'] = gate(rows, overload=concurrency > 4) if len(rows) == count else 'inconclusive'
        if self.stop.is_set():
            result['gate'] = 'fail' if self.report['decision']=='fail' else 'inconclusive'
        self.report['cells'].append(result)
        self.progress(f'Finished {self.current_cell}: {result["successful"]}/{count} P95={result["p95_ms"]}ms {result["gate"]}')
        if result['gate'] != 'pass':
            self.report['decision'] = 'inconclusive' if result['gate']=='inconclusive' else 'fail'
            raise Halt('required closed-loop cell failed: ' + self.current_cell)

    def rate(self, rate, seconds, *, soak=False):
        self.current_cell = ('soak' if soak else 'rate') + f'-{self.scale}-{rate}rps-{seconds}s'
        self.samples_in_cell = 0
        self.outcomes.clear()
        self.consecutive_failures = 0
        self.current_rate = rate
        self.progress('Starting ' + self.current_cell)
        started = time.perf_counter()
        def send(n, _):
            row = self.request('semantic' if n % 2 else 'keyword', n // 2)
            row['query_sequence'], row['sequence'] = row['sequence'], n
            return row
        rows, elapsed, expected = open_loop(send,
            rate=rate, duration=seconds, workers=16, max_pending=32, stop=self.stop, on_sample=self.record)
        result = dict(cell_id=self.current_cell, documents=self.scale, arrival_rate=rate, duration_seconds=seconds,
                      expected=expected, load_model='open_loop', mixture='50% keyword / 50% semantic', **describe(rows, elapsed))
        result['gate'] = gate(rows, overload=rate > 1) if len(rows) == expected else 'inconclusive'
        result['mode_gates'] = {mode: gate([r for r in rows if r.get('mode') == mode], overload=rate > 1)
                               for mode in ('keyword', 'semantic')}
        if result['gate']!='inconclusive' and any(value != 'pass' for value in result['mode_gates'].values()):
            result['gate'] = 'fail'
        if self.stop.is_set():
            result['gate'] = 'fail' if self.report['decision']=='fail' else 'inconclusive'
        if soak:
            result['resources'] = self.resource_gate(started, time.perf_counter())
            if result['resources']['gate'] != 'pass':
                result['gate'] = result['resources']['gate']
            self.report['soaks'].append(result)
        else:
            self.report['rate_cells'].append(result)
        self.progress(f'Finished {self.current_cell}: P95={result["p95_ms"]}ms gate={result["gate"]}')
        if result['gate'] != 'pass':
            self.report['decision'] = 'inconclusive' if result['gate']=='inconclusive' else 'fail'
            raise Halt('required arrival-rate/soak gate failed: ' + self.current_cell)

    def resource_gate(self, start, end):
        samples = [r for r in self.monitor_samples if r.get('web') and start <= r['time'] <= end]
        first = [r['web'] for r in samples if start + 600 <= r['time'] < start + 1200]
        last = [r['web'] for r in samples if end - 600 <= r['time'] <= end]
        if len(first) < 60 or len(last) < 60:
            return dict(gate='inconclusive', reason='insufficient steady-state resource samples')
        before_rss, after_rss = statistics.median(r['rss'] for r in first), statistics.median(r['rss'] for r in last)
        before_handles, after_handles = statistics.median(r['handles'] for r in first), statistics.median(r['handles'] for r in last)
        passed = after_rss - before_rss <= max(64 << 20, before_rss * .5) and after_handles - before_handles <= 32
        connections = [r['client_connections'] for r in self.db_samples if start <= r['time'] <= end]
        if not connections:
            return dict(gate='inconclusive', reason='missing database connection samples')
        sampling_gap = max((b['wall_epoch']-a['wall_epoch'] for a,b in zip(samples,samples[1:])),default=0)
        divergence = clock_divergence_ms(samples)
        if divergence>250:
            return dict(gate='inconclusive', reason='wall/monotonic clock discontinuity', maximum_clock_divergence_ms=divergence)
        passed = passed and max(connections) <= 10 and sampling_gap <= 30
        return dict(gate='pass' if passed else 'fail', rss_before=before_rss, rss_after=after_rss,
                    handles_before=before_handles, handles_after=after_handles,
                    maximum_observed_client_connections=max(connections), maximum_sampling_gap_seconds=sampling_gap,
                    maximum_clock_divergence_ms=divergence)

    def monitor(self):
        begun = last_source_check = time.perf_counter()
        while not self.finished.wait(5):
            try:
                stamp = time.perf_counter()
                if ((self.root / 'CANCEL').exists() or stamp - self.started_monotonic > 24 * 3600
                        or time.time()-self.started_wall > 24*3600):
                    with self.lock:
                        self.report['halt_reason'] = 'cancel file or maximum wall duration reached'
                    self.stop.set()
                if stamp - last_source_check >= (10 if self.args.smoke else 60):
                    last_source_check = stamp
                    if self.source != source_inventory() or self.assets != asset_inventory():
                        with self.lock:
                            self.report['halt_reason'] = 'source/frontend changed during measurements'
                        self.stop.set()
                    if not self.stop.is_set():
                        stats = self.run([self.pg('psql'), '-X', '-w', '-d', self.active_db, '-Atc',
                            "SELECT json_build_object('client_connections',(SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND backend_type='client backend' AND pid<>pg_backend_pid()),'database_bytes',pg_database_size(current_database()),'token_rows',(SELECT count(*) FROM akos_withdraw_v1.identity_tokens))"], timeout=5)
                        item = dict(time=stamp, cell_id=self.current_cell, database=self.active_db, **json.loads(stats.stdout))
                        self.db_samples.append(item)
                        self.raw('database-resources.jsonl', item)
                processes = []
                candidates = {p.pid: p for p in psutil.Process().children(recursive=True)}
                try:
                    pg = psutil.Process(int((self.fixture / 'data/postmaster.pid').read_text().splitlines()[0]))
                    for p in [pg] + pg.children(recursive=True):
                        candidates[p.pid] = p
                except (OSError, ValueError, psutil.Error):
                    pass
                web_sample = None
                for p in candidates.values():
                    try:
                        if p.name().lower().startswith(('postgres', 'memorys-local', 'ollama')):
                            with p.oneshot():
                                cpu = p.cpu_times()
                                item = dict(pid=p.pid, name=p.name(), rss=p.memory_info().rss,
                                            handles=p.num_handles(), cpu_seconds=cpu.user + cpu.system)
                            processes.append(item)
                            if self.web and p.pid == self.web.pid:
                                web_sample = item
                    except psutil.Error:
                        pass
                row = dict(time=time.perf_counter(), wall_epoch=time.time(), cell_id=self.current_cell, web=web_sample,
                           host_available_memory=psutil.virtual_memory().available,
                           host_cpu_times=psutil.cpu_times()._asdict(), processes=processes)
                self.monitor_samples.append(row)
                self.raw('resources.jsonl', row)
                self.save()
            except Exception as error:
                with self.lock:
                    self.report['halt_reason'] = 'monitor failure: ' + type(error).__name__
                self.stop.set()

    def execute(self):
        try:
            self.prepare()
            scales = self.report['plan']['scales']
            for scale in scales:
                self.start_web(scale)
                self.warm()
                for concurrency in (4, 2, 1):
                    for mode in ('semantic', 'keyword'):
                        for repetition in range(1, self.report['plan']['repetitions'] + 1):
                            self.check()
                            # Each cell is well below the two-hour session TTL;
                            # restart/warm explicitly instead of mixing expiry into it.
                            if repetition > 1 or mode == 'keyword' or concurrency != 4:
                                self.start_web(scale)
                                self.warm()
                            self.closed(mode, concurrency, repetition, self.report['plan']['requests_per_cell'])
                for mode in ('keyword', 'semantic'):
                    self.closed(mode, 8, 0, 20)
            self.report['formal_load_acceptance'] = not self.args.smoke
            largest = scales[0]
            self.start_web(largest)
            self.warm()
            for rate in (1, 2, 3):
                self.rate(rate, self.report['plan']['rate_seconds'])
            self.report['nominal_rate_acceptance'] = not self.args.smoke
            if not self.args.smoke:
                if not self.env.get('DEEPSEEK_API_KEY'):
                    raise RuntimeError('bounded answer smoke requires configured provider key')
                self.current_cell = 'answer-smoke-max6'
                self.samples_in_cell = 0
                for n in range(6):
                    row = self.request('answer', n)
                    self.raw('answer-smoke.jsonl', row)
                    if row['outcome'] != 'success':
                        self.report['decision'] = 'fail'
                        raise Halt('bounded answer smoke failed')
            for hours in self.report['plan']['soak_hours']:
                self.start_web(largest)
                self.warm()
                self.rate(1, hours * 3600, soak=True)
            self.report['soak_acceptance'] = not self.args.smoke
            self.report['local_retrieval_performance_acceptance'] = not self.args.smoke
            self.report['decision'] = 'smoke_pass' if self.args.smoke else 'pass'
        except Halt as error:
            if self.report['decision'] == 'running':
                self.report['decision'] = 'inconclusive'
            self.report['halt_reason'] = str(error)
        except Exception as error:
            self.report.update(decision='inconclusive', error_type=type(error).__name__, error=str(error))
        finally:
            self.finished.set()
            if self.thread:
                self.thread.join(10)
            try:
                self.stop_web()
                if self.pg_attempted:
                    p = self.run([self.pg('pg_ctl'), '-D', self.fixture / 'data', '-m', 'fast', '-w', '-t', '30', 'stop'], check=False)
                    if p.returncode and self.run([self.pg('pg_ctl'), '-D', self.fixture / 'data', 'status'], check=False).returncode != 3:
                        raise RuntimeError('owned PostgreSQL remains running')
                self.report['server_stopped'] = True
                if self.ollama:
                    children = psutil.Process(self.ollama.pid).children(recursive=True)
                    self.ollama.terminate()
                    self.ollama.wait(10)
                    for child in children:
                        try:
                            child.terminate()
                        except psutil.NoSuchProcess:
                            pass
                    _, alive = psutil.wait_procs(children, timeout=10)
                    if alive:
                        raise RuntimeError('owned model children remain running')
                    self.report['owned_ollama_stopped'] = True
                if self.fixture.exists():
                    if self.fixture.resolve().parent != self.root or not (self.fixture / '.memorys-owned-formal').is_file():
                        raise RuntimeError('fixture cleanup ownership guard failed')
                    shutil.rmtree(self.fixture)
                self.report['temporary_resources_removed'] = True
            except Exception as error:
                self.report.update(decision='inconclusive', cleanup_error=type(error).__name__ + ': ' + str(error))
            if self.previous_power_state:
                import ctypes
                self.report['keep_awake_state_restored'] = bool(ctypes.windll.kernel32.SetThreadExecutionState(self.previous_power_state))
            self.report['sources_unchanged_during_checks'] = self.source == source_inventory()
            self.report['frontend_unchanged_during_checks'] = self.assets == asset_inventory()
            if not self.report['sources_unchanged_during_checks'] or not self.report['frontend_unchanged_during_checks']:
                self.report['decision'] = 'inconclusive'
                self.report['local_retrieval_performance_acceptance'] = False
            self.report['finished_at'] = now()
            if self.report['decision'] != 'pass':
                self.report['local_retrieval_performance_acceptance'] = False
            self.current_cell = None
            self.progress('Final decision: ' + self.report['decision'])
            self.final_artifacts()
        return 0 if self.report['decision'] in ('pass', 'smoke_pass') else 1

    def final_artifacts(self):
        lines=['# 正式检索性能验收结果','',f'结论：{self.report["decision"]}',
            f'检索范围完整验收：{self.report["local_retrieval_performance_acceptance"]}；production_acceptance=false。',
            '范围为当前个人本机单KB、合成资料、固定硬件/程序/模型，不代表中文质量或完整产品验收。','',
            f'结束原因：{self.report.get("halt_reason",self.report.get("error","按计划完成"))}',
            f'清理：服务停止={self.report.get("server_stopped",False)}；临时资源移除={self.report.get("temporary_resources_removed",False)}。','',
            '| 工况 | 样本 | 成功 | 429 | 失败 | P95毫秒 | P99毫秒 | 结论 |',
            '| --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |']
        for cell in self.report['cells']+self.report['rate_cells']+self.report['soaks']:
            lines.append(f'| {cell["cell_id"]} | {cell["requests"]} | {cell["successful"]} | {cell["rejected_429"]} | {cell["failures"]} | {cell["p95_ms"]} | {cell["p99_ms"]} | {cell["gate"]} |')
        lines+=['','未执行阶段不能按通过处理。正式正常矩阵目标54格×1000次，另有过载观察、固定到达率和2/8小时稳定性；首次必需门槛失败即停止后续。','',
            '完整原始证据见report.json、requests.jsonl、resources.jsonl、database-resources.jsonl和corpus-public-manifest.json。',
            '每次请求校验精确文档/版本/块绑定和正文哈希；记录不含访问令牌、Cookie、数据库凭据或模型密钥。',
            '固定到达率延迟从计划到达时刻计算，包含客户端排队、会话维护和调度延迟；没有自动重试。',
            '冷缓存、多格式、浏览器渲染、模型事实正确性及供应商费用等未验证项目列于report.json。','']
        (self.root/'RESULTS.md').write_text('\n'.join(lines),encoding='utf-8')
        (self.root/'SHA256.json').write_text(json.dumps({p.name:hashlib.sha256(p.read_bytes()).hexdigest()
            for p in self.root.iterdir() if p.is_file() and p.name not in ('SHA256.json','stdout.log','stderr.log')},indent=2),encoding='utf-8')


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('--pg-bin', type=Path, required=True)
    ap.add_argument('--output', type=Path, required=True)
    ap.add_argument('--port', type=int, default=55445)
    ap.add_argument('--reference-baseline', type=Path,
                    default=ROOT / '.local/performance/2026-10-24-optimized-01/report.json',
                    help='completed small baseline; application binary/source, corpus and model must match')
    ap.add_argument('--smoke', action='store_true', help='small runner validation, never formal acceptance')
    args = ap.parse_args()
    if not args.pg_bin.is_absolute() or args.output.exists() or not 1024 <= args.port <= 65535:
        ap.error('absolute PG directory, new output directory and valid unprivileged port required')
    args.output.mkdir(parents=True)
    job = Acceptance(args)
    def stop_signal(*_):
        with job.lock:
            job.report['halt_reason'] = 'controller signal received'
        job.stop.set()
    for sig in (signal.SIGINT, signal.SIGTERM):
        signal.signal(sig, stop_signal)
    return job.execute()


if __name__ == '__main__':
    raise SystemExit(main())

"""Measure real local web requests in an owned PostgreSQL fixture (baseline first)."""
import argparse
import concurrent.futures as futures
import datetime as dt
import hashlib
import http.client
import json
import math
import os
from pathlib import Path
import platform
import shutil
import socket
import subprocess
import threading
import time
import urllib.request

import psutil
from check_architecture import ROOT, source_inventory


def quantile(values, percentile):
    ordered = sorted(values)
    return ordered[max(0, math.ceil(len(ordered) * percentile / 100) - 1)] if ordered else None


def summarize(samples, elapsed):
    successful = [s for s in samples if s['outcome'] == 'success']
    latency = [s['elapsed_ms'] for s in samples]
    return dict(requests=len(samples), successful=len(successful),
                failures=sum(s['outcome'] not in ('success', 'rejected_429') for s in samples),
                rejected_429=sum(s['outcome'] == 'rejected_429' for s in samples),
                timeouts=sum(s['outcome'] == 'timeout' for s in samples),
                correctness_errors=sum(s['outcome'] == 'invalid_result' for s in samples),
                elapsed_seconds=round(elapsed, 3),
                successful_requests_per_second=round(len(successful) / elapsed, 3) if elapsed else 0,
                p50_ms=quantile(latency, 50), p95_ms=quantile(latency, 95), p99_ms=quantile(latency, 99),
                successful_p95_ms=quantile([s['elapsed_ms'] for s in successful], 95))


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('--pg-bin', type=Path, required=True)
    ap.add_argument('--output', type=Path, required=True)
    ap.add_argument('--port', type=int, default=55443)
    ap.add_argument('--scales', type=int, nargs='+', default=[32, 100, 500])
    ap.add_argument('--requests', type=int, default=20)
    ap.add_argument('--answer-requests', type=int, default=6)
    args = ap.parse_args()
    if not args.pg_bin.is_absolute() or not 1 <= args.requests <= 10000 or not 0 <= args.answer_requests <= 50:
        ap.error('absolute PG path and bounded request counts required')
    if args.output.exists() or not all(1 <= n <= 512 for n in args.scales) or not 1024 <= args.port <= 65535:
        ap.error('new output directory, valid port and scales from 1 to 512 required')
    args.output.mkdir(parents=True)
    output = args.output.resolve()
    fixture = output / 'owned-fixture'
    fixture.mkdir()
    (fixture / '.memorys-owned-performance').write_text('owned disposable performance instance')
    report_path = output / 'report.json'
    source_before = source_inventory()
    assets = ROOT / 'dashboard/dist'
    asset_before = {p.relative_to(assets).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest()
                    for p in assets.rglob('*') if p.is_file()}
    report = dict(schema_version='memorys.local-performance/1', stage='baseline',
                  started_at=dt.datetime.now(dt.timezone.utc).isoformat(),
                  production_acceptance=False, local_performance_acceptance=False,
                  sources=source_before, frontend_build=asset_before, cells=[], setup=[], controls=[],
                  thresholds=dict(retrieval_p95_ms=2000, unexpected_error_rate=0.001,
                                  answer_p95_ms=30000, answer_success_rate=0.95),
                  environment=dict(platform=platform.platform(), processor=platform.processor(),
                                   logical_cpus=psutil.cpu_count(), physical_cpus=psutil.cpu_count(logical=False),
                                   ram_bytes=psutil.virtual_memory().total,
                                   http_client='HTTP/1.1 fresh connection; headers and body sent in one buffer'),
                  unverified=['1000 requests per cell and three formal repetitions',
                              '2 hour and 8 hour soak', 'browser render and interactive timing',
                              'full format/size parse matrix', 'production capacity and Chinese Recall@10',
                              'provider token usage (current application does not expose usage)',
                              'internal stage timings (current application has no tracing)',
                              'OS disk cold cache and model-not-resident cold start'])
    env = dict(os.environ)
    for key in list(env):
        if key.startswith('PG') or key.startswith('MEMORYS_'):
            env.pop(key, None)
    env.update(PGHOST='127.0.0.1', PGPORT=str(args.port), PGUSER='postgres', PGDATABASE='postgres',
               MEMORYS_DATABASE_URL=f'postgres://postgres@127.0.0.1:{args.port}/postgres?sslmode=disable',
               MEMORYS_CHAT_MODEL='deepseek-flash')
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    web = ollama = None
    pg_attempted = False
    stop_monitor = threading.Event()
    write_lock = threading.Lock()
    known = {}
    published = []
    origin = cookie = None

    def save():
        temp = output / 'report.pending.json'
        temp.write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
        os.replace(temp, report_path)

    def progress(message):
        print(message, flush=True)
        save()

    def rawline(name, item):
        with write_lock, (output / name).open('a', encoding='utf-8') as f:
            f.write(json.dumps(item, ensure_ascii=False) + '\n')

    def run(command, timeout=120, success=True):
        p = subprocess.run(list(map(str, command)), cwd=ROOT, env=env, capture_output=True,
                           text=True, encoding='utf-8', errors='replace', timeout=timeout,
                           creationflags=subprocess.CREATE_NO_WINDOW)
        if success and p.returncode:
            # CLI errors are static/redacted; never record successful CLI payloads or credentials.
            detail = p.stderr.strip() if Path(str(command[0])).name == 'memorys-local.exe' else ''
            raise RuntimeError(Path(str(command[0])).name + ' failed: ' + str(p.returncode) + ' ' + detail)
        return p

    def pg(name):
        return args.pg_bin / (name + '.exe')

    binary = fixture / 'memorys-local.exe'
    runtime = fixture / 'runtime'

    def cli(command, *extra):
        return json.loads(run([binary, command, '--state', runtime, *extra], timeout=150).stdout)

    def start_web():
        nonlocal web, origin, cookie
        with socket.socket() as s:
            s.bind(('127.0.0.1', 0))
            address = '127.0.0.1:' + str(s.getsockname()[1])
        origin = 'http://' + address
        with (fixture / 'web.log').open('ab') as log:
            web = subprocess.Popen([str(binary), 'web', '--state', str(runtime), '--address', address,
                                    '--ui-dir', str(assets)], cwd=ROOT, env=env, stdout=log, stderr=log,
                                   creationflags=subprocess.CREATE_NO_WINDOW)
        for _ in range(100):
            if web.poll() is not None:
                raise RuntimeError('web process exited')
            try:
                with opener.open(origin + '/local/', timeout=2) as response:
                    cookie = response.headers['Set-Cookie'].split(';')[0]
                return
            except OSError:
                time.sleep(0.2)
        raise RuntimeError('web startup timeout')

    def stop_web():
        nonlocal web
        if web:
            web.terminate()
            web.wait(timeout=15)
            web = None

    def monitor():
        while not stop_monitor.wait(1):
            processes = []
            candidates = {p.pid:p for p in psutil.Process().children(recursive=True)}
            try:
                postmaster = psutil.Process(int((fixture / 'data/postmaster.pid').read_text().splitlines()[0]))
                for p in [postmaster] + postmaster.children(recursive=True):
                    candidates[p.pid] = p
            except (OSError, ValueError, psutil.Error):
                pass
            for p in candidates.values():
                try:
                    if p.name().lower().startswith(('postgres', 'memorys-local', 'ollama')):
                        with p.oneshot():
                            cpu = p.cpu_times()
                            processes.append(dict(pid=p.pid, name=p.name(), rss=p.memory_info().rss,
                                                  cpu_seconds=cpu.user + cpu.system, handles=p.num_handles()))
                except (psutil.Error, OSError):
                    pass
            rawline('resources.jsonl', dict(monotonic_seconds=time.monotonic(),
                                           host_memory_available=psutil.virtual_memory().available,
                                           processes=processes))

    def request(mode, n, scale, category='measured', query_override=None, concurrency=None, cell_id=None):
        if mode == 'keyword':
            kind = n % 4
            index = n % scale
            query = (f'perfdoc{index:04d}' if kind == 0 else f'恢复方案编号{index:04d}' if kind == 1
                     else 'perfshared' if kind == 2 else 'absentperformancemarker')
        else:
            kind = 0
            query = ('星帆项目发生数据库故障时如何恢复？', '紧急联络暗号是什么？',
                     '如何处理数据库备份恢复？')[n % 3]
        query = query_override or query
        started = time.perf_counter()
        sample = dict(mode=mode, scale=scale, category=category, query_number=n,
                      query_sha256=hashlib.sha256(query.encode()).hexdigest(), outcome='error', http_status=None,
                      concurrency=concurrency, cell_id=cell_id, started_monotonic_seconds=started)
        try:
            body = json.dumps(dict(query=query, mode=mode)).encode()
            address = origin.removeprefix('http://')
            # urllib sends headers and body separately. On Windows, an early
            # rejection can reset that split upload before its status is read.
            # Keep the same fresh-connection load model, sending a complete
            # bounded request before reading the response. No retry hides errors.
            wire = (f'POST /local/api/search HTTP/1.1\r\nHost: {address}\r\n'
                    f'Content-Type: application/json\r\nOrigin: {origin}\r\n'
                    f'X-Memorys-Local: 1\r\nCookie: {cookie}\r\n'
                    f'Content-Length: {len(body)}\r\nConnection: close\r\n\r\n').encode() + body
            with socket.create_connection(('127.0.0.1', int(address.rsplit(':', 1)[1])), timeout=125) as sock:
                sock.sendall(wire)
                with http.client.HTTPResponse(sock) as response:
                    response.begin()
                    raw = response.read((4 << 20) + 1)
                    status = response.status
            if len(raw) > 4 << 20:
                raise ValueError('bounded response exceeded')
            sample.update(http_status=status, response_bytes=len(raw))
            data = json.loads(raw)
            if status != 200:
                sample.update(outcome='rejected_429' if status == 429 else 'http_error',
                              response_sha256=hashlib.sha256(raw).hexdigest(),
                              error_message=data.get('error', ''))
                return finish_sample(sample, started)
            items = data.get('evidence')
            valid = isinstance(items, list) and len(items) <= 10
            seen = set()
            for item in items or []:
                citation = item.get('citation', {})
                doc = citation.get('document_id')
                valid = valid and doc in known and doc not in seen and citation.get('source_kind') == 'revision'
                valid = valid and item.get('source_excerpt') == known.get(doc)
                valid = valid and bool(citation.get('revision_id')) and bool(citation.get('block_id'))
                seen.add(doc)
            if mode == 'keyword':
                if kind in (0, 1) and not query_override:
                    valid = valid and published[n % scale] in seen
                elif kind == 2:
                    valid = valid and len(items) == min(scale, 10)
                else:
                    valid = valid and len(items) == 0
            else:
                valid = valid and len(items) == min(scale, 10 if mode == 'semantic' else 4)
            if mode == 'answer':
                cites = data.get('citations') or []
                valid = valid and data.get('status') == 'answered' and bool(cites) and bool(data.get('answer'))
                valid = valid and all(c.get('document_id') in seen for c in cites)
                sample['answer_status'] = data.get('status')
            sample.update(outcome='success' if valid else 'invalid_result', evidence_count=len(items or []))
        except (TimeoutError, socket.timeout):
            sample['outcome'] = 'timeout'
        except Exception as error:
            sample.update(outcome='transport_error', error_type=type(error).__name__)
        return finish_sample(sample, started)

    def finish_sample(sample, started):
        sample['elapsed_ms'] = round((time.perf_counter() - started) * 1000, 3)
        rawline('requests.jsonl', sample)
        return sample

    def cell(mode, scale, concurrency, count):
        started = time.perf_counter()
        cell_id = f'{mode}-{scale}-c{concurrency}'
        with futures.ThreadPoolExecutor(max_workers=concurrency) as pool:
            rows = list(pool.map(lambda n: request(mode, n, scale, concurrency=concurrency, cell_id=cell_id), range(count)))
        result = dict(mode=mode, documents=scale, concurrency=concurrency, load_model='closed_loop',
                      cache='document_vectors_warm' if mode != 'keyword' else 'process_and_database_warm',
                      **summarize(rows, time.perf_counter() - started))
        overload = concurrency > 4
        failures = result['requests'] - result['successful']
        if overload:
            result['baseline_gate'] = 'overload_observation_only'
        elif mode == 'answer':
            result['baseline_gate'] = 'pass' if result['p95_ms'] <= 30000 and failures / count <= .05 else 'fail'
        else:
            result['baseline_gate'] = 'pass' if result['p95_ms'] <= 2000 and failures / count <= .001 else 'fail'
        report['cells'].append(result)
        progress(f'{mode} docs={scale} c={concurrency}: {result["successful"]}/{count}, '
                 f'p95={result["p95_ms"]}ms, {result["baseline_gate"]}')

    thread = None
    try:
        with socket.socket() as s:
            if s.connect_ex(('127.0.0.1', args.port)) == 0:
                raise RuntimeError('isolated PostgreSQL port occupied; existing instance untouched')
        for name in ('postgres', 'pg_ctl', 'initdb', 'psql'):
            if not pg(name).is_file():
                raise RuntimeError('missing PostgreSQL binary: ' + name)
        if not (assets / 'index.html').is_file():
            raise RuntimeError('frontend build missing')
        try:
            with opener.open('http://127.0.0.1:11434/api/tags', timeout=2) as response:
                tags = json.load(response)
        except OSError:
            ollama_exe = shutil.which('ollama')
            if not ollama_exe:
                raise RuntimeError('Ollama unavailable')
            with (fixture / 'ollama.log').open('ab') as log:
                ollama = subprocess.Popen([ollama_exe, 'serve'], stdout=log, stderr=log,
                                          creationflags=subprocess.CREATE_NO_WINDOW)
            for _ in range(100):
                try:
                    with opener.open('http://127.0.0.1:11434/api/tags', timeout=2) as response:
                        tags = json.load(response)
                    break
                except OSError:
                    time.sleep(.2)
            else:
                raise RuntimeError('Ollama startup timeout')
        report['environment']['ollama_models'] = [dict(name=m['name'], digest=m['digest']) for m in tags['models']]
        go = ROOT / '.tools/go/bin/go.exe'
        progress('Building current application and migration binaries')
        run([go, 'build', '-o', binary, './cmd/memorys-local'])
        migrate = fixture / 'migrate.exe'
        run([go, 'build', '-o', migrate, './cmd/memorys-migrate'])
        report['application_sha256'] = hashlib.sha256(binary.read_bytes()).hexdigest()
        report['environment']['go'] = run([go, 'version']).stdout.strip()
        report['environment']['postgres'] = run([pg('postgres'), '--version']).stdout.strip()
        run([pg('initdb'), '-D', fixture / 'data', '-U', 'postgres', '-A', 'trust', '--no-locale', '--encoding=UTF8'])
        pg_attempted = True
        # pg_ctl inherits a real log file; pipe capture would be held open by postgres children.
        with (fixture / 'controller.log').open('ab') as log:
            p = subprocess.run([str(pg('pg_ctl')), '-D', str(fixture / 'data'), '-l', str(fixture / 'server.log'),
                                '-w', '-t', '30', '-o', f'-h 127.0.0.1 -p {args.port} -c shared_buffers=128MB -c max_connections=30 -c jit=off',
                                'start'], env=env, cwd=ROOT, stdout=log, stderr=log, timeout=45,
                               creationflags=subprocess.CREATE_NO_WINDOW)
        if p.returncode:
            raise RuntimeError('isolated PostgreSQL startup failed')
        report['environment']['postgres_settings'] = dict(shared_buffers='128MB', max_connections=30, jit=False)
        run([migrate])
        cli('init')
        cli('enable-ai')
        manifest = json.loads((runtime / 'manifest.json').read_text(encoding='utf-8'))
        report['embedding'] = manifest['Embedding']
        thread = threading.Thread(target=monitor, daemon=True)
        thread.start()
        start_web()
        progress('Isolated PostgreSQL and real local web ready; publishing bounded synthetic corpus')
        source_folder = fixture / 'synthetic'
        source_folder.mkdir()
        for scale in sorted(set(args.scales)):
            while len(published) < scale:
                n = len(published)
                text = (f'perfdoc{n:04d} perfshared 恢复方案编号{n:04d}\n'
                        f'星帆项目第{n:04d}号恢复手册。紧急联络暗号是紫色海豚。'
                        '数据库故障时先停止写入，恢复数据库和匹配的本地状态目录，运行诊断后验证检索。\n')
                text += ' recovery backup verified source' * 18
                if len(text) > 1024 or len(text.encode()) > 1024:
                    raise RuntimeError('corpus fixture exceeded one-chunk/one-KiB design')
                path = source_folder / f'perf-{n:04d}.txt'
                path.write_bytes(text.encode())
                before = time.perf_counter()
                imported = cli('import', path)
                import_ms = (time.perf_counter() - before) * 1000
                before = time.perf_counter()
                cli('publish', imported['draft_id'], '--expect-hash', imported['content_hash'])
                rawline('ingestion.jsonl', dict(index=n, bytes=len(text.encode()), import_ms=round(import_ms, 3),
                                               publish_ms=round((time.perf_counter() - before) * 1000, 3)))
                published.append(imported['document_id'])
                known[imported['document_id']] = text
                if len(published) % 25 == 0:
                    progress(f'Published {len(published)} synthetic documents')
            run([pg('psql'), '-X', '-w', '-c', 'ANALYZE'])
            report['setup'].append(dict(documents=scale, chunks=scale,
                                       corpus_utf8_bytes=sum(len(v.encode()) for v in known.values()),
                                       corpus_sha256=hashlib.sha256(''.join(known.values()).encode()).hexdigest()))
            # First semantic invocation builds/rebuilds missing document vectors; separately recorded.
            for mode in ('keyword', 'semantic'):
                warm = request(mode, 0, scale, category='warmup')
                report['controls'].append(dict(control='warmup', **warm))
                if warm['outcome'] != 'success':
                    progress(f'{mode} warmup failed at docs={scale}; recording failure without hiding it')
                for concurrency in (1, 2, 4, 8):
                    cell(mode, scale, concurrency, args.requests)
            if scale == min(args.scales) and args.answer_requests and env.get('DEEPSEEK_API_KEY'):
                for concurrency in (1, 2):
                    cell('answer', scale, concurrency, args.answer_requests // 2)
            if scale == min(args.scales):
                # Draft canary must remain invisible even though its text is in the DB.
                path = source_folder / 'draft-only.txt'
                path.write_text('draftonlyperformancecanary', encoding='utf-8')
                cli('import', path)
                report['controls'].append(dict(control='draft_hidden', **request('keyword', 3, scale,
                    category='negative_control', query_override='draftonlyperformancecanary')))
        report['execution_complete'] = True
        report['overload_gate'] = 'fail' if any(c['concurrency']>4 and c['failures'] for c in report['cells']) else 'pass'
        report['baseline_decision'] = 'fail' if (any(c['baseline_gate'] == 'fail' for c in report['cells'])
            or any(c['outcome']!='success' for c in report['controls']) or report['overload_gate']=='fail') else 'provisional_pass'
        progress('Baseline matrix complete; stopping owned services and verifying cleanup')
    except Exception as error:
        report.update(execution_complete=False, baseline_decision='inconclusive',
                      error_type=type(error).__name__, error=str(error))
        progress('Run interrupted: ' + type(error).__name__ + ': ' + str(error))
    finally:
        stop_monitor.set()
        if thread:
            thread.join(timeout=5)
        try:
            stop_web()
            if pg_attempted:
                p = run([pg('pg_ctl'), '-D', fixture / 'data', '-m', 'fast', '-w', '-t', '30', 'stop'], success=False, timeout=45)
                if p.returncode and run([pg('pg_ctl'), '-D', fixture / 'data', 'status'], success=False).returncode != 3:
                    raise RuntimeError('owned PostgreSQL could not stop')
            report['server_stopped'] = True
            if ollama:
                parent_process = psutil.Process(ollama.pid)
                children = parent_process.children(recursive=True)
                ollama.terminate()
                ollama.wait(timeout=10)
                for child in children:
                    try:
                        child.terminate()
                    except psutil.NoSuchProcess:
                        pass
                psutil.wait_procs(children, timeout=10)
                report['owned_ollama_stopped'] = True
            if fixture.parent != output or not (fixture / '.memorys-owned-performance').is_file():
                raise RuntimeError('fixture cleanup ownership guard failed')
            shutil.rmtree(fixture)
            report['temporary_resources_removed'] = True
        except Exception as error:
            report['cleanup_error'] = type(error).__name__ + ': ' + str(error)
            report['baseline_decision'] = 'inconclusive'
        report['sources_unchanged_during_checks'] = source_before == source_inventory()
        report['frontend_unchanged_during_checks'] = asset_before == {
            p.relative_to(assets).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest()
            for p in assets.rglob('*') if p.is_file()}
        if not report['sources_unchanged_during_checks'] or not report['frontend_unchanged_during_checks']:
            report['baseline_decision'] = 'inconclusive'
        report['finished_at'] = dt.datetime.now(dt.timezone.utc).isoformat()
        save()
    print(json.dumps(dict(report=str(report_path), decision=report['baseline_decision']), ensure_ascii=False), flush=True)
    return 0 if report.get('execution_complete') and not report.get('cleanup_error') else 1


if __name__ == '__main__':
    raise SystemExit(main())

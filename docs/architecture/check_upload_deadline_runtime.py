"""Real HTTP deadline/cancellation ordering regression and before/after evidence."""
import json
from pathlib import Path
import subprocess

from check_architecture import source_inventory

ROOT = Path(__file__).resolve().parents[2]
TESTS = (
    'TestHTTPUploadDeadlineInterruptsClientThatDoesNotDisconnect',
    'TestHTTPUploadReadDeadlineBeforeContextTimer',
    'TestHTTPUploadCancellationBeforeDeadline',
    'TestHTTPUploadDeadlineCleanupFailure',
    'TestHTTPUploadMetadataReadDeadline',
    'TestHTTPUploadEpilogueReadDeadline',
)


def main():
    sources = source_inventory()
    before = json.loads((ROOT / 'docs/architecture/evidence/upload-deadline-before-fix.json').read_text(encoding='utf-8'))
    if before.get('reproduction_confirmed') is not True or before.get('passed') is not False or before.get('exit_code') == 0 or before.get('sources_unchanged_during_checks') is not True or before['stdout'].count('read deadline lost classification: result=context canceled parent=context canceled read=read tcp') != 3 or before['stdout'].count('deadline_passed=true aborted=1') != 3:
        raise ValueError('controlled three-run before-fix read-timeout evidence missing')
    differences = {p for p in set(sources) | set(before['sources']) if sources.get(p) != before['sources'].get(p)}
    command = [str(ROOT / '.tools/go/bin/go.exe'), 'test', '-json', '-count=10', '-run', '^(' + '|'.join(TESTS) + ')$', './pkg/moduletransport']
    result = subprocess.run(command, cwd=ROOT, capture_output=True, text=True, encoding='utf-8', errors='replace', timeout=120)
    events = [json.loads(line) for line in result.stdout.splitlines() if line.strip()]
    counts = {name: 0 for name in TESTS}
    failures = []
    package_pass = False
    for event in events:
        if event['Action'] == 'fail':
            failures.append(event)
        if event['Action'] == 'pass' and event.get('Test'):
            name = event['Test']
            if name not in counts:
                raise ValueError('unexpected upload test result: ' + name)
            counts[name] += 1
        if event['Action'] == 'pass' and not event.get('Test'):
            package_pass = True
    unchanged = source_inventory() == sources
    passed = result.returncode == 0 and not failures and package_pass and all(n == 10 for n in counts.values()) and unchanged
    report = {'passed': passed, 'production_acceptance': False, 'sources': sources, 'sources_unchanged_during_checks': unchanged, 'before_fix_reproductions': 3, 'changed_paths_since_reproduction': sorted(differences), 'tests': counts, 'command': command, 'exit_code': result.returncode, 'events': events, 'stderr': result.stderr, 'scope': 'Actual localhost HTTP/1.1 silent multipart requests; socket deadline wins application timer ordering, cancellation observed before deadline remains canceled, deadline plus abort failure preserves ErrCleanup, metadata/file/epilogue read timeout returns empty receipt and cleans handed-off staging, original HTTP deadline test repeated; six cases ten runs each. Access and staging are controlled test fixtures, not production blob service/Grant/commit or broad stability acceptance.'}
    (ROOT / '.local/architecture-check/upload-deadline-runtime.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    print('HTTP upload deadline regression: ' + ('60/60 passed' if passed else 'failed; see runtime report'))
    return 0 if passed else 1


if __name__ == '__main__':
    raise SystemExit(main())

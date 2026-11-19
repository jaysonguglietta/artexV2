#!/usr/bin/env python3
"""Bounded installed jq security/compatibility checks; no network or credentials."""
import argparse
import json
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess
import tempfile

VERSION = '1.8.2-0artex1'


def run(arguments, *, data=None, timeout=5):
    result = subprocess.run(arguments, input=data, capture_output=True, timeout=timeout)
    if result.returncode < 0 or result.returncode >= 128:
        raise AssertionError('jq fixture crashed instead of returning a handled result')
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--report', type=Path)
    args = parser.parse_args()
    checks = []
    def check(name, condition):
        if not condition:
            raise AssertionError(name)
        checks.append({'check': name, 'passed': True})
        print('PASS ' + name, flush=True)
    executable = shutil.which('jq')
    check('Installed CLI and both native packages identify actual jq1.8.2',
          executable == '/usr/bin/jq' and run([executable, '--version']).stdout == b'jq-1.8.2\n'
          and all(run(['dpkg-query', '-W', '-f=${Status} ${Version}', package]).stdout.decode()
                  == 'install ok installed ' + VERSION for package in ('jq', 'libjq1')))
    libraries = run(['ldd', executable]).stdout.decode()
    match = re.search(r'libjq\.so\.1 => (\S+)', libraries)
    verification = run(['dpkg', '--verify', 'jq', 'libjq1'])
    ownership = run(['dpkg-query', '-S', str(Path(match[1]).resolve())]) if match else None
    if (match is None or ownership.returncode != 0 or not ownership.stdout.startswith(b'libjq1:')
            or verification.returncode != 0 or verification.stdout):
        raise AssertionError('Shared-library/dpkg integrity evidence: ' + json.dumps({
            'ldd': libraries, 'ownership': ownership.stdout.decode() if ownership else None,
            'ownership_error': ownership.stderr.decode() if ownership else None,
            'verification_status': verification.returncode,
            'verification': verification.stdout.decode(), 'verification_error': verification.stderr.decode()}))
    check('CLI loads the new dpkg-owned shared libjq and packages verify cleanly',
          True)
    check('libjq dynamically uses inventoried system Oniguruma instead of bundling it',
          b'libonig.so.5' in run(['ldd', str(Path(match[1]).resolve())]).stdout)
    for package in ('jq', 'libjq1'):
        document = Path('/usr/share/doc') / package
        pin = json.loads((document / 'artex-source.json').read_text())
        check(package + ' retains complete upstream licensing and source identity',
              pin['upstream_version'] == '1.8.2' and pin['package_version'] == VERSION
              and pin['source_sha256'] == '71b8d6e8f5fe81f6c6d0d110e3892251f6ce76ed095abd315e26e6e1193af3af'
              and (document / 'copyright').stat().st_size > 5000)
    with tempfile.TemporaryDirectory(prefix='artex-jq-behavior-') as temporary:
        scratch = Path(temporary)
        normal = run([executable, '-nc', '--arg', 'text', 'quoted " input', '--argjson', 'ports', '[443,80]',
                      '{text:$text,ports:($ports|sort),encoded:("a b"|@uri)}'])
        check('Normal JSON construction, quoting, sorting and URI encoding work', normal.returncode == 0
              and json.loads(normal.stdout) == {'text': 'quoted " input', 'ports': [80, 443], 'encoded': 'a%20b'})
        streamed = run([executable, '-cs', 'map(.port)|sort'], data=b'{"port":443}\n{"port":80}\n')
        check('Multi-record scanning output can be slurped and filtered', streamed.returncode == 0 and json.loads(streamed.stdout) == [80, 443])
        raw = run([executable, '-Rsc', 'explode'], data=b'a\0b\n')
        check('Legitimate raw input preserves embedded NUL bytes', raw.returncode == 0 and json.loads(raw.stdout) == [97, 0, 98, 10])
        malformed = run([executable, '-c', '.'], data=b'{"safe":true}\0{"injected":1}\n')
        check('JSON parser rejects NUL truncation instead of accepting a hidden suffix', malformed.returncode != 0)
        program = scratch / 'nul.jq'
        program.write_bytes(b'.safe\0| .injected')
        check('Program files reject embedded NUL rather than executing a truncated filter',
              run([executable, '-f', str(program)], data=b'{"safe":1}\n').returncode != 0)
        (scratch / 'safe.jq').write_text('def value: 42;')
        module = run([executable, '-n', '-L', str(scratch), 'import "safe\\u0000suffix" as m; m::value'])
        check('Module paths reject embedded NUL before loading another file', module.returncode != 0)
        (scratch / 'cycle_a.jq').write_text('import "cycle_b" as b; def value: b::value;')
        (scratch / 'cycle_b.jq').write_text('import "cycle_a" as a; def value: a::value;')
        cycle = run([executable, '-n', '-L', str(scratch), 'import "cycle_a" as a; a::value'])
        check('Circular module imports fail within a bounded time without crashing', cycle.returncode != 0)
        path = run([executable, '-n', 'setpath([range(0;10001)|"a"];1)'])
        check('Excessive setpath recursion returns a handled depth error', path.returncode == 5 and b'Path too deep' in path.stderr)
        merge = run([executable, '-n', '(reduce range(0;10001) as $i ({}; {a:.})) as $x | $x * $x'])
        check('Excessive recursive object merge returns a handled depth error', merge.returncode == 5 and b'Object merge too deep' in merge.stderr)
        invalid_types = run([executable, '-n', '123 | _strindices("abc")'])
        check('Internal string-index builtin rejects invalid runtime argument types',
              invalid_types.returncode == 5 and b'cannot be searched' in invalid_types.stderr)
        regexp = run([executable, '-nec', '"host.example:443"|capture("^(?<host>[^:]+):(?<port>[0-9]+)$")'])
        check('Regex capture needed by scanning scripts still works', regexp.returncode == 0
              and json.loads(regexp.stdout) == {'host': 'host.example', 'port': '443'})
        check('Exit-status semantics distinguish false and empty results',
              run([executable, '-ne', 'false']).returncode == 1 and run([executable, '-ne', 'empty']).returncode == 4)
    report = {'passed': True, 'upstream_version': '1.8.2', 'package_version': VERSION,
              'platform': platform.system().lower() + '/' + platform.machine(), 'uid': os.getuid(), 'checks': checks}
    if args.report:
        args.report.write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps({'passed': True, 'checks': len(checks)}))


if __name__ == '__main__':
    main()

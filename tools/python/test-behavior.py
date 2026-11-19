#!/usr/bin/env python3
"""Offline installed pip/venv/ensurepip checks using only disposable local wheels.

Run on the built Python3.14 runtime with a read-only root and an executable
private scratch filesystem. No package index, upload or publish operation is used.
"""
import base64
import csv
import hashlib
import importlib.metadata
import importlib.util
import io
import json
import os
import re
from pathlib import Path
import subprocess
import sys
import tempfile
import zipfile

VERSION = '26.3+artex.1'
# Canonical components/dependency graph from the checksum-pinned official wheel,
# with only pkg:pypi/setuptools@70.3.0 and its edges removed.
RETAINED_SBOM_SHA256 = '395aedb208d4087f99953b11e3ef9327130415c34c17398261c55994a5191e2c'
PASSED = []


def run(arguments, environment, *, expected=0):
    result = subprocess.run(arguments, env=environment, capture_output=True, text=True, timeout=45)
    if result.returncode != expected:
        raise AssertionError(f'Unexpected exit {result.returncode} for {arguments[0]}: {result.stderr[-4000:]}')
    return result.stdout


def make_wheel(directory):
    files = {
        'artex_pip_fixture/__init__.py': b'VALUE = "owned offline fixture"\n',
        'artex_pip_fixture/cli.py': b'import json, sys\ndef main():\n    print(json.dumps({"fixture": True, "arguments": sys.argv[1:]}))\n',
        'artex_pip_fixture-1.0.dist-info/METADATA': b'Metadata-Version: 2.4\nName: artex-pip-fixture\nVersion: 1.0\nRequires-Python: >=3.14\n',
        'artex_pip_fixture-1.0.dist-info/WHEEL': b'Wheel-Version: 1.0\nGenerator: artex-owned-offline-fixture\nRoot-Is-Purelib: true\nTag: py3-none-any\n',
        'artex_pip_fixture-1.0.dist-info/entry_points.txt': b'[console_scripts]\nartex-pip-fixture = artex_pip_fixture.cli:main\n',
    }
    record = io.StringIO(newline=''); writer = csv.writer(record, lineterminator='\n')
    for name, data in sorted(files.items()):
        digest = base64.urlsafe_b64encode(hashlib.sha256(data).digest()).decode().rstrip('=')
        writer.writerow([name, 'sha256=' + digest, len(data)])
    name = 'artex_pip_fixture-1.0.dist-info/RECORD'
    writer.writerow([name, '', '']); files[name] = record.getvalue().encode()
    wheel = directory / 'artex_pip_fixture-1.0-py3-none-any.whl'
    with zipfile.ZipFile(wheel, 'w', zipfile.ZIP_DEFLATED) as bundle:
        for name, data in sorted(files.items()): bundle.writestr(name, data)
    return wheel


def verify_installed_inventory():
    import pip
    assert pip.__version__ == VERSION
    distribution = importlib.metadata.distribution('pip')
    assert distribution.version == VERSION
    assert distribution.metadata['Requires-Python'] == '>=3.14'
    package = Path(pip.__file__).parent
    assert not (package / '_vendor/pkg_resources').exists()
    assert not (package / '_internal/metadata/pkg_resources.py').exists()
    assert importlib.util.find_spec('pip._vendor.pkg_resources') is None
    assert importlib.util.find_spec('pip._internal.metadata.pkg_resources') is None
    assert 'setuptools==' not in (package / '_vendor/vendor.txt').read_text()
    bom = json.loads((package / '_vendor/bom.cdx.json').read_text())
    assert bom['bomFormat'] == 'CycloneDX' and bom['version'] == 2
    assert bom['metadata']['component']['version'] == VERSION
    inventory = {key: bom[key] for key in ('components', 'dependencies')}
    assert hashlib.sha256(json.dumps(inventory, sort_keys=True, separators=(',', ':')).encode()).hexdigest() == RETAINED_SBOM_SHA256
    assert len(bom['components']) == 18
    vendor_versions = {name.lower().replace('-', '_'): version for name, version in
                       re.findall(r'^\s*([A-Za-z0-9_.-]+)==([^\s]+)', (package / '_vendor/vendor.txt').read_text(), re.M)}
    for component in bom['components']:
        assert component.get('bom-ref') != 'pkg:pypi/setuptools@70.3.0'
        if component['bom-ref'] == 'bom-ref:pip': continue
        module = component['name'].lower().replace('-', '_')
        assert vendor_versions[module] == component['version']
        assert importlib.util.find_spec('pip._vendor.' + module) is not None
    for dependency in bom['dependencies']:
        assert dependency['ref'] != 'pkg:pypi/setuptools@70.3.0'
        assert 'pkg:pypi/setuptools@70.3.0' not in dependency.get('dependsOn', [])

    files = list(distribution.files)
    assert not any('pkg_resources' in str(file) for file in files)
    for name in distribution.metadata.get_all('License-File', []):
        assert distribution.locate_file(distribution._path.name + '/licenses/' + name).is_file()
    checked = 0
    for path in files:
        if path.hash is None: continue
        assert path.hash.mode == 'sha256'
        data = distribution.locate_file(path).read_bytes()
        digest = base64.urlsafe_b64encode(hashlib.sha256(data).digest()).decode().rstrip('=')
        assert digest == path.hash.value, str(path)
        if path.size is not None: assert len(data) == path.size
        checked += 1
    assert checked > 100
    PASSED.append('honest identity, actual code/license/SBOM inventory consistency and installed RECORD hashes')


def main():
    if sys.version_info < (3, 14):
        raise SystemExit('This fork is supported only on Python >=3.14; use the pinned runtime fixture')
    verify_installed_inventory()
    with tempfile.TemporaryDirectory(prefix='artex-pip-behavior-') as temporary:
        work = Path(temporary)
        environment = {key: value for key, value in os.environ.items() if not key.startswith('PIP_') and key not in ('PYTHONPATH', 'VIRTUAL_ENV')}
        environment.update({'PIP_CONFIG_FILE': os.devnull, 'PIP_NO_INDEX': '1', 'PIP_DISABLE_PIP_VERSION_CHECK': '1',
                            'PIP_NO_CACHE_DIR': '1', 'PYTHONNOUSERSITE': '1', 'PYTHONDONTWRITEBYTECODE': '1',
                            'TMPDIR': str(work), '_PIP_USE_IMPORTLIB_METADATA': '0'})
        python = sys.executable
        assert VERSION in run([python, '-m', 'pip', '--version'], environment)
        packages = json.loads(run([python, '-m', 'pip', 'list', '--format=json'], environment))
        assert any(p['name'] == 'pip' and p['version'] == VERSION for p in packages)
        run([python, '-m', 'pip', 'check'], environment)
        PASSED.append('installed CLI version/list/check without package-index access')

        backend_probe = 'from pip._internal.metadata import select_backend, get_default_environment; assert select_backend().__name__.endswith(".importlib"); assert str(get_default_environment().get_distribution("pip").version)=="' + VERSION + '"'
        run([python, '-c', backend_probe], environment)
        PASSED.append('metadata API uses importlib even when the legacy environment switch requests otherwise')
        assert VERSION in run([python, '-m', 'ensurepip', '--version'], environment)
        PASSED.append('ensurepip reports the replaced bundled wheel version')

        target = work / 'automatic-venv'
        run([python, '-m', 'venv', str(target)], environment)
        vpython = str(target / 'bin/python')
        assert VERSION in run([vpython, '-m', 'pip', '--version'], environment)
        run([vpython, '-c', backend_probe], environment)
        PASSED.append('new venv bootstraps the actual patched wheel offline')

        explicit = work / 'explicit-venv'
        run([python, '-m', 'venv', '--without-pip', str(explicit)], environment)
        explicit_python = str(explicit / 'bin/python')
        run([explicit_python, '-m', 'ensurepip', '--upgrade', '--default-pip'], environment)
        assert VERSION in run([explicit_python, '-m', 'pip', '--version'], environment)
        PASSED.append('explicit ensurepip installs the patched bundle into a pip-free venv')

        wheelhouse = work / 'wheelhouse'; wheelhouse.mkdir()
        wheel = make_wheel(wheelhouse)
        requirements = work / 'requirements.txt'
        requirements.write_text('artex-pip-fixture==1.0 --hash=sha256:' + '0' * 64 + '\n')
        install = [vpython, '-m', 'pip', 'install', '--no-index', '--no-deps', '--require-hashes', '--find-links', str(wheelhouse), '-r', str(requirements)]
        failure = subprocess.run(install, env=environment, capture_output=True, text=True, timeout=45)
        assert failure.returncode != 0 and 'hash' in (failure.stdout + failure.stderr).lower()
        run([vpython, '-c', 'import importlib.util; assert importlib.util.find_spec("artex_pip_fixture") is None'], environment)
        assert not (target / 'bin/artex-pip-fixture').exists()
        PASSED.append('wrong wheel hash is rejected without partial package/command installation')

        requirements.write_text('artex-pip-fixture==1.0 --hash=sha256:' + hashlib.sha256(wheel.read_bytes()).hexdigest() + '\n')
        run(install, environment)
        run([vpython, '-c', 'import artex_pip_fixture, importlib.metadata as m; assert artex_pip_fixture.VALUE=="owned offline fixture"; assert m.version("artex-pip-fixture")=="1.0"'], environment)
        output = json.loads(run([str(target / 'bin/artex-pip-fixture'), '--owned-fixture', 'value'], environment))
        assert output == {'fixture': True, 'arguments': ['--owned-fixture', 'value']}
        PASSED.append('hash-verified offline wheel install, import/metadata and direct console-script execution')

        inspected = json.loads(run([vpython, '-m', 'pip', 'inspect', '--local'], environment))
        assert any(p['metadata']['name'] == 'artex-pip-fixture' for p in inspected['installed'])
        downloads = work / 'downloads'; downloads.mkdir()
        run([vpython, '-m', 'pip', 'download', '--no-index', '--no-deps', '--find-links', str(wheelhouse), '--dest', str(downloads), 'artex-pip-fixture==1.0'], environment)
        assert (downloads / wheel.name).read_bytes() == wheel.read_bytes()
        wheels = work / 'wheels'; wheels.mkdir()
        run([vpython, '-m', 'pip', 'wheel', '--no-index', '--no-deps', '--wheel-dir', str(wheels), str(wheel)], environment)
        assert (wheels / wheel.name).read_bytes() == wheel.read_bytes()
        PASSED.append('inspect, download and wheel CLI workflows retain package metadata/content')

        run([vpython, '-m', 'pip', 'uninstall', '-y', 'artex-pip-fixture'], environment)
        run([vpython, '-c', 'import importlib.util; assert importlib.util.find_spec("artex_pip_fixture") is None'], environment)
        assert not (target / 'bin/artex-pip-fixture').exists()
        run([vpython, '-m', 'pip', 'check'], environment)
        PASSED.append('uninstall removes the owned package and executable; dependency check remains clean')
    print(json.dumps({'python': sys.version, 'platform': sys.platform, 'pip_version': VERSION,
                      'checks_passed': len(PASSED), 'checks': PASSED,
                      'retained_sbom_components': 18, 'retained_sbom_inventory_sha256': RETAINED_SBOM_SHA256,
                      'network': 'All package operations explicitly offline; fixture container must also use network none',
                      'scope': 'Installed CLI/metadata/venv/ensurepip/local-wheel behavior, not the full upstream test suite'}, indent=2))


if __name__ == '__main__':
    main()

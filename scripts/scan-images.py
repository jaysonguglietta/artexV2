#!/usr/bin/env python3
"""Scan exact local Docker image IDs and enforce the reviewed image policy.

Docker exports immutable IDs to private temporary archives. The pinned Trivy
process reads only those archives, with no Docker socket or ambient Trivy config.
Reports retain every severity, unfixed issue, package and suppressed finding.
"""
import argparse
from collections import Counter
from datetime import datetime, timedelta, timezone
import hashlib
import gzip
import importlib.util
import json
import os
from pathlib import Path, PurePosixPath
import re
import subprocess
import tarfile
import tempfile
import urllib.request
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[1]
PLATFORMS = {'linux/amd64', 'linux/arm64'}
SEVERITIES = {'UNKNOWN', 'LOW', 'MEDIUM', 'HIGH', 'CRITICAL'}
EXCEPTION_FIELDS = {'vulnerability_id', 'package', 'installed_version', 'component',
                    'target', 'platform', 'reviewed_at', 'expires', 'reason', 'reference'}


def load_installer():
    spec = importlib.util.spec_from_file_location('artex_install_trivy', ROOT / 'scripts/install-trivy.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def timestamp(value):
    if not isinstance(value, str):
        raise ValueError('Missing or malformed UTC timestamp')
    parsed = datetime.fromisoformat(value.replace('Z', '+00:00'))
    if parsed.tzinfo is None:
        raise ValueError('Timestamp must include a timezone')
    return parsed.astimezone(timezone.utc)


def sanitized_environment(environment, docker_config):
    clean = {key: value for key, value in environment.items() if not key.upper().startswith('TRIVY_')}
    for key in ('DOCKER_HOST', 'DOCKER_CONTEXT', 'DOCKER_CERT_PATH', 'DOCKER_TLS_VERIFY'):
        clean.pop(key, None)
    clean['DOCKER_CONFIG'] = str(docker_config)
    return clean



ADVISORY_ID = re.compile(r"(?:CVE-[0-9]{4}-[0-9]{4,}|GHSA-[a-z0-9]{4}-[a-z0-9]{4}-[a-z0-9]{4})")


class RefuseAdvisoryRedirects(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, response, code, message, headers, new_url):
        raise ValueError("Official advisory verification refuses all redirects")


def fetch_official_advisories(identifier):
    """Read bounded public GitHub advisory evidence; never follow a report URL."""
    if not isinstance(identifier, str) or not ADVISORY_ID.fullmatch(identifier):
        raise ValueError("Withdrawal verification requires an exact CVE/GHSA identifier")
    url = ("https://api.github.com/advisories?cve_id=" + identifier + "&ecosystem=npm&per_page=100"
           if identifier.startswith("CVE-") else "https://api.github.com/advisories/" + identifier)
    request = urllib.request.Request(url, headers={
        "Accept": "application/vnd.github+json", "User-Agent": "ARTEX-image-security/1",
        "X-GitHub-Api-Version": "2022-11-28", "Cache-Control": "no-cache",
    })
    opener = urllib.request.build_opener(RefuseAdvisoryRedirects())
    with opener.open(request, timeout=20) as response:
        if response.geturl() != url or response.getcode() != 200:
            raise ValueError("Unexpected official advisory response origin/status")
        if 'rel="next"' in response.headers.get("Link", ""):
            raise ValueError("Paginated advisory evidence is ambiguous; review manually")
        raw = response.read(4 * 1024 * 1024 + 1)
    if len(raw) > 4 * 1024 * 1024:
        raise ValueError("Official advisory response exceeds the evidence budget")
    payload = json.loads(raw)
    records = payload if identifier.startswith("CVE-") else [payload]
    if not isinstance(records, list) or not 0 < len(records) <= 100:
        raise ValueError("Missing/ambiguous official advisory records")
    return {"source": url, "response_sha256": hashlib.sha256(raw).hexdigest(), "advisories": records}


class WithdrawalVerifier:
    """Cache live primary evidence within one scan; no persisted approval cache."""
    def __init__(self, fetch=None, now=None):
        self.fetch = fetch or fetch_official_advisories
        self.now = now
        self.responses = {}

    def verify(self, identifier, package):
        if not isinstance(identifier, str) or not ADVISORY_ID.fullmatch(identifier):
            raise ValueError("Unrecognized advisory identity")
        if identifier not in self.responses:
            try:
                evidence = self.fetch(identifier)
                self.responses[identifier] = {"response": evidence, "checked_at": (self.now or datetime.now(timezone.utc)).isoformat()}
            except (ValueError, OSError) as error:
                self.responses[identifier] = {"error": str(error)}
        cached = self.responses[identifier]
        if "error" in cached:
            raise ValueError("Official withdrawal verification unavailable: " + cached["error"])
        evidence = cached["response"]
        records = evidence.get("advisories")
        if not isinstance(records, list) or not 0 < len(records) <= 100:
            raise ValueError("Missing/ambiguous advisory identity evidence")
        seen = set()
        for record in records:
            if not isinstance(record, dict):
                raise ValueError("Malformed official advisory record")
            ghsa, cve = record.get("ghsa_id"), record.get("cve_id")
            if not isinstance(ghsa, str) or not re.fullmatch(r"GHSA-[a-z0-9]{4}-[a-z0-9]{4}-[a-z0-9]{4}", ghsa) or ghsa in seen:
                raise ValueError("Missing/duplicate official GHSA identity")
            seen.add(ghsa)
            if identifier.startswith("CVE-") and cve != identifier:
                raise ValueError("Official advisory CVE identity mismatch")
            if identifier.startswith("GHSA-") and ghsa != identifier:
                raise ValueError("Official advisory GHSA identity mismatch")
            if cve is not None and (not isinstance(cve, str) or not re.fullmatch(r"CVE-[0-9]{4}-[0-9]{4,}", cve)):
                raise ValueError("Malformed official CVE identity")
            affected = record.get("vulnerabilities")
            if not isinstance(affected, list) or not any(
                isinstance(item, dict) and isinstance(item.get("package"), dict)
                and item["package"].get("ecosystem") == "npm" and item["package"].get("name") == package
                for item in affected
            ):
                raise ValueError("Official advisory does not identify the exact npm package")
            withdrawn = timestamp(record.get("withdrawn_at"))
            if withdrawn > timestamp(cached['checked_at']):
                raise ValueError("Official withdrawal date is in the future")
        return {"classification": "verified_withdrawn", "identifier": identifier,
                "package": package, "ecosystem": "npm", "checked_at": cached["checked_at"], **evidence}


def validate_policy(policy, now):
    if policy.get('schema_version') != 1:
        raise ValueError('Unsupported image-policy schema')
    if any(type(policy.get(key)) is not int for key in ('max_database_age_hours', 'max_exception_days')):
        raise ValueError('Database/exception limits must be integer bounds')
    if not 0 < policy['max_database_age_hours'] <= 72 or not 0 < policy['max_exception_days'] <= 30:
        raise ValueError('Database/exception limits cannot exceed 72 hours/30 days')
    if not isinstance(policy.get('components'), dict) or set(policy['components']) != {'application', 'worker', 'broker', 'postgres'}:
        raise ValueError('Image policy must describe all four supported components')
    if type(policy.get('max_image_bytes')) is not int or not 0 < policy['max_image_bytes'] <= 16 * 1024 ** 3:
        raise ValueError('Invalid maximum image size')
    for component, settings in policy['components'].items():
        if not isinstance(settings, dict) or not isinstance(settings.get('os_families'), list):
            raise ValueError('Malformed component OS policy')
        allowed_fields = {'os_families', 'os_major_versions', 'os_versions', 'allow_no_os_go_target', 'required_go_module', 'required_go_targets', 'required_node_packages'}
        if set(settings) - allowed_fields or any(x not in ('debian', 'ubuntu', 'alpine') for x in settings['os_families']):
            raise ValueError('Unknown component OS policy fields or family')
        if 'os_major_versions' in settings and (not isinstance(settings['os_major_versions'], list)
                or not settings['os_major_versions'] or any(not isinstance(x, str) or not x.isdecimal() for x in settings['os_major_versions'])):
            raise ValueError('OS major versions must be explicit numeric strings')
        if 'os_versions' in settings and (not isinstance(settings['os_versions'], list) or not settings['os_versions']
                or any(not isinstance(x, str) or not re.fullmatch(r'[0-9]+(?:\.[0-9]+)*', x) for x in settings['os_versions'])):
            raise ValueError('OS versions must be explicit numeric version strings')
        required_go = settings.get('required_go_targets', {})
        if not isinstance(required_go, dict) or any(not isinstance(k, str) or not k or not isinstance(v, str) or not v
                or any(c in k + v for c in '*?[]\0\r\n') for k, v in required_go.items()):
            raise ValueError('Required Go targets must be exact nonempty paths/modules')
        if component in ('worker', 'broker') and not required_go:
            raise ValueError('Worker/broker must inventory their executable Go targets')
        required_node = settings.get('required_node_packages', [])
        if not isinstance(required_node, list) or any(not isinstance(x, str) or not x for x in required_node):
            raise ValueError('Required Node packages must be exact names')
        if component == 'worker' and not {'npm', 'playwright', 'playwright-core'} <= set(required_node):
            raise ValueError('Worker must inventory npm and browser packages')
        if component != 'application' and any(x in settings for x in ('allow_no_os_go_target', 'required_go_module')):
            raise ValueError('Only the minimal application may omit OS detection')
        if component == 'application' and (settings.get('allow_no_os_go_target') != 'app/artex'
                or settings.get('required_go_module') != 'github.com/Autumn-27/artex'):
            raise ValueError('Minimal application must inventory its precise Go binary/module')
    exceptions = policy.get('exceptions')
    if not isinstance(exceptions, list):
        raise ValueError('Exceptions must be an explicit list')
    seen = set()
    for item in exceptions:
        if not isinstance(item, dict) or set(item) != EXCEPTION_FIELDS:
            raise ValueError('Exception fields must be exact and complete')
        if not all(isinstance(value, str) and value.strip() and not any(c in value for c in '\0\r\n') for value in item.values()):
            raise ValueError('Exception values must be nonempty single-line strings')
        if item['component'] not in policy['components'] or item['platform'] not in PLATFORMS:
            raise ValueError('Exception component/platform is unknown')
        if not re.fullmatch(r'CVE-[0-9]{4}-[0-9]{4,}', item['vulnerability_id']):
            raise ValueError('Exceptions must identify one exact CVE')
        if any(any(c in item[key] for c in '*?[]') for key in ('package', 'installed_version', 'component', 'target', 'platform')):
            raise ValueError('Wildcard exceptions are forbidden')
        reviewed = timestamp(item['reviewed_at'])
        expiry = timestamp(item['expires'])
        if reviewed > now + timedelta(minutes=5) or not reviewed < expiry <= reviewed + timedelta(days=policy['max_exception_days']):
            raise ValueError('Exception review/expiry exceeds the bounded review window')
        if expiry <= now:
            raise ValueError('Expired vulnerability exception: ' + item['vulnerability_id'])
        reference = urlsplit(item['reference'])
        if reference.scheme != 'https' or not reference.hostname or reference.username or reference.password or reference.fragment:
            raise ValueError('Exception requires a primary HTTPS advisory/reference without credentials')
        if len(item['reason'].strip()) < 20:
            raise ValueError('Exception requires a concrete reviewed justification')
        key = exception_key(item)
        if key in seen:
            raise ValueError('Duplicate vulnerability exception')
        seen.add(key)
    return exceptions


def exception_key(item):
    return tuple(item[key] for key in ('component', 'platform', 'target', 'vulnerability_id', 'package', 'installed_version'))


def database_identity(cache, policy, now):
    installer = load_installer()
    path = Path(cache) / 'db/metadata.json'
    database = Path(cache) / 'db/trivy.db'
    if path.is_symlink() or database.is_symlink() or not path.is_file() or not database.is_file():
        raise ValueError('Trivy database and metadata must be regular local files')
    metadata = json.loads(path.read_text())
    if metadata.get('Version') != 2 or not 0 < database.stat().st_size <= 2 * 1024 ** 3:
        raise ValueError('Missing/unsupported Trivy database inventory')
    updated, downloaded = timestamp(metadata.get('UpdatedAt')), timestamp(metadata.get('DownloadedAt'))
    if updated > now + timedelta(minutes=5) or downloaded > now + timedelta(minutes=5):
        raise ValueError('Trivy database metadata is in the future')
    if now - updated > timedelta(hours=policy['max_database_age_hours']):
        raise ValueError('Trivy vulnerability database is older than the allowed freshness window')
    timestamp(metadata.get('NextUpdate'))
    return {'repository': json.loads((ROOT / 'security/tool-pins.json').read_text())['trivy']['db_repository'],
            'metadata': metadata, 'database_sha256': installer.sha256_file(database),
            'metadata_sha256': installer.sha256_file(path)}


def run(command, *, environment=None, cwd=None, timeout=600):
    result = subprocess.run(command, capture_output=True, text=True, env=environment, cwd=cwd, timeout=timeout)
    if result.returncode:
        raise ValueError(f'{Path(command[0]).name} failed (exit {result.returncode}): {result.stderr[-4000:]}')
    return result.stdout


def trivy_arguments(executable, cache):
    return [str(executable), '--cache-dir', str(cache), '--config', '', '--timeout', '10m', '--quiet']


def scan_arguments(executable, cache, archive, output):
    return trivy_arguments(executable, cache) + [
        'image', '--input', str(archive), '--format', 'json', '--output', str(output),
        '--scanners', 'vuln', '--pkg-types', 'os,library', '--severity',
        'UNKNOWN,LOW,MEDIUM,HIGH,CRITICAL', '--ignorefile', '', '--ignore-policy', '',
        '--ignore-unfixed=false', '--ignore-status', '', '--show-suppressed',
        '--list-all-pkgs=true', '--offline-scan', '--skip-db-update',
        '--skip-java-db-update', '--disable-telemetry', '--skip-version-check',
        '--detection-priority', 'comprehensive', '--parallel', '2',
    ]


def inspect_image(reference, requested_platform, policy):
    if not reference or reference.startswith('-') or any(c in reference for c in '\0\r\n'):
        raise ValueError('Invalid Docker image reference')
    images = json.loads(run(['docker', 'image', 'inspect', '--', reference], timeout=30))
    if not isinstance(images, list) or len(images) != 1:
        raise ValueError('Docker must resolve exactly one local image')
    image = images[0]
    if not isinstance(image, dict):
        raise ValueError('Malformed Docker image inspection')
    identity = image.get('Id', '')
    actual_platform = image.get('Os', '') + '/' + image.get('Architecture', '')
    if not re.fullmatch(r'sha256:[0-9a-f]{64}', identity) or actual_platform not in PLATFORMS:
        raise ValueError('Unsupported Docker image ID/platform')
    if requested_platform and actual_platform != requested_platform:
        raise ValueError('Docker image platform does not match the requested platform')
    if not isinstance(image.get('Size'), int) or not 0 < image['Size'] <= policy['max_image_bytes']:
        raise ValueError('Docker image exceeds the bounded image size')
    rootfs = image.get('RootFS')
    if not isinstance(rootfs, dict) or rootfs.get('Type') != 'layers' or not isinstance(rootfs.get('Layers'), list) or not rootfs['Layers'] or any(not isinstance(x, str) or not re.fullmatch(r'sha256:[0-9a-f]{64}', x) for x in rootfs['Layers']):
        raise ValueError('Docker inspection lacks immutable root filesystem layers')
    return {'reference': reference, 'image_id': identity, 'platform': actual_platform, 'rootfs_diff_ids': rootfs['Layers'],
            'repo_digests': image.get('RepoDigests') or [], 'size': image['Size']}



DIGEST = re.compile(r"sha256:[0-9a-f]{64}")
MAX_ARCHIVE_JSON = 8 * 1024 * 1024


def archive_identity(archive, image, maximum_bytes):
    """Authenticate Docker/OCI archive content back to the inspected immutable ID.

    Docker's containerd store may expose an index ID while Trivy identifies the
    selected config. Neither identifier is substituted without verifying the full
    index/manifest/config chain and all selected layer/diffID bindings.
    """
    expected_layers = image.get('rootfs_diff_ids')
    if not isinstance(expected_layers, list) or not expected_layers or any(not isinstance(x, str) or not DIGEST.fullmatch(x) for x in expected_layers):
        raise ValueError('Missing inspected root filesystem layer identity')
    with tarfile.open(archive, mode='r:') as bundle:
        members = {}
        for number, member in enumerate(bundle, 1):
            name = member.name.removeprefix('./')
            path = PurePosixPath(name)
            if number > 10000 or len(name) > 4096 or path.is_absolute() or '..' in path.parts or name in members:
                raise ValueError('Unsafe, duplicate or oversized image archive inventory')
            members[name] = member

        def regular(name, limit):
            member = members.get(name)
            if member is None or not member.isreg() or member.issparse() or not 0 < member.size <= limit:
                raise ValueError('Missing/nonregular/oversized archive member: ' + str(name))
            return member

        def unique_object(pairs):
            result = {}
            for key, value in pairs:
                if key in result:
                    raise ValueError('Duplicate image metadata key')
                result[key] = value
            return result

        def document(name, descriptor=None):
            member = regular(name, MAX_ARCHIVE_JSON)
            with bundle.extractfile(member) as source:
                raw = source.read(MAX_ARCHIVE_JSON + 1)
            if len(raw) != member.size:
                raise ValueError('Truncated archive metadata')
            digest = 'sha256:' + hashlib.sha256(raw).hexdigest()
            if descriptor and (descriptor.get('digest') != digest or descriptor.get('size') != len(raw)):
                raise ValueError('Archive metadata descriptor digest/size mismatch')
            return json.loads(raw, object_pairs_hook=unique_object), digest

        def blob(descriptor):
            if not isinstance(descriptor, dict) or not isinstance(descriptor.get('digest'), str) or not DIGEST.fullmatch(descriptor['digest']):
                raise ValueError('Unsupported OCI blob digest')
            return 'blobs/sha256/' + descriptor['digest'].split(':')[1]

        legacy, _ = document('manifest.json')
        if not isinstance(legacy, list) or len(legacy) != 1 or not isinstance(legacy[0], dict):
            raise ValueError('Export must have exactly one Docker image manifest')
        config_path = legacy[0].get('Config')
        layer_paths = legacy[0].get('Layers')
        if not isinstance(config_path, str) or not isinstance(layer_paths, list) or len(layer_paths) != len(expected_layers) or any(not isinstance(x, str) for x in layer_paths):
            raise ValueError('Docker manifest config/layer inventory mismatch')
        config, config_digest = document(config_path)
        if not isinstance(config, dict) or config.get('os', '') + '/' + config.get('architecture', '') != image['platform']:
            raise ValueError('Authenticated archive config platform mismatch')
        rootfs = config.get('rootfs')
        if not isinstance(rootfs, dict) or rootfs.get('type') != 'layers' or rootfs.get('diff_ids') != expected_layers:
            raise ValueError('Archive config rootfs differs from Docker inspection')
        chain = []
        selected = None
        if image['image_id'] != config_digest:
            index, index_digest = document('index.json')
            if not isinstance(index, dict) or index.get('schemaVersion') != 2 or not isinstance(index.get('manifests'), list):
                raise ValueError('Missing OCI index binding for non-config Docker ID')
            if index_digest == image['image_id']:
                current, current_digest = index, index_digest
            else:
                roots = [d for d in index['manifests'] if isinstance(d, dict) and d.get('digest') == image['image_id']]
                if len(roots) != 1:
                    raise ValueError('OCI export index does not uniquely bind the inspected ID')
                current, current_digest = document(blob(roots[0]), roots[0])
            for _ in range(8):
                if not isinstance(current, dict) or current.get('schemaVersion') != 2:
                    raise ValueError('Malformed OCI identity chain')
                chain.append(current_digest)
                if 'config' in current:
                    selected = current
                    break
                choices = current.get('manifests')
                if not isinstance(choices, list) or len(choices) > 128:
                    raise ValueError('Malformed OCI platform manifest list')
                candidates = [d for d in choices if isinstance(d, dict) and isinstance(d.get('platform'), dict)
                              and d['platform'].get('os', '') + '/' + d['platform'].get('architecture', '') == image['platform']]
                if len(candidates) != 1:
                    raise ValueError('OCI platform selection is missing or ambiguous')
                descriptor = candidates[0]
                current, current_digest = document(blob(descriptor), descriptor)
            if selected is None:
                raise ValueError('OCI identity chain exceeds its depth limit')
            selected_config, selected_digest = document(blob(selected.get('config')), selected['config'])
            if selected_digest != config_digest or selected_config != config:
                raise ValueError('OCI and Docker manifest configs disagree')
        checked_layers = {}
        expanded = 0

        def layer_identity(name, descriptor=None):
            nonlocal expanded
            if name in checked_layers:
                raw_digest, diff_id = checked_layers[name]
                if descriptor and descriptor.get('digest') != raw_digest:
                    raise ValueError('OCI layer descriptor disagrees with Docker layer')
                return diff_id
            member = regular(name, maximum_bytes)
            digest = hashlib.sha256()
            with bundle.extractfile(member) as source:
                magic = source.read(2)
                digest.update(magic)
                for chunk in iter(lambda: source.read(1024 * 1024), b''):
                    digest.update(chunk)
            raw_digest = 'sha256:' + digest.hexdigest()
            if descriptor and (descriptor.get('digest') != raw_digest or descriptor.get('size') != member.size):
                raise ValueError('OCI layer blob digest/size mismatch')
            with bundle.extractfile(member) as source:
                decoded = gzip.GzipFile(fileobj=source) if magic == b'\x1f\x8b' else source
                diff = hashlib.sha256()
                try:
                    for chunk in iter(lambda: decoded.read(1024 * 1024), b''):
                        expanded += len(chunk)
                        if expanded > maximum_bytes:
                            raise ValueError('Image layer expansion exceeds its bound')
                        diff.update(chunk)
                finally:
                    if decoded is not source:
                        decoded.close()
            diff_id = 'sha256:' + diff.hexdigest()
            checked_layers[name] = (raw_digest, diff_id)
            return diff_id

        if selected is not None:
            descriptors = selected.get('layers')
            if not isinstance(descriptors, list) or len(descriptors) != len(expected_layers):
                raise ValueError('OCI layer count differs from the authenticated config')
            for descriptor, expected in zip(descriptors, expected_layers):
                if not isinstance(descriptor, dict) or descriptor.get('mediaType') not in {
                    'application/vnd.oci.image.layer.v1.tar', 'application/vnd.oci.image.layer.v1.tar+gzip',
                    'application/vnd.docker.image.rootfs.diff.tar', 'application/vnd.docker.image.rootfs.diff.tar.gzip',
                }:
                    raise ValueError('Unsupported OCI layer media type')
                if layer_identity(blob(descriptor), descriptor) != expected:
                    raise ValueError('OCI layer contents disagree with inspected rootfs diffIDs')
        for name, expected in zip(layer_paths, expected_layers):
            if layer_identity(name) != expected:
                raise ValueError('Docker layer contents disagree with inspected rootfs diffIDs')
    return {'config_digest': config_digest, 'rootfs_diff_ids': expected_layers,
            'archive_identity': {'format': 'oci-chain' if chain else 'docker-config',
                                 'exported_image_id': image['image_id'], 'oci_chain': chain,
                                 'config_digest': config_digest, 'platform': image['platform'],
                                 'verified_layers': len(expected_layers)}}


def evaluate_report(report, component, image, policy, exceptions, scanner_version, withdrawal_verifier=None):
    if not isinstance(report, dict) or report.get('SchemaVersion') != 2:
        raise ValueError('Malformed/unsupported Trivy JSON report')
    if not isinstance(report.get('Trivy'), dict) or report['Trivy'].get('Version') != scanner_version or report.get('ArtifactType') != 'container_image':
        raise ValueError('Trivy report scanner/artifact identity mismatch')
    timestamp(report.get('CreatedAt'))
    metadata = report.get('Metadata')
    if not isinstance(metadata, dict) or metadata.get('ImageID') != image.get('config_digest'):
        raise ValueError('Trivy report config ID differs from the authenticated exported config')
    if metadata.get('DiffIDs') != image.get('rootfs_diff_ids'):
        raise ValueError('Trivy report layer diffIDs differ from the authenticated image')
    config = metadata.get('ImageConfig', {})
    if not isinstance(config, dict):
        raise ValueError('Malformed image platform metadata')
    if config.get('os', '') + '/' + config.get('architecture', '') != image['platform']:
        raise ValueError('Trivy report platform differs from the inspected image')
    results = report.get('Results')
    if not isinstance(results, list) or not results:
        raise ValueError('Trivy report lacks package inventory')
    rules = policy['components'][component]
    operating_system = metadata.get('OS') or {}
    if not isinstance(operating_system, dict):
        raise ValueError('Malformed OS metadata')
    if operating_system.get('EOSL', False) is not False:
        raise ValueError('Image OS is end-of-life or has malformed EOL status')
    family, version = operating_system.get('Family'), operating_system.get('Name')
    os_present = bool(family and family not in ('none', 'unknown'))
    if os_present:
        if family not in rules.get('os_families', []) or not isinstance(version, str) or not re.fullmatch(r'[0-9]+(?:\.[0-9]+)*', version):
            raise ValueError('Unexpected or unsupported OS family/version for ' + component)
        versions = rules.get('os_versions')
        if versions and version not in versions:
            raise ValueError('Image OS version does not match reviewed policy')
        majors = rules.get('os_major_versions')
        if majors and version.split('.')[0] not in majors:
            raise ValueError('Image OS major version does not match reviewed policy')
        if not any(isinstance(r, dict) and r.get('Class') == 'os-pkgs' and r.get('Type') == family
                   and isinstance(r.get('Packages'), list) and r['Packages'] for r in results):
            raise ValueError('OS detected without an OS package inventory')
        coverage = {'os': operating_system, 'os_scan': 'OS packages inventoried and evaluated'}
    else:
        target = rules.get('allow_no_os_go_target')
        inventory = [r for r in results if isinstance(r, dict) and r.get('Class') == 'lang-pkgs'
                     and r.get('Type') == 'gobinary' and isinstance(r.get('Target'), str) and r['Target'].removeprefix('/') == target
                     and isinstance(r.get('Packages'), list)]
        if component != 'application' or not target or not any(
            {p.get('Name') for p in r['Packages'] if isinstance(p, dict)} >= {rules.get('required_go_module'), 'stdlib'}
            and any(isinstance(p, dict) and p.get('Name') == 'stdlib' and isinstance(p.get('Version'), str) and p['Version'] for p in r['Packages'])
            for r in inventory
        ):
            raise ValueError('Missing OS is allowed only for the inventoried static application Go binary')
        coverage = {'os': None, 'os_scan': 'Not applicable: minimal application has no detected OS package inventory; Go binary inventoried'}
    go_coverage = {}
    for target, module in rules.get('required_go_targets', {}).items():
        inventories = [r for r in results if isinstance(r, dict) and r.get('Class') == 'lang-pkgs'
                       and r.get('Type') == 'gobinary' and isinstance(r.get('Target'), str)
                       and r['Target'].removeprefix('/') == target and isinstance(r.get('Packages'), list)]
        valid = [r for r in inventories if {p.get('Name') for p in r['Packages'] if isinstance(p, dict)} >= {module, 'stdlib'}
                 and any(isinstance(p, dict) and p.get('Name') == 'stdlib' and isinstance(p.get('Version'), str) and p['Version'] for p in r['Packages'])]
        if not valid:
            raise ValueError('Missing required Go executable/module inventory: ' + target)
        go_coverage[target] = {'required_module': module, 'package_entries': len(valid[0]['Packages'])}
    node_versions = {}
    for package in rules.get('required_node_packages', []):
        versions = {p['Version'] for r in results if isinstance(r, dict) and r.get('Class') == 'lang-pkgs'
                    and r.get('Type') == 'node-pkg' and isinstance(r.get('Packages'), list)
                    for p in r['Packages'] if isinstance(p, dict) and p.get('Name') == package
                    and isinstance(p.get('Version'), str) and p['Version']}
        if not versions:
            raise ValueError('Missing required worker npm/browser package inventory: ' + package)
        node_versions[package] = sorted(versions)
    coverage['required_go_inventory'] = go_coverage
    coverage['required_node_inventory'] = node_versions
    counts, blocking, accepted, withdrawn, matched = Counter(), [], [], [], set()
    lookup = {exception_key(item): item for item in exceptions}
    packages = 0
    for result in results:
        if not isinstance(result, dict) or not isinstance(result.get('Target'), str) or not result['Target']:
            raise ValueError('Malformed package result')
        if result.get('Class') not in ('os-pkgs', 'lang-pkgs') or not isinstance(result.get('Packages'), list) or not result['Packages']:
            raise ValueError('Missing or unrecognized package inventory')
        if any(not isinstance(p, dict) or not isinstance(p.get('Name'), str) or not p['Name'] for p in result['Packages']):
            raise ValueError('Malformed package inventory entry')
        packages += len(result['Packages'])
        target = 'os:' + str(family) + ':' + str(version) if result['Class'] == 'os-pkgs' else result['Target'].removeprefix('/')
        vulnerabilities = result.get('Vulnerabilities', [])
        if vulnerabilities is None:
            vulnerabilities = []
        if not isinstance(vulnerabilities, list):
            raise ValueError('Malformed vulnerability list')
        # Trivy includes ignored findings under this separate collection. They
        # remain subject to our gate; scanner/VEX status cannot authorize a waiver.
        vulnerabilities = list(vulnerabilities)
        suppressed = result.get('ExperimentalModifiedFindings', []) or []
        if not isinstance(suppressed, list):
            raise ValueError('Malformed suppressed findings')
        for modified in suppressed:
            if not isinstance(modified, dict) or modified.get('Type') != 'vulnerability' or not isinstance(modified.get('Finding'), dict):
                raise ValueError('Unknown suppressed-finding schema; review scanner output')
            vulnerabilities.append(modified['Finding'])
        for vulnerability in vulnerabilities:
            if not isinstance(vulnerability, dict):
                raise ValueError('Malformed vulnerability record')
            severity = vulnerability.get('Severity')
            identifier, package, installed = (vulnerability.get(k) for k in ('VulnerabilityID', 'PkgName', 'InstalledVersion'))
            if severity not in SEVERITIES or not all(isinstance(x, str) and x.strip() for x in (identifier, package, installed)):
                raise ValueError('Unknown/missing vulnerability ID, severity, package or installed version')
            counts[severity] += 1
            finding = {'component': component, 'platform': image['platform'], 'target': target,
                       'vulnerability_id': identifier, 'package': package, 'installed_version': installed,
                       'severity': severity, 'fixed_version': vulnerability.get('FixedVersion'),
                       'status': vulnerability.get('Status'), 'reference': vulnerability.get('PrimaryURL')}
            key = exception_key(finding)
            exception = lookup.get(key)
            if exception:
                matched.add(key)
                accepted.append({'finding': finding, 'exception': exception})
            elif severity in ('HIGH', 'CRITICAL', 'UNKNOWN'):
                if result['Class'] == 'lang-pkgs' and result.get('Type') == 'node-pkg' and withdrawal_verifier and ADVISORY_ID.fullmatch(identifier):
                    try:
                        proof = withdrawal_verifier.verify(identifier, package)
                    except (ValueError, OSError) as error:
                        finding['withdrawal_verification_error'] = str(error)
                    else:
                        withdrawn.append({'finding': finding, 'proof': proof})
                        continue
                blocking.append(finding)
    return {'component': component, **image, **coverage, 'package_entries': packages,
            'counts': {s: counts[s] for s in sorted(SEVERITIES)}, 'blocking': blocking,
            'reviewed_exceptions': accepted, 'verified_withdrawn': withdrawn}, matched


def check_unused_exceptions(exceptions, matched, selected):
    unused = [item for item in exceptions if (item['component'], item['platform']) in selected
              and exception_key(item) not in matched]
    if unused:
        raise ValueError('Stale exception does not match a finding in selected component/platform: ' + unused[0]['vulnerability_id'])


def scan_images(arguments):
    now = datetime.now(timezone.utc)
    policy = json.loads((ROOT / 'security/image-policy.json').read_text())
    exceptions = validate_policy(policy, now)
    selected = {}
    for argument in arguments.image:
        component, separator, reference = argument.partition('=')
        if not separator or component not in policy['components'] or component in selected:
            raise ValueError('Use one --image component=reference per known component')
        selected[component] = inspect_image(reference, arguments.platform, policy)
    output = arguments.output.expanduser().absolute()
    if output.is_symlink() or (output.exists() and (not output.is_dir() or any(output.iterdir()))):
        raise ValueError('Report output must be a new or empty directory')
    output.mkdir(parents=True, exist_ok=True, mode=0o700)
    summary = {'schema_version': 1, 'created_at': now.isoformat(), 'policy_sha256': load_installer().sha256_file(ROOT / 'security/image-policy.json'),
               'status': 'error', 'images': [], 'errors': []}
    try:
        installer = load_installer()
        executable, provenance = installer.install(arguments.trivy)
        cache = arguments.cache_dir.expanduser().absolute()
        cache.mkdir(parents=True, exist_ok=True, mode=0o700)
        with tempfile.TemporaryDirectory(prefix='artex-image-security-') as temporary:
            work = Path(temporary)
            empty_config = work / 'empty-docker-config'
            empty_config.mkdir()
            environment = sanitized_environment(os.environ, empty_config)
            version = run([str(executable), '--version'], environment=environment, cwd=work, timeout=30)
            if not re.search(r'^Version: ' + re.escape(provenance['version']) + r'$', version, re.M):
                raise ValueError('Trivy executable reported an unexpected version')
            summary['scanner'] = provenance
            try:
                database = database_identity(cache, policy, now)
            except (ValueError, OSError, json.JSONDecodeError):
                pins, _ = installer.release_pin()
                run(trivy_arguments(executable, cache) + ['image', '--download-db-only', '--db-repository', pins['db_repository'],
                    '--ignorefile', '', '--ignore-policy', '', '--disable-telemetry', '--skip-version-check'],
                    environment=environment, cwd=work, timeout=660)
                database = database_identity(cache, policy, datetime.now(timezone.utc))
            summary['database'] = database
            matched = set()
            withdrawals = WithdrawalVerifier()
            for component, image in selected.items():
                archive = work / (component + '.tar')
                run(['docker', 'image', 'save', '--output', str(archive), '--', image['image_id']], timeout=600)
                if archive.is_symlink() or not archive.is_file() or not 0 < archive.stat().st_size <= policy['max_image_bytes'] + 512 * 1024 ** 2:
                    raise ValueError('Exported image archive exceeds the bounded size')
                image.update(archive_identity(archive, image, policy['max_image_bytes'] + 512 * 1024 ** 2))
                if installer.existing_digest(executable) != provenance['verified_binary_sha256']:
                    raise ValueError('Trivy executable changed after verification')
                report_path = output / (component + '.trivy.json')
                run(scan_arguments(executable, cache, archive, report_path), environment=environment, cwd=work, timeout=660)
                report = json.loads(report_path.read_text())
                component_summary, used = evaluate_report(report, component, image, policy, exceptions, provenance['version'], withdrawals)
                matched.update(used)
                sbom_path = output / (component + '.cdx.json')
                run(trivy_arguments(executable, cache) + ['convert', '--format', 'cyclonedx', '--output', str(sbom_path),
                    '--ignorefile', '', '--ignore-policy', '', '--show-suppressed', str(report_path)],
                    environment=environment, cwd=work, timeout=300)
                sbom = json.loads(sbom_path.read_text())
                if sbom.get('bomFormat') != 'CycloneDX' or not isinstance(sbom.get('components'), list) or not sbom['components']:
                    raise ValueError('CycloneDX SBOM lacks package inventory')
                component_summary['artifacts'] = {path.name: installer.sha256_file(path) for path in (report_path, sbom_path)}
                summary['images'].append(component_summary)
                archive.unlink()
            check_unused_exceptions(exceptions, matched, {(c, i['platform']) for c, i in selected.items()})
            if database_identity(cache, policy, datetime.now(timezone.utc)) != database:
                raise ValueError('Trivy vulnerability database changed during the scan')
            summary['status'] = 'blocked' if any(item['blocking'] for item in summary['images']) else 'passed'
    except (ValueError, OSError, json.JSONDecodeError, subprocess.SubprocessError, tarfile.TarError) as error:
        summary['errors'].append(str(error))
    (output / 'summary.json').write_text(json.dumps(summary, indent=2) + '\n')
    print(json.dumps({'status': summary['status'], 'output': str(output), 'images': len(summary['images']),
                      'blocking_findings': sum(len(item['blocking']) for item in summary['images']), 'errors': summary['errors']}, indent=2))
    return 0 if summary['status'] == 'passed' else 1


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--image', action='append', required=True)
    parser.add_argument('--platform', choices=sorted(PLATFORMS))
    parser.add_argument('--trivy', type=Path, default=Path.home() / '.cache/artex-security-tools/trivy-0.76.2')
    parser.add_argument('--cache-dir', type=Path, default=Path.home() / '.cache/artex-trivy')
    args = parser.parse_args()
    try:
        return scan_images(args)
    except (ValueError, OSError, json.JSONDecodeError, subprocess.SubprocessError, tarfile.TarError) as error:
        parser.exit(1, f'Image scan refused: {error}\n')


if __name__ == '__main__':
    raise SystemExit(main())

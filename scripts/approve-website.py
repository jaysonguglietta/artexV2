#!/usr/bin/env python3
"""Operator-only target approval. Never called by the API or assessment agents."""
import argparse
import ipaddress
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
from urllib.parse import urlsplit


def public_addresses(host):
    addresses = sorted({row[4][0] for row in socket.getaddrinfo(host, 443, socket.AF_INET, socket.SOCK_STREAM)})
    if not addresses or any(not ipaddress.ip_address(ip).is_global for ip in addresses):
        raise ValueError('The guided approval requires public IPv4 addresses. Private targets need a reviewed operator policy.')
    return addresses


def target_host(value):
    url = urlsplit(value)
    if url.scheme != 'https' or not url.hostname or url.username or url.password or url.port not in (None, 443):
        raise ValueError('Use an HTTPS website without credentials or a custom port.')
    return url.hostname


def updated_policy(policy, task, addresses):
    ranges = [ipaddress.ip_network(value) for value in policy.get('management_cidrs', [])]
    for address in addresses:
        ip = ipaddress.ip_address(address)
        if ip.version != 4 or not ip.is_global or any(ip in network for network in ranges):
            raise ValueError('A resolved address is private or denied by management policy; approval refused.')
    policy = json.loads(json.dumps(policy))
    policy.setdefault('sessions', {})[f'task-{task}'] = [f'{ip}/32' for ip in addresses]
    return policy


def main():
    parser = argparse.ArgumentParser(description='Approve a website for one ARTEX assessment, with confirmation.')
    parser.add_argument('--task', required=True, type=int)
    parser.add_argument('--url', required=True)
    parser.add_argument('--context', help='Docker context used by this installation')
    args = parser.parse_args()
    if args.task <= 0:
        parser.error('Assessment ID must be a positive integer.')
    root = Path(__file__).resolve().parent.parent
    os.chdir(root)
    docker = ['docker'] + (['--context', args.context] if args.context else [])
    # Select a running deployment by this checkout's exact policy mount, not an
    # assumed Compose name. Never act on another installation or shared daemon.
    ids = subprocess.check_output(docker + ['ps', '-q', '--filter', 'label=com.docker.compose.service=worker-broker'], text=True).split()
    matches = []
    policy_path = root / '.secrets/worker-policy.json'
    for container_id in ids:
        data = json.loads(subprocess.check_output(docker + ['inspect', container_id], text=True))[0]
        if any(m.get('Destination') == '/run/worker-policy.json' and m.get('Source') == str(policy_path) for m in data.get('Mounts', [])):
            matches.append(container_id)
    if len(matches) != 1:
        raise ValueError('Could not find exactly one running scanning service for this folder. Start the worker stack and select its Docker context first.')
    addresses = public_addresses(target_host(args.url))
    policy = json.loads(policy_path.read_text())
    revised = updated_policy(policy, args.task, addresses)
    print(f'Assessment {args.task}: {args.url}')
    print('Resolved IPv4 addresses: ' + ', '.join(addresses))
    print('This permits worker network access to those IPs, not only the website hostname.')
    print('Proceed only if these are your authorized server addresses. Shared/CDN IPs need separate review.')
    print('Existing management deny rules and other assessments will be preserved.')
    if input('Type APPROVE to save and restart the scanning service: ').strip() != 'APPROVE':
        print('Cancelled. Nothing changed.')
        return
    # Avoid replacing a file bind-mounted by Docker; write through the existing inode.
    # Refuse overwriting any policy edit made while the operator was reviewing.
    if json.loads(policy_path.read_text()) != policy:
        raise ValueError('Policy changed during review. Run the command again.')
    backup = policy_path.with_name('worker-policy.before-approval.json')
    fd = os.open(backup, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, 'w') as output:
        json.dump(policy, output, indent=2)
    with policy_path.open('w') as output:
        json.dump(revised, output, indent=2)
        output.write('\n')
        output.flush()
        os.fsync(output.fileno())
    try:
        subprocess.run(docker + ['restart', matches[0]], check=True)
    except subprocess.CalledProcessError:
        raise ValueError('Policy saved, but service restart failed. Restart the broker and use Check setup in ARTEX before starting.') from None
    print('Target approval saved. In ARTEX, choose Check setup. No assessment was started.')


if __name__ == '__main__':
    try:
        main()
    except (ValueError, OSError, subprocess.SubprocessError) as exc:
        print(f'Could not approve target: {exc}', file=sys.stderr)
        sys.exit(1)

#!/bin/sh
# Only bootstrap has network administration capability. The supervisor retains
# UID 0 with no capabilities; its daemon and all tools run as UID 10001.
set -eu
: "${WORKER_ALLOW:?operator target policy is required}"
: "${WORKER_DENY:?management deny policy is required}"
: "${WORKER_DNS:?approved DNS resolvers are required}"
iptables -F OUTPUT
iptables -P OUTPUT DROP
ip6tables -F OUTPUT
ip6tables -P OUTPUT DROP
ip6tables -A OUTPUT -o lo -j ACCEPT
iptables -A OUTPUT -o lo -j ACCEPT
old_ifs=$IFS
IFS=,
for cidr in $WORKER_DENY; do iptables -A OUTPUT -d "$cidr" -j REJECT; done
# Docker is configured with only these explicit resolvers by the broker.
for resolver in $WORKER_DNS; do
 iptables -A OUTPUT -d "$resolver" -p udp --dport 53 -j ACCEPT
 iptables -A OUTPUT -d "$resolver" -p tcp --dport 53 -j ACCEPT
done
for cidr in $WORKER_ALLOW; do iptables -A OUTPUT -d "$cidr" -j ACCEPT; done
IFS=$old_ifs
unset WORKER_ALLOW WORKER_DENY WORKER_DNS
exec /usr/local/bin/tool-worker supervise

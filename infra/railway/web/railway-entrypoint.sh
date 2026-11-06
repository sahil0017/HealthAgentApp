#!/bin/sh
set -eu

dns_server="$(awk '$1 == "nameserver" { print $2; exit }' /etc/resolv.conf)"
if [ -z "$dns_server" ]; then
  echo "No DNS resolver configured in /etc/resolv.conf" >&2
  exit 1
fi

case "$dns_server" in
  *:*) dns_server="[$dns_server]" ;;
esac
export NGINX_RESOLVER="$dns_server"

exec /docker-entrypoint.sh "$@"

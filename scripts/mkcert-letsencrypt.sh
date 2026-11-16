#!/bin/sh
# Obtain a Let's Encrypt certificate for gr_report with certbot and install
# a deploy hook that copies renewed certificates to the gr_report TLS
# directory and reloads the service (SIGHUP, no downtime).
#
# Usage: sudo scripts/mkcert-letsencrypt.sh -n NAME -m EMAIL [-o DIR] [-g GROUP] [-s] [-w WEBROOT]
#   -n NAME     public DNS name of the server (required)
#   -m EMAIL    contact address for Let's Encrypt (required)
#   -o DIR      where gr_report reads its certificate (default: /etc/gr_report/tls)
#   -g GROUP    group of the service, gets read access to the key (default: gr_report)
#   -s          use the Let's Encrypt staging environment (for testing)
#   -w WEBROOT  use the webroot method instead of certbot's standalone server
#
# Requirements: certbot, a DNS name that resolves to this host, and port 80
# reachable from the internet (HTTP-01 challenge). By running this script you
# agree to the Let's Encrypt subscriber agreement.
set -eu

name='' email='' out=/etc/gr_report/tls group=gr_report staging='' webroot=''
while getopts "n:m:o:g:sw:h" opt; do
	case "$opt" in
	n) name=$OPTARG ;;
	m) email=$OPTARG ;;
	o) out=$OPTARG ;;
	g) group=$OPTARG ;;
	s) staging=--staging ;;
	w) webroot=$OPTARG ;;
	*) sed -n '2,16p' "$0"; exit 2 ;;
	esac
done

if [ -z "$name" ] || [ -z "$email" ]; then sed -n '2,16p' "$0"; exit 2; fi
case "$name" in *[!A-Za-z0-9.-]*) echo "invalid DNS name: $name" >&2; exit 2 ;; esac
case "$out$group" in *[!A-Za-z0-9/._-]*) echo "invalid character in -o or -g" >&2; exit 2 ;; esac
command -v certbot >/dev/null || { echo "certbot is required (apt install certbot / dnf install certbot)" >&2; exit 1; }
[ "$(id -u)" -eq 0 ] || { echo "run as root" >&2; exit 1; }

hook_dir=/etc/letsencrypt/renewal-hooks/deploy
hook=$hook_dir/gr_report-$name.sh
mkdir -p "$hook_dir"
cat >"$hook" <<HOOK
#!/bin/sh
# Installed by gr_report scripts/mkcert-letsencrypt.sh. Runs after every
# successful issuance or renewal.
set -eu
live=/etc/letsencrypt/live/$name
[ -z "\${RENEWED_LINEAGE:-}" ] || [ "\$RENEWED_LINEAGE" = "\$live" ] || exit 0
umask 077
mkdir -p "$out"
install -m 0644 "\$live/fullchain.pem" "$out/cert.pem.new"
install -m 0600 "\$live/privkey.pem" "$out/key.pem.new"
if getent group "$group" >/dev/null 2>&1; then
	chgrp "$group" "$out" "$out/key.pem.new"
	chmod 0640 "$out/key.pem.new"
	chmod 0750 "$out"
fi
mv "$out/key.pem.new" "$out/key.pem"
mv "$out/cert.pem.new" "$out/cert.pem"
if command -v restorecon >/dev/null 2>&1; then restorecon -R "$out" || true; fi
if command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet gr_report; then
	systemctl reload gr_report
fi
HOOK
chmod 0755 "$hook"

if [ -n "$webroot" ]; then
	method="--webroot -w $webroot"
else
	method=--standalone
fi
# shellcheck disable=SC2086
certbot certonly $method $staging --non-interactive --agree-tos \
	-m "$email" -d "$name" --key-type ecdsa --deploy-hook "$hook"

# certbot only runs --deploy-hook when a certificate was issued; make sure
# the files are in place also when an existing one was kept.
[ -e "$out/cert.pem" ] || "$hook"

echo "certificate installed in $out; renewal is handled by certbot's timer"
echo "start with: gr_report -tls-cert $out/cert.pem -tls-key $out/key.pem"

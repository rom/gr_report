#!/bin/sh
# Create a self-signed TLS certificate for gr_report (ECDSA P-256).
#
# Usage: scripts/mkcert-selfsigned.sh [-o DIR] [-n NAME] [-a ALTNAMES] [-d DAYS] [-g GROUP] [-f]
#   -o DIR       output directory            (default: ./tls)
#   -n NAME      primary host name           (default: localhost)
#   -a ALTNAMES  extra names/IPs, comma separated, e.g. "report.lan,192.0.2.10"
#   -d DAYS      validity in days            (default: 365)
#   -g GROUP     make the private key readable by GROUP (mode 0640), e.g. gr_report
#   -f           overwrite existing files
#
# Browsers will warn about a self-signed certificate. Distribute cert.pem to
# the participants out of band or use scripts/mkcert-letsencrypt.sh.
set -eu

out=./tls name=localhost alt='' days=365 group='' force=0
while getopts "o:n:a:d:g:fh" opt; do
	case "$opt" in
	o) out=$OPTARG ;;
	n) name=$OPTARG ;;
	a) alt=$OPTARG ;;
	d) days=$OPTARG ;;
	g) group=$OPTARG ;;
	f) force=1 ;;
	*) sed -n '2,13p' "$0"; exit 2 ;;
	esac
done

command -v openssl >/dev/null || { echo "openssl is required" >&2; exit 1; }
case "$days" in *[!0-9]* | "") echo "invalid -d value: $days" >&2; exit 2 ;; esac
case "$name$alt" in *[!A-Za-z0-9.,:_-]*) echo "invalid character in host name" >&2; exit 2 ;; esac

if [ "$force" -eq 0 ] && { [ -e "$out/cert.pem" ] || [ -e "$out/key.pem" ]; }; then
	echo "$out already holds a certificate; use -f to overwrite" >&2
	exit 1
fi

san="DNS:$name"
[ "$name" = localhost ] && san="$san,IP:127.0.0.1,IP:::1"
old_ifs=$IFS; IFS=,
for a in $alt; do
	[ -n "$a" ] || continue
	case "$a" in
	*:* ) san="$san,IP:$a" ;;
	*[!0-9.]* ) san="$san,DNS:$a" ;;
	* ) san="$san,IP:$a" ;;
	esac
done
IFS=$old_ifs

umask 077
mkdir -p "$out"
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes \
	-sha256 -days "$days" -subj "/CN=$name" \
	-addext "subjectAltName=$san" \
	-addext "keyUsage=critical,digitalSignature" \
	-addext "extendedKeyUsage=serverAuth" \
	-addext "basicConstraints=critical,CA:FALSE" \
	-keyout "$out/key.pem.new" -out "$out/cert.pem.new" 2>/dev/null
mv "$out/key.pem.new" "$out/key.pem"
mv "$out/cert.pem.new" "$out/cert.pem"
chmod 600 "$out/key.pem"
chmod 644 "$out/cert.pem"
if [ -n "$group" ]; then
	chgrp "$group" "$out" "$out/key.pem"
	chmod 750 "$out"
	chmod 640 "$out/key.pem"
fi

echo "certificate: $out/cert.pem"
echo "private key: $out/key.pem"
echo "names:       $san"
printf 'fingerprint: '
openssl x509 -in "$out/cert.pem" -noout -fingerprint -sha256 | cut -d= -f2
echo
echo "start with:  gr_report -tls-cert $out/cert.pem -tls-key $out/key.pem"

#!/usr/bin/env bash
# Probes whether a web UI login can reach the /cws/api REST endpoints, which
# would save us from having to mint a Web API token in the Crestron Home app.
set -euo pipefail

HOST="${CRESTRON_HOST:-192.168.3.2}"
: "${CRESTRON_USER:?set CRESTRON_USER}"
: "${CRESTRON_PASS:?set CRESTRON_PASS}"

JAR="$(mktemp)"
trap 'rm -f "$JAR"' EXIT

echo "=== 1. seed session + XSRF ==="
XSRF="$(curl -sk -c "$JAR" -D - -o /dev/null "https://$HOST/userlogin.html" \
  | grep -i '^CREST-XSRF-TOKEN:' | tr -d '\r' | cut -d' ' -f2- || true)"
if [ -n "$XSRF" ]; then echo "xsrf token: present"; else echo "xsrf token: absent"; fi

echo "=== 2. login ==="
CODE="$(curl -sk -b "$JAR" -c "$JAR" -o /dev/null -w '%{http_code}' \
  ${XSRF:+-H "CREST-XSRF-TOKEN: $XSRF"} \
  --data-urlencode "login=$CRESTRON_USER" \
  --data-urlencode "passwd=$CRESTRON_PASS" \
  "https://$HOST/userlogin.html")"
echo "login http: $CODE"
if grep -qi authtoken "$JAR"; then echo "AUTHTOKEN cookie: acquired"; else echo "AUTHTOKEN cookie: none"; fi

echo "=== 3. does the web session reach /cws/api? ==="
for ep in rooms lights devices; do
  printf '  /cws/api/%s -> ' "$ep"
  curl -sk -b "$JAR" ${XSRF:+-H "CREST-XSRF-TOKEN: $XSRF"} \
    "https://$HOST/cws/api/$ep" | tr -d '\n' | head -c 200; echo
done

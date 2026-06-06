#!/bin/sh
set -eu

API_BASE="${AUTHMAN_API_BASE:-/api}"
APP_KIND="player"
DEFAULT_LOCALE="${AUTHMAN_DEFAULT_LOCALE:-en}"

cat >/usr/share/nginx/html/config.js <<EOF
window.__AUTHMAN_RUNTIME_CONFIG__ = {
  apiBase: "${API_BASE}",
  appKind: "${APP_KIND}",
  defaultLocale: "${DEFAULT_LOCALE}"
};
EOF

exec "$@"

#!/bin/sh
set -eu

# Inject runtime configuration into /config.js from environment variables.
# This file is consumed by the SPA at runtime so production hostnames are
# never baked into the static bundle.

API_BASE="${AUTHMAN_API_BASE:-/api}"
APP_KIND="admin"
DEFAULT_LOCALE="${AUTHMAN_DEFAULT_LOCALE:-en}"

cat >/usr/share/nginx/html/config.js <<EOF
window.__AUTHMAN_RUNTIME_CONFIG__ = {
  apiBase: "${API_BASE}",
  appKind: "${APP_KIND}",
  defaultLocale: "${DEFAULT_LOCALE}"
};
EOF

exec "$@"

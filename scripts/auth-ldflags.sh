#!/usr/bin/env bash
set -euo pipefail

required=${1:-optional}
if [[ "$required" != optional && "$required" != required ]]; then
  printf 'Usage: auth-ldflags.sh [optional|required]\n' >&2
  exit 1
fi
if [[ -z "${BIGBOARD_GITHUB_CLIENT_ID:-}${BIGBOARD_GITHUB_CLIENT_SECRET:-}${BIGBOARD_GITHUB_APP_SLUG:-}" && "$required" == optional ]]; then
  exit 0
fi
for name in BIGBOARD_GITHUB_CLIENT_ID BIGBOARD_GITHUB_CLIENT_SECRET; do
  value=${!name:-}
  if [[ ! "$value" =~ ^[A-Za-z0-9._-]+$ || ${#value} -gt 4096 ]]; then
    printf 'Missing or invalid publisher GitHub App configuration: %s\n' "$name" >&2
    exit 1
  fi
done
if [[ ! "${BIGBOARD_GITHUB_APP_SLUG:-}" =~ ^[a-z0-9-]+$ || ${#BIGBOARD_GITHUB_APP_SLUG} -gt 100 ]]; then
  printf 'Missing or invalid publisher GitHub App configuration: BIGBOARD_GITHUB_APP_SLUG\n' >&2
  exit 1
fi
printf -- '-X main.githubClientID=%s -X main.githubClientSecret=%s -X main.githubAppSlug=%s' "$BIGBOARD_GITHUB_CLIENT_ID" "$BIGBOARD_GITHUB_CLIENT_SECRET" "$BIGBOARD_GITHUB_APP_SLUG"

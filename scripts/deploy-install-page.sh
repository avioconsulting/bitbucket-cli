#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
case "${1:---dry-run}" in
    --dry-run) printf '%s\n' 'Project: public-sites-0570' 'scripts/install/index.html -> gs://avio-tools/bkt' 'scripts/install/bkt.sh -> gs://avio-tools/bkt.sh'; exit 0 ;;
    --apply) ;;
    *) echo 'Usage: bash scripts/deploy-install-page.sh [--dry-run|--apply]' >&2; exit 1 ;;
esac
sh -n scripts/install/bkt.sh
gcloud storage cp scripts/install/bkt.sh gs://avio-tools/bkt.sh \
    --project=public-sites-0570 --content-type='text/plain; charset=utf-8' --cache-control='public,max-age=60'
gcloud storage cp scripts/install/index.html gs://avio-tools/bkt \
    --project=public-sites-0570 --content-type='text/html; charset=utf-8' --cache-control='public,max-age=60'

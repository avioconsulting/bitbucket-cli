#!/usr/bin/env bash
# Called only after verified stable publication by the current-master Jenkins job.
set -euo pipefail
cd "$(dirname "$0")/.."
version=$(cat AVIO_VERSION)
[[ "$version" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-avio$ ]] || { echo 'Expected a committed AVIO release version' >&2; exit 1; }
major=${BASH_REMATCH[1]}; minor=${BASH_REMATCH[2]}
next="$major.$((minor + 1)).0-avio-SNAPSHOT"
source=${SOURCE_COMMIT:?Missing built commit}
[[ "$(git rev-parse HEAD)" == "$source" && -z "$(git status --porcelain --untracked-files=no)" ]]
git fetch origin master --tags
[[ "$(git rev-parse origin/master)" == "$source" ]] || { echo 'Master advanced; release finalization requires review' >&2; exit 1; }
tag="v$version"
if git rev-parse --verify "refs/tags/$tag" >/dev/null 2>&1; then
    [[ "$(git rev-parse "$tag^{commit}")" == "$source" ]] || { echo 'Release tag conflict' >&2; exit 1; }
else
    git -c user.name='AVIO Jenkins' -c user.email='jenkins@avioconsulting.com' tag -a "$tag" "$source" -m "AVIO bkt $version"
fi
printf '%s\n' "$next" > AVIO_VERSION
git add AVIO_VERSION
git -c user.name='AVIO Jenkins' -c user.email='jenkins@avioconsulting.com' commit -m "chore: begin $next development"
# One atomic, non-forced push: a concurrent master update cannot leave a partial tag/bump.
git push --atomic origin "refs/tags/$tag" HEAD:refs/heads/master

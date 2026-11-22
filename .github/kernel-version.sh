#!/usr/bin/env bash
# Resolve the version once, before starting the build matrix.
set -euo pipefail
stable='^openweir-v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
version=$(cat VERSION)
[[ $version =~ $stable ]] || { echo 'VERSION must contain openweir-vMAJOR.MINOR.PATCH' >&2; exit 1; }
# Builds that embed mihomo as a Go module (the desktop app) get no ldflags,
# so the source default must name the same release.
grep -qF "Version    = \"$version\"" constant/version.go || { echo 'constant.Version differs from VERSION' >&2; exit 1; }
release=false
case "$GITHUB_EVENT_NAME:$GITHUB_REF_TYPE" in
  push:tag)
    [[ $GITHUB_REF_NAME == "$version" ]] || { echo 'Tag differs from VERSION' >&2; exit 1; }
    git merge-base --is-ancestor HEAD origin/Alpha || { echo "Tag is outside Alpha history" >&2; exit 1; }
    release=true ;;
  workflow_dispatch:*)
    [[ $GITHUB_REF_NAME == Alpha || $GITHUB_REF_NAME == "$version" ]] || exit 1
    git merge-base --is-ancestor HEAD origin/Alpha || { echo "Requested commit is outside Alpha history" >&2; exit 1; }
    [[ -z ${REQUESTED_VERSION:-} || $REQUESTED_VERSION == "$version" ]] || { echo 'Requested version differs from VERSION' >&2; exit 1; }
    release=true ;;
  push:branch)
    # Release whenever Alpha carries a VERSION that has not been published yet, so the
    # first push after a bump releases it and a failed release retries on the next push.
    if [[ $GITHUB_REF_NAME == Alpha ]]; then
      if git ls-remote --exit-code --tags origin "refs/tags/$version" > /dev/null; then
        :
      else
        rc=$?
        [[ $rc == 2 ]] || { echo 'Cannot list remote tags' >&2; exit "$rc"; }
        release=true
      fi
    fi ;;
esac
if [[ $release != true ]]; then
  branch=$(printf %s "$GITHUB_REF_NAME" | tr '[:upper:]' '[:lower:]')
  version="$branch-$(git rev-parse --short=8 HEAD)"
  version=${version//\//-}
fi
printf 'version=%s\nstable=%s\n' "$version" "$release" >> "$GITHUB_OUTPUT"

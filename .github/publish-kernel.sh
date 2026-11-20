#!/usr/bin/env bash
set -euo pipefail
version=${VERSION:?}
sha=$(git rev-parse "${GITHUB_SHA:?}^{commit}")
[[ $sha == "$(git rev-parse HEAD)" ]] || { echo 'Checkout differs from build commit' >&2; exit 1; }
repo=${GITHUB_REPOSITORY:?}
cd "${1:?artifact directory required}"

for arch in 386-softfloat amd64-v1 arm64 armv5 armv6 armv7 mips-softfloat mipsle-softfloat riscv64 loong64-abi2; do
  archive="mihomo-linux-$arch-$version.gz"
  [[ -s $archive ]] || { echo "Missing router kernel: $archive" >&2; exit 1; }
  gzip -t "$archive"
done
[[ $(cat version.txt) == "$version" ]]
# Omit the duplicated per-job marker; publish checksums for all downloadable files.
rm version.txt
export LC_ALL=C
for asset in *; do
  [[ $asset == sha256sums ]] || sha256sum "$asset"
done > sha256sums
sha256sum --check sha256sums > /dev/null

tag_ref="refs/tags/$version"
if git ls-remote --exit-code origin "$tag_ref" > /dev/null; then
  git fetch --no-tags origin "$tag_ref"
  [[ $(git rev-parse 'FETCH_HEAD^{commit}') == "$sha" ]] || { echo 'Tag already identifies another commit' >&2; exit 1; }
else
  rc=$?
  [[ $rc == 2 ]] || exit "$rc"
  git push origin "$sha:$tag_ref"
fi

if release=$(gh release view "$version" --repo "$repo" --json isDraft,isPrerelease,targetCommitish); then
  if [[ $(jq -r .isDraft <<< "$release") == false ]]; then
    jq -e '.isPrerelease == false' <<< "$release" > /dev/null
    echo "Stable release $version already published"; exit 0
  fi
  [[ $(jq -r .targetCommitish <<< "$release") == "$sha" ]] || { echo 'Existing draft targets another commit' >&2; exit 1; }
else
  gh release create "$version" --repo "$repo" --verify-tag --target "$sha" --draft \
    --title "Mihomo $version" --generate-notes
fi
# Retry an interrupted upload only on our unpublished draft.
gh release upload "$version" --repo "$repo" --clobber ./*
gh release view "$version" --repo "$repo" --json assets \
  | jq -r '.assets[] | [.digest, .name] | @tsv' | LC_ALL=C sort > "$RUNNER_TEMP/kernel-remote-assets"
for asset in *; do
  printf 'sha256:%s\t%s\n' "$(sha256sum "$asset" | cut -d ' ' -f1)" "$asset"
done | LC_ALL=C sort > "$RUNNER_TEMP/kernel-local-assets"
diff -u "$RUNNER_TEMP/kernel-local-assets" "$RUNNER_TEMP/kernel-remote-assets"
# GitHub chooses Latest using semantic version and release date, including backfills.
gh api --method PATCH "repos/$repo/releases/$(gh release view "$version" --repo "$repo" --json databaseId --jq .databaseId)" \
  -F draft=false -F prerelease=false -f make_latest=legacy --silent

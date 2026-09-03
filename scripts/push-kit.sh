#!/usr/bin/env bash
# Validate and push this mixin kit to an OCI registry.
#
#   ./scripts/push-kit.sh                          # pushes :latest
#   TAG=1.0.0 ./scripts/push-kit.sh                # pushes :1.0.0
#   DOCKERHUB_NAMESPACE=me ./scripts/push-kit.sh   # pushes to another namespace
#
# A tag is for humans picking a release; consumers must reference the kit by
# digest (`oci://<image>@sha256:...`), which the script prints when it can
# resolve it. .github/workflows/publish.yaml runs this on every push to main
# (installing sbx from docker/sbx-releases); a maintainer can also run it locally.
set -euo pipefail

namespace="${DOCKERHUB_NAMESPACE:-${DOCKER_NAMESPACE:-firecrawl}}"
kit_name="${KIT_NAME:-firecrawl-docker-sandbox}"   # also the staged subdir name
tag="${TAG:-latest}"
repo_root="$(cd "$(dirname "$0")/.." && pwd)"
image="docker.io/$namespace/$kit_name"

stage=""
cleanup() {
  if [ -n "$stage" ]; then rm -rf "$stage"; fi
}
trap cleanup EXIT INT TERM

command -v sbx >/dev/null 2>&1 || {
  echo "push-kit: sbx not found. It ships with Docker Desktop; make sure its CLI is on PATH." >&2
  exit 1
}
sbx kit --help >/dev/null 2>&1 || {
  echo "push-kit: this sbx has no 'kit' command, so it predates kit spec v2. Update Docker Desktop." >&2
  exit 1
}

# Keep the published tag honest about which version of the spec it carries.
spec_version="$(sed -n 's/^version:[[:space:]]*"\{0,1\}\([0-9][^"]*\)"\{0,1\}[[:space:]]*$/\1/p' "$repo_root/spec.yaml")"
if [ "$tag" != "latest" ] && [ "$tag" != "$spec_version" ] && [ "$tag" != "v$spec_version" ]; then
  echo "push-kit: TAG=$tag does not match spec.yaml version $spec_version." >&2
  exit 1
fi

stage="$(mktemp -d /tmp/sbx-kit-push.XXXXXX)"
mkdir -p "$stage/$kit_name"
cp "$repo_root/spec.yaml" "$stage/$kit_name/spec.yaml"
for extra in README.md LICENSE NOTICE; do
  if [ -f "$repo_root/$extra" ]; then cp "$repo_root/$extra" "$stage/$kit_name/$extra"; fi
done
if [ -d "$repo_root/files" ]; then cp -R "$repo_root/files" "$stage/$kit_name/files"; fi

sbx kit validate "$stage/$kit_name"
sbx kit push "$stage/$kit_name" "$image:$tag"
echo "Pushed $image:$tag (spec version $spec_version)"

# Consumers must reference the kit by digest: `--kit oci://...:latest` is
# rejected. Resolving it is left as a printed command rather than run here,
# because it blocks on the registry and a stall after a successful push reads
# like a failed push.
echo
echo "Give consumers the digest, not the tag:"
echo "  digest=\$(docker buildx imagetools inspect $image:$tag --format '{{.Manifest.Digest}}')"
echo "  sbx run --kit \"oci://$image@\$digest\" claude"

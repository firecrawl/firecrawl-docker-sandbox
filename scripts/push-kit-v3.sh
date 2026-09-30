#!/usr/bin/env bash
# Build and push the Kit v3 mixin (firecrawl/firecrawl.yaml) to an OCI registry.
#
#   ./scripts/push-kit-v3.sh                          # pushes :<version> and :latest
#   DOCKERHUB_NAMESPACE=me ./scripts/push-kit-v3.sh   # pushes to another namespace
#   PUSH=0 ./scripts/push-kit-v3.sh                   # build only, into an OCI layout
#
# A v3 kit is an ordinary image: BuildKit reads the descriptor's
# `# syntax=docker/sandbox-kit:3` line, pulls the kit frontend, validates the
# descriptor and attaches it to the manifest. Both platforms go in one
# invocation so the tag serves one image index. .github/workflows/publish.yaml
# runs this on every push to main; a maintainer can also run it locally with
# Docker Desktop (buildx) and `docker login`.
set -euo pipefail

namespace="${DOCKERHUB_NAMESPACE:-${DOCKER_NAMESPACE:-firecrawl}}"
kit_name="${KIT_V3_NAME:-sbx-kit-firecrawl}"
repo_root="$(cd "$(dirname "$0")/.." && pwd)"
kit_dir="$repo_root/firecrawl"
image="docker.io/$namespace/$kit_name"
push="${PUSH:-1}"

command -v docker >/dev/null 2>&1 || {
  echo "push-kit-v3: docker not found. The v3 kit builds with docker buildx." >&2
  exit 1
}
docker buildx version >/dev/null 2>&1 || {
  echo "push-kit-v3: docker buildx not available." >&2
  exit 1
}

# The tag is the descriptor's own version, so the tag says what the image is.
version="$(sed -n 's/^version:[[:space:]]*"\{0,1\}\([0-9][^"]*\)"\{0,1\}[[:space:]]*$/\1/p' "$kit_dir/firecrawl.yaml")"
[ -n "$version" ] || { echo "push-kit-v3: firecrawl/firecrawl.yaml has no version: field" >&2; exit 1; }

if [ "$push" = "1" ]; then
  docker buildx build "$kit_dir" -f "$kit_dir/firecrawl.yaml" \
    --platform linux/amd64,linux/arm64 --push \
    -t "$image:$version" -t "$image:latest" \
    --metadata-file "${METADATA_FILE:-/tmp/sbx-kit-firecrawl-push.json}"
  echo "Pushed $image:$version and $image:latest"
  echo
  echo "Compose it onto a v3 workload:"
  echo "  sbx run docker/sbx-kit-shell:1.0.0 --kit $image:$version ."
else
  layout="${LAYOUT_DIR:-/tmp/sbx-kit-firecrawl-layout}"
  rm -rf "$layout"
  docker buildx build "$kit_dir" -f "$kit_dir/firecrawl.yaml" \
    -t "$kit_name:$version" \
    --output "type=oci,dest=$layout,tar=false"
  echo "Built $kit_name:$version into $layout"
  echo "  kit-tck validate --layout $layout $version"
fi

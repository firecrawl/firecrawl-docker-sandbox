# Contributing

## Validating a change

```console
cd tools/kitcheck && go run . ../..
```

This is what CI runs. It loads the v2 kit through the authoritative validator from
[docker/sbx-kits-contrib](https://github.com/docker/sbx-kits-contrib) (the same code path as
`sbx kit validate`) and adds the checks that validator leaves to the engine or to this repo: inject domains
must be in the egress allow list, an api-key credential must be `proxyManaged`, the SDK pin must agree between
`spec.yaml`, `v3/firecrawl.yaml` and `README.md`, README git references must be SHA-pinned, and the kit
version must match the newest `CHANGELOG.md` entry. It also checks that the v3 descriptor declares the same
hosts, credential and agent instructions as `spec.yaml`, since the two are published side by side.

The v3 descriptor itself is validated by the kit frontend while it builds, and the built image is judged by
Docker's conformance suite. With Docker Desktop (buildx) and
[`kit-tck`](https://github.com/docker/sandbox-kit-spec/releases):

```console
PUSH=0 ./scripts/push-kit-v3.sh
kit-tck validate --layout /tmp/sbx-kit-firecrawl-layout 1.0.0
```

With a v2-capable `sbx` (v0.45 or later), also run the real validator and a smoke test of each form:

```console
sbx kit validate .
sbx run --kit . shell                                 # v2 mixin on a built-in agent
sbx run docker/sbx-kit-shell:1.0.0 --kit ./v3 .       # v3 mixin on Docker's v3 shell workload
```

## Bumping the SDK

1. Change `SDK_VERSION` in `spec.yaml` and the `sdkVersion` default in `v3/firecrawl.yaml`.
2. Change the `firecrawl-py <version>` mentions in `README.md`.
3. Bump `version:` in `spec.yaml` and `v3/firecrawl.yaml`, and add a `CHANGELOG.md` entry.
4. Run `kitcheck`, then smoke-test a scrape in a real sandbox: an SDK bump can change the Python API the
   `agentInstructions` snippets use, and nothing but a live run catches that.

## Adding a network domain

Every host the kit reaches at install or runtime has to be in `permissions.network.allow` (`spec.yaml`) and in
the matching phase of `network-policy@1` (`v3/firecrawl.yaml`); sandboxes run default-deny, so an unlisted
host fails as a proxy 403. Keep the list to hosts the kit itself needs, and keep inject domains spelled the
same way as their allow entry. Anything the agent's instructions say about the network has to change in
both `spec.yaml` and `v3/firecrawl-context.md`; kitcheck fails if the two bodies differ.

## Releasing

Publishing runs from `.github/workflows/publish.yaml` on every push to `main`. It needs the
`DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN` repo secrets and publishes both forms:

- `publish-kit-v2` installs `sbx` from `docker/sbx-releases` and runs `scripts/push-kit.sh`, which pushes
  `docker.io/firecrawl/firecrawl-docker-sandbox`.
- `publish-kit-v3` runs `scripts/push-kit-v3.sh`, which builds `v3/firecrawl.yaml` with `docker buildx` for
  `linux/amd64` and `linux/arm64` in one invocation and pushes `docker.io/firecrawl/sbx-kit-firecrawl` tagged
  with the descriptor's version and `latest`.

To publish by hand instead:

```console
TAG=1.0.0 ./scripts/push-kit.sh     # v2: refuses a TAG that disagrees with spec.yaml's version
./scripts/push-kit-v3.sh            # v3: the tag is read from v3/firecrawl.yaml
```

The v2 script stages the kit into a temporary directory, validates it, pushes it, and prints the commands
that turn the pushed tag into the digest reference v2 consumers need (`--kit` references there must be a
digest or a 40-hex commit SHA). v3 kits are ordinary images and are referenced by tag or digest like any
other image.

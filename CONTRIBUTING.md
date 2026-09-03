# Contributing

## Validating a change

```console
cd tools/kitcheck && go run . ../..
```

This is what CI runs. It loads the kit through the authoritative validator from
[docker/sbx-kits-contrib](https://github.com/docker/sbx-kits-contrib) (the same code path as
`sbx kit validate`) and adds the checks that validator leaves to the engine or to this repo: inject domains
must be in the egress allow list, an api-key credential must be `proxyManaged`, the SDK pin must agree between
`spec.yaml` and `README.md`, README git references must be SHA-pinned, and `spec.yaml`'s version must match the
newest `CHANGELOG.md` entry.

With Docker Desktop and a v2-capable `sbx`, also run the real validator and a smoke test:

```console
sbx kit validate .
sbx run --kit . shell
```

## Bumping the SDK

1. Change `SDK_VERSION` in `spec.yaml`.
2. Change the two `firecrawl-py <version>` mentions in `README.md`.
3. Bump `version:` in `spec.yaml` and add a `CHANGELOG.md` entry.
4. Run `kitcheck`, then smoke-test a scrape in a real sandbox: an SDK bump can change the Python API the
   `agentInstructions` snippets use, and nothing but a live run catches that.

## Adding a network domain

Every host the kit reaches at install or runtime has to be in `permissions.network.allow`; sandboxes run
default-deny, so an unlisted host fails as a proxy 403. Keep the list to hosts the kit itself needs, and keep
inject domains spelled the same way as their allow entry.

## Releasing

Publishing runs from `.github/workflows/publish.yaml` on every push to `main`, which installs `sbx` from `docker/sbx-releases` and runs `scripts/push-kit.sh`. It needs the `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN` repo secrets.

To publish a version tag by hand instead:

```console
TAG=1.0.0 ./scripts/push-kit.sh
```

The script refuses a `TAG` that disagrees with `spec.yaml`'s version, stages the kit into a temporary
directory, validates it, pushes it, and prints the two commands that turn the pushed tag into the digest
reference consumers need. Tags are for humans; `--kit` references must be a digest or a 40-hex commit SHA.

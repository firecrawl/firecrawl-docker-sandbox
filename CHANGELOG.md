# Changelog

The version here is the kit's `version:` field in `spec.yaml` and in `firecrawl/firecrawl.yaml`; `tools/kitcheck`
fails if any of the three disagree.

## 1.0.0

First release under `firecrawl/firecrawl-docker-sandbox`, adapted from
[ajeetraina/sbx-kits-firecrawl](https://github.com/ajeetraina/sbx-kits-firecrawl).

- Ships in both kit formats. `spec.yaml` (kit spec v2) publishes as
  `docker.io/firecrawl/firecrawl-docker-sandbox` for the built-in `sbx` agents; `firecrawl/firecrawl.yaml`
  ([Kit v3](https://github.com/docker/sandbox-kit-spec)) publishes as `docker.io/firecrawl/sbx-kit-firecrawl`
  for v3 workloads such as `docker/sbx-kit-shell`. Same SDK pin, hosts, credential and agent instructions;
  `tools/kitcheck` asserts they agree, and CI judges the v3 image with Docker's `kit-tck` conformance suite.

- Includes Alexandria (beta) via `firecrawl-py` 4.44.0: `search(..., sources=["alexandria"])`,
  `find_tools()` and `scrape_alexandria()`. All three call `api.firecrawl.dev`, so no extra network
  allow entries. Needs an API key enabled for the beta; `agentInstructions` say so.
- Pins `firecrawl-py==4.44.0`, installed as user `1000` and asserted after install, so a broken install fails
  the sandbox create instead of surfacing as a missing module mid-task.
- Wires the API key with `credentials[].apiKey.proxyManaged` and `scheme: bearer` rather than hardcoding the
  `proxy-managed` sentinel in `environment.variables`. The credential is `required`, so an unbound key fails at
  create time rather than 401-ing later.
- Declares kit identity for distribution: `version`, `sourceURL`, `licenses`.
- Guards the install hook on `python3` and `pip`, and copes with PEP 668 externally-managed Python.
- Adds `tools/kitcheck`, which runs the authoritative `docker/sbx-kits-contrib` validator plus engine-level and
  repo-consistency checks, and runs it in CI.

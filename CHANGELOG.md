# Changelog

The version here is the kit's `version:` field in `spec.yaml`; `tools/kitcheck` fails if the two disagree.

## 1.0.0

First release under `firecrawl/firecrawl-docker-sandbox`, adapted from
[ajeetraina/sbx-kits-firecrawl](https://github.com/ajeetraina/sbx-kits-firecrawl).

- Pins `firecrawl-py==4.41.0`, installed as user `1000` and asserted after install, so a broken install fails
  the sandbox create instead of surfacing as a missing module mid-task.
- Wires the API key with `credentials[].apiKey.proxyManaged` and `scheme: bearer` rather than hardcoding the
  `proxy-managed` sentinel in `environment.variables`. The credential is `required`, so an unbound key fails at
  create time rather than 401-ing later.
- Declares kit identity for distribution: `version`, `sourceURL`, `licenses`.
- Guards the install hook on `python3` and `pip`, and copes with PEP 668 externally-managed Python.
- Adds `tools/kitcheck`, which runs the authoritative `docker/sbx-kits-contrib` validator plus engine-level and
  repo-consistency checks, and runs it in CI.

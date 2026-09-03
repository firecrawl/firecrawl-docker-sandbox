# sbx kits for Firecrawl

<img width="1147" height="661" alt="image" src="https://github.com/user-attachments/assets/eac58f39-1e14-4df1-9f7e-b1084562b3c3" />

A standalone [Docker Sandboxes](https://docs.docker.com/ai/sandboxes/) kit (`kind: mixin`) that adds
live web access to any sandbox agent via the [Firecrawl](https://www.firecrawl.dev/) Python SDK (`firecrawl-py`).

This kit gives whatever agent you run the ability to search the web, scrape a
page to clean markdown, and crawl a site, instead of relying on its training-cutoff knowledge.

## What the kit does

Four observable things, so each is independently verifiable (see §3):

1. Installs `firecrawl-py==4.30.1` as the agent user (`1000`).
2. Declares a `firecrawl` credential - the key is swapped into the `Authorization` header by the sbx proxy on
   requests to `api.firecrawl.dev`, never baked into the image or written to the sandbox.
3. Allows network egress to `api.firecrawl.dev` (plus PyPI for install) via `permissions.network.allow`.
4. Injects an `agentInstructions` note so the agent knows the capability exists and how to call it.

## Prerequisites

### 1. Store the Firecrawl API key

Get a key from [firecrawl.dev](https://www.firecrawl.dev/) (it looks like `fc-...`). Store it once with
sbx's secret manager - the key is never baked into the kit, and the sbx proxy injects it into the sandbox at
runtime (`sbx run` has no `-e` flag):

```console
echo "$FIRECRAWL_API_KEY" | sbx secret set -g firecrawl   # -g = all sandboxes
```

Running `sbx secret set -g firecrawl` with no piped value prompts you for the key interactively instead.
Confirm it's stored:

```console
sbx secret ls
```

On the **first** `sbx run` with this kit, sbx asks you to approve sending the `firecrawl` credential to
`api.firecrawl.dev` and records a binding in `~/.config/sbx/credentials.yaml`. Because the value already lives
in the secret store, you can accept the defaults — no env var or file source is needed. (In v2 the kit only
declares _what_ it needs and _where to inject it_; you control _where the key comes from_.)

> **Under centralized governance, one org rule is also required.** The kit declares egress to
> `api.firecrawl.dev` itself, but if `sbx policy ls` shows `Governance: Managed by <org>`, that managed policy
> is default-deny and **overrides the kit's allow** — scrapes fail with a proxy-side 403
> (`Blocked by network policy: domain api.firecrawl.dev:443 — no matching allow rule`). The governance owner
> must add a network allow rule for `api.firecrawl.dev` to the org policy (mirror the shape of any existing
> per-service rule, e.g. a single-host `allow`). PyPI is typically already allowed for installs; `api.firecrawl.dev`
> is the one host to add. Local `sbx policy allow` rules do **not** work here — they're ignored for org-managed
> domains. On an ungoverned host using a local `balanced`/`open` policy, no extra step is needed.

### 2. Launch the sandbox with the kit

Layer the mixin onto an agent. From the published image:

```console
sbx run --kit docker.io/firecrawl/firecrawl-docker-sandbox:latest claude
```

Or straight from this repo over git:

```console
sbx run --kit "git+https://github.com/firecrawl/firecrawl-docker-sandbox.git" claude
```

Or from a local clone (the kit lives at the repo root):

```console
git clone https://github.com/firecrawl/firecrawl-docker-sandbox.git
sbx run --kit ./firecrawl-docker-sandbox/ claude
```

#### Choosing the agent

The trailing argument (`claude` above) is the **coding agent** that runs inside the sandbox, a separate axis
from the kit. Any supported agent works — `sbx run --help` lists them:

```
claude, claude-bedrock, codex, copilot, cursor, docker-agent, droid, gemini, kiro, opencode, shell
```

So you can swap `claude` for, say, `codex`:

```console
sbx run --kit docker.io/firecrawl/firecrawl-docker-sandbox:latest codex
```

Arguments meant for the agent itself go after a `--` separator, e.g. `sbx run --kit ...:latest codex -- --help`.

## 3. Confirm the kit installed correctly

Once you're in the sandbox session, use `!` shell escapes to prove the mixin is really inside. Verify on
independent layers, from a cheap import check up to a full end-to-end scrape.

**i. The package is installed (the pinned version, in the user-site path):**

```console
!python3 -c "import firecrawl, importlib.metadata as m; print('firecrawl-py', m.version('firecrawl-py'), '->', firecrawl.__file__)"
```

Expect `firecrawl-py 4.30.1` (the exact pin from this kit's `spec.yaml`) installed under
`/home/agent/.local/lib/.../site-packages/` — the user-site location that matches the kit installing as user
`1000` rather than as root.

**ii. The credential is wired as a proxy-managed sentinel** — the real key never enters the sandbox.
`FIRECRAWL_API_KEY` is set to the literal `proxy-managed` inside the container (the kit sets this sentinel via
`environment.variables`, since the firecrawl-py SDK won't send a request without it), and the proxy swaps in
the real key on outbound requests to `api.firecrawl.dev`. Seeing the sentinel (not a real `fc-…` key) is the
fingerprint that the credential is proxy-managed:

```console
!env | grep -E 'FIRECRAWL_API_KEY'
```

Expect `FIRECRAWL_API_KEY=proxy-managed`. If you instead see a real `fc-…` value, the credential is coming
from somewhere other than this kit's proxy injection.

**iii. End-to-end functional proof** — scrape a page through the cloud API. This single command transitively
exercises the package, the env var, and network egress to `api.firecrawl.dev`, so if you only run one check,
run this one:

```console
!python3 - <<'PY'
from firecrawl import Firecrawl
fc = Firecrawl()                                   # reads FIRECRAWL_API_KEY
doc = fc.scrape("https://docs.docker.com/ai/sandboxes/", formats=["markdown"])
md = getattr(doc, "markdown", None) or (doc.get("markdown") if isinstance(doc, dict) else "")
print(md[:300])
PY
```

Expect the first few hundred characters of the page's clean markdown.

## Using Firecrawl from the agent

The `agentInstructions` note tells the agent it can do this; the one-liners:

```python
from firecrawl import Firecrawl
fc = Firecrawl()

fc.scrape("https://example.com", formats=["markdown"])   # one page -> clean markdown
fc.search("docker sandboxes mixin kit", limit=5)         # search the web, get page content
fc.crawl("https://docs.example.com", limit=20)           # crawl a whole site/section
```

See the [Firecrawl Python SDK docs](https://docs.firecrawl.dev/sdks/python) for the full API (formats,
structured JSON extraction with a schema, crawl options, etc.).

## Troubleshooting

If `sbx run --kit docker.io/...` fails with a mount-policy error like:

```
ERROR: failed to create sandbox: ... mount policy denied: /Users/<you>: no applicable policies for op(...)
```

the sbx runtime is refusing to mount your home directory. `sbx run` mounts the current working directory into
the sandbox, and mounting your entire home dir is blocked for safety. Run from any directory other than your
home directory.

If a scrape fails with a network error, confirm `api.firecrawl.dev` is in the kit's `permissions.network.allow`
(it is, by default) and that your org's governance policy hasn't overridden it.

If a scrape fails with `WebsiteNotSupportedError: ... Blocked by network policy: domain api.firecrawl.dev:443 —
no matching allow rule — blocked by default deny policy` (HTTP 403 from the sbx proxy, **not** from Firecrawl),
your sandbox is under **centralized governance** and the managed policy is default-deny without a rule for
`api.firecrawl.dev`. The kit's own `permissions.network.allow` — and any local `sbx policy allow` — **cannot
widen** egress under managed governance; only the org can. Confirm with:

```console
sbx policy ls <sandbox-name>        # look for "Managed by <org>" and whether api.firecrawl.dev is allowed
```

If you see `network policy for "api.firecrawl.dev" is managed by your organization; local allow rules are not
applied`, ask whoever owns the governance profile to add an `api.firecrawl.dev` allow rule (alongside the
existing PyPI allow). Alternatively, run the kit on a host using a local `balanced` or `open` policy instead of
managed governance. Everything else in the kit (SDK install, proxy-injected credential) works regardless — only
the outbound scrape is gated by this rule.

If a scrape raises `PaymentRequiredError: ... Insufficient credits` (HTTP 402), that's the **good** failure:
the request authenticated successfully (a bad/missing key returns 401, not 402) — your Firecrawl account is
just out of credits. The kit is working; top up at <https://firecrawl.dev/pricing> or lower the request
`limit`.

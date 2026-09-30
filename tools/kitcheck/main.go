// Command kitcheck validates this repo's kit without needing an sbx install.
//
// It runs the authoritative loader from docker/sbx-kits-contrib (the same
// spec.LoadFromDirectory + ValidateArtifact path `sbx kit validate` uses), then
// adds the checks that library leaves to the engine or to the repo:
//
//   - every credential inject domain is in permissions.network.allow
//   - no kit-set env var uses a runtime-reserved prefix (SPEC-v2 §5.5)
//   - an api-key credential the agent must read declares proxyManaged
//   - the pinned firecrawl-py version is identical in spec.yaml and README.md
//   - spec.yaml `version:` matches the newest CHANGELOG.md heading
//   - the Kit v3 descriptor (firecrawl/firecrawl.yaml) says the same thing as
//     spec.yaml: SDK pin, version, hosts, credential, agent instructions
//
// The v3 descriptor's own grammar is validated by the kit frontend when it is
// built (scripts/push-kit-v3.sh) and the image by kit-tck; this tool only
// keeps the two descriptors from drifting apart.
//
// Usage: go run . [path-to-kit-dir]   (default: ../..)
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/docker/sbx-kits-contrib/spec"
	"go.yaml.in/yaml/v3"
)

var reservedEnvPrefixes = []string{"DASH_", "SBX_", "DOCKER_"}

type report struct {
	failures []string
	notes    []string
}

func (r *report) fail(format string, args ...any) {
	r.failures = append(r.failures, fmt.Sprintf(format, args...))
}

func (r *report) note(format string, args ...any) {
	r.notes = append(r.notes, fmt.Sprintf(format, args...))
}

func main() {
	dir := "../.."
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "kitcheck: %v\n", err)
		os.Exit(2)
	}

	art, err := spec.LoadFromDirectory(abs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "INVALID: %s\n  %v\n", abs, err)
		os.Exit(1)
	}

	r := &report{}
	checkArtifact(art, r)
	checkRepoConsistency(abs, r)
	checkV3Descriptor(abs, art, r)

	fmt.Printf("kit:      %s (%s, schema v%s, version %s)\n",
		art.Manifest.Name, art.Manifest.Kind, art.Manifest.SchemaVersion, versionOrDash(art.Manifest.Version))
	fmt.Printf("spec:     VALID per docker/sbx-kits-contrib ValidateArtifact\n")
	for _, w := range art.Warnings {
		fmt.Printf("warning:  %s\n", w)
	}
	for _, n := range r.notes {
		fmt.Printf("check:    %s\n", n)
	}
	if len(r.failures) > 0 {
		fmt.Fprintf(os.Stderr, "\nFAILED %d check(s):\n", len(r.failures))
		for _, f := range r.failures {
			fmt.Fprintf(os.Stderr, "  - %s\n", f)
		}
		os.Exit(1)
	}
	fmt.Println("result:   OK")
}

func versionOrDash(v string) string {
	if v == "" {
		return "-"
	}
	return v
}

func checkArtifact(art *spec.Artifact, r *report) {
	// Engine rule (SPEC-v2 §5.4): inject[].domain MUST appear in
	// permissions.network.allow. ValidateArtifact does not check it, and the
	// failure it prevents surfaces as a proxy 403 mid-scrape.
	allow := map[string]bool{}
	if art.Caps != nil && art.Caps.Network != nil {
		for _, d := range art.Caps.Network.Allow {
			allow[d] = true
			allow[strings.SplitN(d, ":", 2)[0]] = true
		}
	}
	for _, c := range art.Credentials {
		for _, host := range c.RoutingHosts() {
			if !allow[host] && !allow[strings.SplitN(host, ":", 2)[0]] {
				r.fail("credential %q routes %q, which is not in permissions.network.allow", c.Service, host)
			}
		}
		if c.ApiKey != nil && c.ApiKey.Name != "" && !c.ApiKey.ProxyManaged {
			r.fail("credential %q sets apiKey.name=%s without proxyManaged: true, so the env var is not set in-container",
				c.Service, c.ApiKey.Name)
		}
	}
	if len(art.Credentials) > 0 {
		r.note("%d credential(s) route only to allowed domains, sentinel set in-container", len(art.Credentials))
	}

	// SPEC-v2 §5.5: the runtime owns these prefixes; a kit must not set them.
	if art.Environment != nil {
		for name := range art.Environment.Variables {
			for _, p := range reservedEnvPrefixes {
				if strings.HasPrefix(name, p) {
					r.fail("environment.variables sets %q, which uses the runtime-reserved prefix %q", name, p)
				}
			}
		}
		// A kit that hardcodes the sentinel defeats proxyManaged and hides an
		// unbound credential behind a 401 (SPEC-v2 §9.5).
		for name, value := range art.Environment.Variables {
			if value == "proxy-managed" {
				r.fail("environment.variables hardcodes the proxy sentinel for %q; use credentials[].apiKey.proxyManaged instead", name)
			}
		}
	}
}

var (
	specPinRE     = regexp.MustCompile(`(?m)^\s*SDK_VERSION=([0-9]+\.[0-9]+\.[0-9]+)\s*$`)
	readmePinRE   = regexp.MustCompile(`firecrawl-py[= ]+([0-9]+\.[0-9]+\.[0-9]+)`)
	specVersionRE = regexp.MustCompile(`(?m)^version:\s*"?([0-9]+\.[0-9]+\.[0-9]+)"?`)
	changelogRE   = regexp.MustCompile(`(?m)^##\s+\[?v?([0-9]+\.[0-9]+\.[0-9]+)\]?`)
	gitRefRE      = regexp.MustCompile(`git\+https://[^\s"'` + "`" + `)]+`)
	sha40RE       = regexp.MustCompile(`ref=[0-9a-f]{40}`)
)

func checkRepoConsistency(dir string, r *report) {
	specText := mustRead(filepath.Join(dir, "spec.yaml"), r)
	readme := mustRead(filepath.Join(dir, "README.md"), r)
	if specText == "" || readme == "" {
		return
	}

	// The SDK version is pinned once, in spec.yaml's install hook. Anywhere the
	// README quotes it, it has to agree, or a user verifying the install sees a
	// version mismatch and assumes the kit is broken.
	specPins := distinct(specPinRE.FindAllStringSubmatch(specText, -1))
	readmePins := distinct(readmePinRE.FindAllStringSubmatch(readme, -1))
	switch {
	case len(specPins) != 1:
		r.fail("spec.yaml must pin exactly one SDK_VERSION=X.Y.Z, found %v", specPins)
	case len(readmePins) == 0:
		r.fail("README.md never states the pinned firecrawl-py version (%s)", specPins[0])
	case len(readmePins) > 1 || readmePins[0] != specPins[0]:
		r.fail("README.md states firecrawl-py %v but spec.yaml pins %s", readmePins, specPins[0])
	default:
		r.note("firecrawl-py pin %s agrees in spec.yaml and README.md", specPins[0])
	}

	// Every git+ reference the README hands a user must be SHA-pinned: the
	// loader's strict-pin rule rejects tags and branches.
	for _, ref := range gitRefRE.FindAllString(readme, -1) {
		if !sha40RE.MatchString(ref) && !strings.Contains(ref, "<") {
			r.fail("README git+ reference is not pinned to a 40-hex commit SHA: %s", ref)
		}
	}

	changelog := mustRead(filepath.Join(dir, "CHANGELOG.md"), r)
	specVersion := ""
	if m := specVersionRE.FindStringSubmatch(specText); m != nil {
		specVersion = m[1]
	}
	if specVersion == "" {
		r.fail("spec.yaml has no version: field")
		return
	}
	if m := changelogRE.FindStringSubmatch(changelog); m == nil {
		r.fail("CHANGELOG.md has no released version heading")
	} else if m[1] != specVersion {
		r.fail("spec.yaml version %s does not match newest CHANGELOG.md entry %s", specVersion, m[1])
	} else {
		r.note("kit version %s matches the newest CHANGELOG entry", specVersion)
	}
}

func distinct(matches [][]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range matches {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

func mustRead(path string, r *report) string {
	b, err := os.ReadFile(path)
	if err != nil {
		r.fail("read %s: %v", filepath.Base(path), err)
		return ""
	}
	return string(b)
}

// v3Descriptor is the subset of the Kit v3 grammar this tool compares against
// spec.yaml. Strict decoding is the frontend's job; this decodes leniently.
type v3Descriptor struct {
	SchemaVersion string `yaml:"schemaVersion"`
	Kind          string `yaml:"kind"`
	Version       string `yaml:"version"`
	Args          map[string]struct {
		Default string `yaml:"default"`
	} `yaml:"args"`
	Provides     []string `yaml:"provides"`
	Capabilities []struct {
		Type   string    `yaml:"type"`
		Config yaml.Node `yaml:"config"`
	} `yaml:"capabilities"`
}

type v3NetworkPolicy struct {
	Install struct {
		Allow []string `yaml:"allow"`
	} `yaml:"install"`
	Runtime struct {
		Allow []string `yaml:"allow"`
	} `yaml:"runtime"`
}

type v3Credential struct {
	Service string `yaml:"service"`
	Phase   string `yaml:"phase"`
	APIKey  struct {
		Name         string `yaml:"name"`
		ProxyManaged bool   `yaml:"proxyManaged"`
		Inject       []struct {
			Domain string `yaml:"domain"`
		} `yaml:"inject"`
	} `yaml:"apiKey"`
}

type v3Lifecycle struct {
	Install []struct {
		Command string `yaml:"command"`
		User    string `yaml:"user"`
	} `yaml:"install"`
}

type v3AgentContext struct {
	ContentFile string `yaml:"contentFile"`
	Content     string `yaml:"content"`
}

var v3PinRE = regexp.MustCompile(`(?m)^\s*SDK_VERSION="\$\{\{ kit\.args\.sdkVersion \}\}"\s*$`)

// checkV3Descriptor keeps firecrawl/firecrawl.yaml in step with spec.yaml. The two
// are published side by side (v2 for the built-in agents, v3 for v3
// workloads), so a change that lands in one and not the other ships two kits
// that disagree about what they do.
func checkV3Descriptor(dir string, art *spec.Artifact, r *report) {
	path := filepath.Join(dir, "firecrawl", "firecrawl.yaml")
	text := mustRead(path, r)
	if text == "" {
		return
	}
	var d v3Descriptor
	if err := yaml.Unmarshal([]byte(text), &d); err != nil {
		r.fail("firecrawl/firecrawl.yaml does not parse: %v", err)
		return
	}
	if d.SchemaVersion != "3" || d.Kind != "mixin" {
		r.fail("firecrawl/firecrawl.yaml must declare schemaVersion \"3\" and kind: mixin, got %q / %q", d.SchemaVersion, d.Kind)
	}
	if d.Version != art.Manifest.Version {
		r.fail("firecrawl/firecrawl.yaml version %s does not match spec.yaml version %s", d.Version, art.Manifest.Version)
	}

	// One SDK pin: spec.yaml's SDK_VERSION= line and the v3 arg default.
	specText := mustRead(filepath.Join(dir, "spec.yaml"), r)
	specPins := distinct(specPinRE.FindAllStringSubmatch(specText, -1))
	sdk, ok := d.Args["sdkVersion"]
	switch {
	case !ok || sdk.Default == "":
		r.fail("firecrawl/firecrawl.yaml must declare args.sdkVersion with a default")
	case len(specPins) == 1 && sdk.Default != specPins[0]:
		r.fail("firecrawl/firecrawl.yaml args.sdkVersion default %s does not match spec.yaml SDK_VERSION=%s", sdk.Default, specPins[0])
	}
	wantProvide := "firecrawl-py@${{ kit.args.sdkVersion }}"
	if len(d.Provides) != 1 || d.Provides[0] != wantProvide {
		r.fail("firecrawl/firecrawl.yaml must provide exactly %q, got %v", wantProvide, d.Provides)
	}

	// Capabilities, decoded one by one against the v2 fields they mirror.
	var (
		seen    = map[string]bool{}
		v2Allow = map[string]bool{}
	)
	if art.Caps != nil && art.Caps.Network != nil {
		for _, h := range art.Caps.Network.Allow {
			v2Allow[strings.SplitN(h, ":", 2)[0]] = true
		}
	}
	for _, c := range d.Capabilities {
		if seen[c.Type] {
			r.fail("firecrawl/firecrawl.yaml declares %s twice", c.Type)
		}
		seen[c.Type] = true
		switch c.Type {
		case "com.docker.sandbox/network-policy@1":
			var np v3NetworkPolicy
			if err := c.Config.Decode(&np); err != nil {
				r.fail("v3 network-policy@1 config: %v", err)
				continue
			}
			v3Allow := map[string]bool{}
			for _, h := range append(append([]string{}, np.Install.Allow...), np.Runtime.Allow...) {
				v3Allow[strings.SplitN(h, ":", 2)[0]] = true
			}
			for h := range v2Allow {
				if !v3Allow[h] {
					r.fail("spec.yaml allows %q but firecrawl/firecrawl.yaml does not list it in any phase", h)
				}
			}
			for h := range v3Allow {
				if !v2Allow[h] {
					r.fail("firecrawl/firecrawl.yaml allows %q but spec.yaml does not", h)
				}
			}
			for _, h := range np.Runtime.Allow {
				if strings.HasPrefix(h, "pypi.") || strings.HasSuffix(h, "pythonhosted.org") {
					r.fail("firecrawl/firecrawl.yaml grants %q at runtime; PyPI is install-phase only", h)
				}
			}
		case "com.docker.sandbox/credential@1":
			var cred v3Credential
			if err := c.Config.Decode(&cred); err != nil {
				r.fail("v3 credential@1 config: %v", err)
				continue
			}
			if len(art.Credentials) != 1 {
				r.fail("spec.yaml declares %d credentials; the v3 comparison assumes one", len(art.Credentials))
				continue
			}
			v2 := art.Credentials[0]
			if cred.Service != v2.Service {
				r.fail("v3 credential service %q does not match spec.yaml %q", cred.Service, v2.Service)
			}
			if cred.Phase != "runtime" {
				r.fail("v3 credential phase must be runtime, got %q", cred.Phase)
			}
			if v2.ApiKey != nil {
				if cred.APIKey.Name != v2.ApiKey.Name {
					r.fail("v3 credential apiKey.name %q does not match spec.yaml %q", cred.APIKey.Name, v2.ApiKey.Name)
				}
				if cred.APIKey.ProxyManaged != v2.ApiKey.ProxyManaged {
					r.fail("v3 credential proxyManaged=%v does not match spec.yaml %v", cred.APIKey.ProxyManaged, v2.ApiKey.ProxyManaged)
				}
			}
			var v3Domains []string
			for _, inj := range cred.APIKey.Inject {
				v3Domains = append(v3Domains, inj.Domain)
			}
			v2Domains := v2.RoutingHosts()
			for i := range v2Domains {
				v2Domains[i] = strings.SplitN(v2Domains[i], ":", 2)[0]
			}
			if strings.Join(v3Domains, ",") != strings.Join(v2Domains, ",") {
				r.fail("v3 credential inject domains %v do not match spec.yaml %v", v3Domains, v2Domains)
			}
		case "com.docker.sandbox/lifecycle@1":
			var lc v3Lifecycle
			if err := c.Config.Decode(&lc); err != nil {
				r.fail("v3 lifecycle@1 config: %v", err)
				continue
			}
			if len(lc.Install) != 1 {
				r.fail("v3 lifecycle@1 must declare exactly one install hook, got %d", len(lc.Install))
				continue
			}
			if lc.Install[0].User != "1000" {
				r.fail("v3 install hook runs as %q; spec.yaml installs as user 1000", lc.Install[0].User)
			}
			if !v3PinRE.MatchString(lc.Install[0].Command) {
				r.fail("v3 install hook must set SDK_VERSION from ${{ kit.args.sdkVersion }}")
			}
		case "com.docker.sandbox/agent-context@1":
			var ac v3AgentContext
			if err := c.Config.Decode(&ac); err != nil {
				r.fail("v3 agent-context@1 config: %v", err)
				continue
			}
			if ac.ContentFile == "" {
				r.fail("v3 agent-context@1 must reference a contentFile (a mixin owns no profile)")
				continue
			}
			// The loader surfaces v2 agentInstructions.content as AgentContext.
			rel := filepath.Join("firecrawl", filepath.Clean(ac.ContentFile))
			body := mustRead(filepath.Join(dir, rel), r)
			if strings.TrimSpace(body) != strings.TrimSpace(art.AgentContext) {
				r.fail("%s differs from spec.yaml agentInstructions.content; the agent must read the same text in both kits", rel)
			}
		default:
			r.fail("firecrawl/firecrawl.yaml declares %s, which spec.yaml has no counterpart for", c.Type)
		}
	}
	for _, want := range []string{
		"com.docker.sandbox/network-policy@1",
		"com.docker.sandbox/credential@1",
		"com.docker.sandbox/lifecycle@1",
		"com.docker.sandbox/agent-context@1",
	} {
		if !seen[want] {
			r.fail("firecrawl/firecrawl.yaml is missing %s", want)
		}
	}
	if len(r.failures) == 0 {
		r.note("firecrawl/firecrawl.yaml agrees with spec.yaml (version, SDK pin, hosts, credential, agent instructions)")
	}
}

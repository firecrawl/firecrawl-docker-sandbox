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

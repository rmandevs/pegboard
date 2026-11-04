// Package sbom exports pegboard.lock as a software bill of materials:
// CycloneDX 1.5 or SPDX 2.3, both in JSON. It lists what the lock pins
// (git skills and included stacks) with their commit and content hash. It
// is an inventory, not a compliance attestation: local skills are not in the
// lock, and MCP servers run packages pegboard does not resolve.
package sbom

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/rmandevs/pegboard/internal/skills"
)

// Item is one pinned component.
type Item struct {
	Kind   string // skill or include
	Name   string
	Source string // as declared
	Repo   string // git URL, if any
	Path   string
	Commit string
	Hash   string // sha256:…
}

// Items lists what a lock pins, in a stable order.
func Items(locks ...*skills.Lock) []Item {
	var out []Item
	for _, l := range locks {
		if l == nil {
			continue
		}
		for src, ls := range l.Sources {
			parsed, _ := skills.Parse(src)
			for name, h := range ls.Skills {
				out = append(out, Item{Kind: "skill", Name: name, Source: src, Repo: parsed.URL, Path: parsed.Path, Commit: ls.Commit, Hash: h})
			}
		}
		for raw, li := range l.Includes {
			it := Item{Kind: "include", Name: raw, Source: raw, Commit: li.Commit, Hash: li.Hash}
			if in, err := skills.ParseInclude(raw); err == nil {
				it.Repo, it.Path = in.Git.URL, in.Git.Path
				if in.URL != "" {
					it.Repo = in.URL
				}
			}
			out = append(out, it)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Kind != b.Kind {
			return a.Kind > b.Kind // skills first
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Source < b.Source
	})
	return out
}

func hexHash(h string) string { return strings.TrimPrefix(h, "sha256:") }

var ghRe = regexp.MustCompile(`^https://github\.com/([^/]+)/([^/]+?)(?:\.git)?$`)

// purl is the package URL of a GitHub-hosted item; "" otherwise.
func purl(it Item) string {
	m := ghRe.FindStringSubmatch(it.Repo)
	if m == nil {
		return ""
	}
	p := "pkg:github/" + strings.ToLower(m[1]) + "/" + strings.ToLower(m[2])
	if it.Commit != "" {
		p += "@" + it.Commit
	}
	if it.Path != "" {
		p += "#" + it.Path
	}
	return p
}

// id is a stable identifier derived from the content.
func id(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func uuid(seed string) string {
	h := id(seed)
	return fmt.Sprintf("%s-%s-5%s-a%s-%s", h[0:8], h[8:12], h[13:16], h[17:20], h[20:32])
}

func seed(items []Item) string {
	var b strings.Builder
	for _, it := range items {
		b.WriteString(it.Kind + it.Name + it.Source + it.Hash + "\n")
	}
	return b.String()
}

// Doc describes the document: what it covers and who made it.
type Doc struct {
	Name    string // project or scope
	Tool    string // pegboard version
	Created time.Time
}

// CycloneDX builds a CycloneDX 1.5 JSON document.
func CycloneDX(d Doc, items []Item) map[string]any {
	comps := []map[string]any{}
	for _, it := range items {
		c := map[string]any{
			"type":    "library",
			"bom-ref": it.Kind + ":" + it.Name + ":" + id(it.Source, it.Name)[:12],
			"name":    it.Name,
			"group":   it.Kind,
			"hashes":  []map[string]string{{"alg": "SHA-256", "content": hexHash(it.Hash)}},
			"properties": []map[string]string{
				{"name": "pegboard:kind", "value": it.Kind},
				{"name": "pegboard:source", "value": it.Source},
			},
		}
		if it.Commit != "" {
			c["version"] = it.Commit
		}
		if p := purl(it); p != "" {
			c["purl"] = p
		}
		if it.Repo != "" {
			c["externalReferences"] = []map[string]string{{"type": "vcs", "url": it.Repo}}
		}
		comps = append(comps, c)
	}
	return map[string]any{
		"bomFormat":    "CycloneDX",
		"specVersion":  "1.5",
		"serialNumber": "urn:uuid:" + uuid(d.Name+seed(items)),
		"version":      1,
		"metadata": map[string]any{
			"timestamp": d.Created.UTC().Format(time.RFC3339),
			"tools": map[string]any{"components": []map[string]string{
				{"type": "application", "name": "pegboard", "version": d.Tool},
			}},
			"component": map[string]string{"type": "application", "name": d.Name, "bom-ref": "root"},
		},
		"components": comps,
	}
}

var spdxIDRe = regexp.MustCompile(`[^A-Za-z0-9.-]+`)

// SPDX builds an SPDX 2.3 JSON document.
func SPDX(d Doc, items []Item) map[string]any {
	pkgs := []map[string]any{}
	rels := []map[string]string{}
	for _, it := range items {
		sid := "SPDXRef-" + spdxIDRe.ReplaceAllString(it.Kind+"-"+it.Name, "-") + "-" + id(it.Source, it.Name)[:8]
		loc := "NOASSERTION"
		if it.Repo != "" {
			loc = it.Repo
			if !strings.HasSuffix(strings.ToLower(it.Repo), ".zip") {
				loc = "git+" + strings.TrimPrefix(it.Repo, "git+")
				if it.Commit != "" {
					loc += "@" + it.Commit
				}
				if it.Path != "" {
					loc += "#" + it.Path
				}
			}
		}
		p := map[string]any{
			"SPDXID":           sid,
			"name":             it.Name,
			"downloadLocation": loc,
			"filesAnalyzed":    false,
			"checksums":        []map[string]string{{"algorithm": "SHA256", "checksumValue": hexHash(it.Hash)}},
			"licenseConcluded": "NOASSERTION",
			"licenseDeclared":  "NOASSERTION",
			"copyrightText":    "NOASSERTION",
			"comment":          "pegboard " + it.Kind + " from " + it.Source,
		}
		if it.Commit != "" {
			p["versionInfo"] = it.Commit
		}
		if pu := purl(it); pu != "" {
			p["externalRefs"] = []map[string]string{{"referenceCategory": "PACKAGE-MANAGER", "referenceType": "purl", "referenceLocator": pu}}
		}
		pkgs = append(pkgs, p)
		rels = append(rels, map[string]string{"spdxElementId": "SPDXRef-DOCUMENT", "relationshipType": "DESCRIBES", "relatedSpdxElement": sid})
	}
	return map[string]any{
		"spdxVersion":       "SPDX-2.3",
		"dataLicense":       "CC0-1.0",
		"SPDXID":            "SPDXRef-DOCUMENT",
		"name":              d.Name,
		"documentNamespace": "https://usepegboard.dev/spdx/" + uuid(d.Name+seed(items)),
		"creationInfo": map[string]any{
			"created":  d.Created.UTC().Format(time.RFC3339),
			"creators": []string{"Tool: pegboard-" + d.Tool},
		},
		"packages":      pkgs,
		"relationships": rels,
	}
}

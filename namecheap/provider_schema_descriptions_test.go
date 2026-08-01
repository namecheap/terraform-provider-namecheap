package namecheap_provider

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/stretchr/testify/assert"
)

// The registry pages are generated from the schema, so an attribute with no
// Description renders as a blank cell on the published documentation. These
// tests are the enforcement arm of that: they walk every attribute the provider
// exposes — provider config, every resource, every data source, and every
// nested block within them — and fail on anything undocumented.
//
// This is what stops the generated docs decaying as resources are added: a new
// attribute without a description cannot reach master.

// schemaWalkResult is one attribute found during the walk, identified by a path
// like "namecheap_domain_records.record.hostname".
type schemaWalkResult struct {
	path        string
	description string
}

// walkSchema collects every attribute reachable from s, recursing into nested
// resources so block attributes are covered too. prefix names the container.
func walkSchema(prefix string, s map[string]*schema.Schema, out *[]schemaWalkResult) {
	for name, attr := range s {
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		*out = append(*out, schemaWalkResult{path: path, description: attr.Description})

		if nested, ok := attr.Elem.(*schema.Resource); ok && nested != nil {
			walkSchema(path, nested.Schema, out)
		}
	}
}

// providerAttributes returns every attribute of the provider config, all
// resources and all data sources, sorted for deterministic failure output.
func providerAttributes(t *testing.T) []schemaWalkResult {
	t.Helper()
	p := Provider()

	var found []schemaWalkResult
	walkSchema("provider", p.Schema, &found)
	for name, resource := range p.ResourcesMap {
		walkSchema(name, resource.Schema, &found)
	}
	for name, dataSource := range p.DataSourcesMap {
		walkSchema(name, dataSource.Schema, &found)
	}

	sort.Slice(found, func(i, j int) bool { return found[i].path < found[j].path })
	return found
}

// TestSchemaDescriptionsArePresent fails on any attribute the registry would
// render with an empty description.
func TestSchemaDescriptionsArePresent(t *testing.T) {
	var missing []string
	for _, attr := range providerAttributes(t) {
		if strings.TrimSpace(attr.description) == "" {
			missing = append(missing, attr.path)
		}
	}

	assert.Empty(t, missing,
		"every schema attribute needs a Description — the registry page is generated from it, "+
			"so an empty one publishes a blank cell. Undocumented attributes:\n  %s",
		strings.Join(missing, "\n  "))
}

// TestSchemaDescriptionsAreUsable catches descriptions that exist but say
// nothing — a single word, or a restatement of the attribute name. Those pass a
// presence check while still leaving the reader with no information.
func TestSchemaDescriptionsAreUsable(t *testing.T) {
	const minWords = 3

	var weak []string
	for _, attr := range providerAttributes(t) {
		description := strings.TrimSpace(attr.description)
		if description == "" {
			continue // reported by TestSchemaDescriptionsArePresent
		}

		if len(strings.Fields(description)) < minWords {
			weak = append(weak, fmt.Sprintf("%s: %q (fewer than %d words)", attr.path, description, minWords))
			continue
		}

		// "hostname" described as "Hostname" tells a reader nothing they could
		// not read off the attribute name.
		name := attr.path[strings.LastIndex(attr.path, ".")+1:]
		if strings.EqualFold(strings.TrimSuffix(description, "."), strings.ReplaceAll(name, "_", " ")) {
			weak = append(weak, fmt.Sprintf("%s: %q merely restates the attribute name", attr.path, description))
		}
	}

	assert.Empty(t, weak, "these descriptions are present but uninformative:\n  %s", strings.Join(weak, "\n  "))
}

// TestSchemaDescriptionsCoverEverySurface guards the walk itself: if the walk
// silently stopped finding attributes — a refactor to how resources register,
// say — the two tests above would pass vacuously.
func TestSchemaDescriptionsCoverEverySurface(t *testing.T) {
	p := Provider()
	found := providerAttributes(t)

	assert.NotEmpty(t, p.ResourcesMap, "provider should expose resources")
	assert.NotEmpty(t, p.DataSourcesMap, "provider should expose data sources")

	prefixes := map[string]bool{}
	for _, attr := range found {
		prefixes[attr.path[:strings.Index(attr.path+".", ".")]] = true
	}
	assert.True(t, prefixes["provider"], "walk should cover provider configuration")
	for name := range p.ResourcesMap {
		assert.True(t, prefixes[name], "walk should cover resource %s", name)
	}
	for name := range p.DataSourcesMap {
		assert.True(t, prefixes[name], "walk should cover data source %s", name)
	}

	// Nested blocks are the easiest thing for a walk to miss, and the records
	// resource has one, so assert the recursion actually descended into it.
	var nested bool
	for _, attr := range found {
		if attr.path == "namecheap_domain_records.record.hostname" {
			nested = true
		}
	}
	assert.True(t, nested, "walk should descend into nested blocks (namecheap_domain_records.record)")
}

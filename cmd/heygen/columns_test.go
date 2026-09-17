package main

import (
	"encoding/json"
	"testing"

	"github.com/heygen-com/heygen-cli/gen"
	"github.com/heygen-com/heygen-cli/internal/command"
)

// itemProperties returns the properties of a single item in spec's response
// payload, unwrapping the data envelope and, for a list, its array element.
// Every curated command resolves to one of those two shapes today, so it
// reports an empty result instead of returning it: that means the schema moved,
// and skipping would retire the field checks below without failing.
func itemProperties(t *testing.T, key string, spec *command.Spec) map[string]any {
	t.Helper()
	if spec.ResponseSchema == "" {
		t.Errorf("%s: has curated columns but no response schema to check them against", key)
		return nil
	}
	var schema struct {
		Properties struct {
			Data struct {
				Properties map[string]any `json:"properties"`
				Items      struct {
					Properties map[string]any `json:"properties"`
				} `json:"items"`
			} `json:"data"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(spec.ResponseSchema), &schema); err != nil {
		t.Fatalf("%s: response schema is not valid JSON: %v", key, err)
	}
	if props := schema.Properties.Data.Items.Properties; len(props) > 0 {
		return props
	}
	if props := schema.Properties.Data.Properties; len(props) > 0 {
		return props
	}
	t.Errorf("%s: response schema exposes no data properties", key)
	return nil
}

// Both halves of a curated column entry fail silently at runtime.
// defaultColumnsForSpec is a bare map lookup, so a key matching no command
// yields nil columns and renders a generic table; and a Field naming no
// response property renders an empty cell. Neither raises an error, so only
// this test tells either apart from a table that is legitimately sparse.
func TestDefaultColumnsMatchGeneratedSchemas(t *testing.T) {
	specs := make(map[string]*command.Spec)
	for group, groupSpecs := range gen.Groups {
		for _, spec := range groupSpecs {
			specs[group+"/"+spec.Name] = spec
		}
	}

	for key, columns := range DefaultColumns {
		spec, ok := specs[key]
		if !ok {
			t.Errorf("DefaultColumns key %q matches no generated command", key)
			continue
		}
		props := itemProperties(t, key, spec)
		if props == nil {
			continue // itemProperties already reported why
		}
		for _, col := range columns {
			if _, ok := props[col.Field]; !ok {
				t.Errorf("%s: column %q uses field %q, absent from the response schema",
					key, col.Header, col.Field)
			}
		}
	}
}

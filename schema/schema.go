// Package schema publishes and applies the pegboard.yaml JSON Schema.
package schema

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

//go:embed pegboard.schema.json
var JSON []byte

const url = "https://raw.githubusercontent.com/rmandevs/pegboard/main/schema/pegboard.schema.json"

var compiled = sync.OnceValues(func() (*jsonschema.Schema, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(JSON))
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(url, doc); err != nil {
		return nil, err
	}
	return c.Compile(url)
})

// ValidateYAML validates the contents of a pegboard.yaml against the schema.
func ValidateYAML(src []byte) error {
	var v any
	if err := yaml.Unmarshal(src, &v); err != nil {
		return fmt.Errorf("invalid YAML: %w", err)
	}
	// YAML → JSON → jsonschema model: this way types (integers, maps with
	// non-string keys) look the way an editor validator would see them.
	j, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("YAML is not representable as JSON: %w", err)
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(j))
	if err != nil {
		return err
	}
	s, err := compiled()
	if err != nil {
		return fmt.Errorf("invalid internal schema: %w", err)
	}
	return s.Validate(inst)
}

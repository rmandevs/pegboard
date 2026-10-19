// Package schema publica y aplica el JSON Schema de pegboard.yaml.
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

const url = "https://raw.githubusercontent.com/rmandevs/agents-casc/main/schema/pegboard.schema.json"

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

// ValidateYAML valida el contenido de un pegboard.yaml contra el schema.
func ValidateYAML(src []byte) error {
	var v any
	if err := yaml.Unmarshal(src, &v); err != nil {
		return fmt.Errorf("YAML inválido: %w", err)
	}
	// YAML → JSON → modelo de jsonschema: así los tipos (enteros, mapas
	// con llaves no string) quedan como los vería un validador de editor.
	j, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("YAML no representable como JSON: %w", err)
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(j))
	if err != nil {
		return err
	}
	s, err := compiled()
	if err != nil {
		return fmt.Errorf("schema interno inválido: %w", err)
	}
	return s.Validate(inst)
}

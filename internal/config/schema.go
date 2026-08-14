package config

import (
	"encoding/json"

	"github.com/invopop/jsonschema"
)

// BoxSchema returns the JSON Schema for kind: Box documents
// (`bastion config schema`).
func BoxSchema() ([]byte, error) {
	r := jsonschema.Reflector{}
	s := r.Reflect(&Box{})
	s.ID = "https://sanketsaurav.com/bastion/schema/v1alpha1.json"
	s.Title = "Bastion Box"
	s.Description = "A bastion/v1alpha1 Box definition."
	return json.MarshalIndent(s, "", "  ")
}

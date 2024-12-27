package helm

import (
	"bytes"
	"fmt"

	"github.com/invopop/jsonschema"
	"kcl-lang.io/kcl-go/pkg/tools/gen"

	"github.com/MacroPower/kclx/pkg/util/safekcl"
)

type SchemaMode string

const (
	SchemaAuto       SchemaMode = "auto"
	SchemaFromValues SchemaMode = "values"
	SchemaNone       SchemaMode = "none"
)

// Chart represents the KCL schema `helm.Chart`.
type Chart struct {
	// Chart is the Helm chart name.
	Chart string `json:"chart" jsonschema:"description=The Helm chart name."`
	// RepoURL is the URL of the Helm chart repository.
	RepoURL string `json:"repoURL" jsonschema:"description=The URL of the Helm chart repository."`
	// TargetRevision is the semver tag for the chart's version.
	TargetRevision string `json:"targetRevision" jsonschema:"description=The semver tag for the chart's version."`
	// ReleaseName is the Helm release name to use. If omitted it will use the chart name.
	ReleaseName string `json:"releaseName,omitempty" jsonschema:"-,description=The Helm release name to use. If omitted it will use the chart name."`
	// SkipCRDs will skip the custom resource definition installation step (--skip-crds).
	SkipCRDs bool `json:"skipCRDs,omitempty" jsonschema:"-,description=Skip the custom resource definition installation step."`
	// PassCredentials will pass credentials to all domains (--pass-credentials).
	PassCredentials bool `json:"passCredentials,omitempty" jsonschema:"-,description=Pass credentials to all domains."`
	// SchemaMode is the mode to use for schema generation.
	SchemaMode SchemaMode `json:"schemaMode,omitempty" jsonschema:"description=The mode to use for schema generation."`
	// SchemaURL is the URL of the schema to use. If set, it will override schemaMode.
	SchemaURL string `json:"schemaURL,omitempty" jsonschema:"-,description=The URL of the schema to use. If set, it will override schemaMode."`
	// Values is the values to use for the chart.
	Values any `json:"values,omitempty" jsonschema:"description=The values to use for the chart."`
}

func (c *Chart) GenerateKcl(b *bytes.Buffer) error {
	r := &jsonschema.Reflector{
		DoNotReference: true,
		ExpandedStruct: true,
	}
	js := r.Reflect(&Chart{})
	if cv, ok := js.Properties.Get("chart"); ok {
		cv.Default = c.Chart
	}
	if cv, ok := js.Properties.Get("repoURL"); ok {
		cv.Default = c.RepoURL
	}
	if cv, ok := js.Properties.Get("targetRevision"); ok {
		cv.Default = c.TargetRevision
	}

	jsBytes, err := js.MarshalJSON()
	if err != nil {
		return fmt.Errorf("failed to marshal json schema: %w", err)
	}

	if err := safekcl.Gen.GenKcl(b, "chart", jsBytes, &gen.GenKclOptions{
		Mode:          gen.ModeJsonSchema,
		CastingOption: gen.OriginalName,
	}); err != nil {
		return fmt.Errorf("failed to generate kcl schema: %w", err)
	}

	return nil
}
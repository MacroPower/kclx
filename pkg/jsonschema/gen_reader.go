package jsonschema

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	helmschema "github.com/dadav/helm-schema/pkg/schema"
	"gopkg.in/yaml.v3"
)

// DefaultReaderGenerator is an opinionated [ReaderGenerator].
var DefaultReaderGenerator = NewReaderGenerator()

var _ FileGenerator = DefaultReaderGenerator

// ReaderGenerator reads a JSON Schema from a given location and returns
// corresponding []byte representations.
type ReaderGenerator struct{}

// NewReaderGenerator creates a new [ReaderGenerator].
func NewReaderGenerator() *ReaderGenerator {
	return &ReaderGenerator{}
}

// FromPaths reads a JSON Schema from at least one of the given file paths or
// URLs and returns the corresponding []byte representation. It will return an
// error only if none of the paths provide a valid JSON Schema.
func (g *ReaderGenerator) FromPaths(paths ...string) ([]byte, error) {
	if len(paths) == 0 {
		return nil, errors.New("no paths provided")
	}
	if len(paths) == 1 {
		return g.fromPath(paths[0])
	}

	pathErrs := map[string]error{}
	for _, path := range paths {
		jsBytes, err := g.fromPath(path)
		if err == nil {
			return jsBytes, nil
		}
		pathErrs[path] = err
	}

	pathErrMsgs := []string{}
	for path, err := range pathErrs {
		pathErrMsgs = append(pathErrMsgs, fmt.Sprintf("\t%s: %s\n", path, err))
	}
	multiErr := fmt.Errorf("could not read JSON Schema from any of the provided paths:\n%s", pathErrMsgs)

	return nil, fmt.Errorf("error generating JSON Schema: %w", multiErr)
}

// fromPath reads a JSON Schema from the given file path or URL and returns the
// corresponding []byte representation.
func (g *ReaderGenerator) fromPath(path string) ([]byte, error) {
	schemaPath, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("failed to parse path: %w", err)
	}

	switch schemaPath.Scheme {
	case "http", "https":
		return g.FromURL(schemaPath)
	case "":
		return g.FromFile(schemaPath.Path)
	}

	return nil, fmt.Errorf("unsupported scheme: %s", schemaPath.Scheme)
}

func (g *ReaderGenerator) FromFile(path string) ([]byte, error) {
	jsBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	baseDir, _ := filepath.Abs(filepath.Dir(path))

	return g.FromReader(bytes.NewReader(jsBytes), baseDir)
}

func (g *ReaderGenerator) FromURL(schemaURL *url.URL) ([]byte, error) {
	schema, err := http.DefaultClient.Do(&http.Request{
		Method: http.MethodGet,
		URL:    schemaURL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed http request: %w", err)
	}
	defer schema.Body.Close()

	return g.FromReader(schema.Body, "")
}

func (g *ReaderGenerator) FromReader(r io.Reader, refBasePath string) ([]byte, error) {
	jsBytes, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read: %w", err)
	}

	return g.FromData(jsBytes, refBasePath)
}

func (g *ReaderGenerator) FromData(data []byte, refBasePath string) ([]byte, error) {
	// YAML is a superset of JSON, so this works and is simpler than re-writing
	// the Unmarshaler for JSON.
	var jsonNode yaml.Node
	if err := yaml.Unmarshal(data, &jsonNode); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON Schema: %w", err)
	}
	hs := &helmschema.Schema{}
	if err := hs.UnmarshalYAML(&jsonNode); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON Schema: %w", err)
	}

	if err := hs.Validate(); err != nil {
		return nil, fmt.Errorf("invalid schema: %w", err)
	}

	// Remove the ID to keep downstream KCL schema generation consistent.
	hs.Id = ""

	if err := handleSchemaRefs(hs, refBasePath); err != nil {
		return nil, fmt.Errorf("failed to handle schema refs: %w", err)
	}

	mhs := &helmschema.Schema{}
	mhs = mergeHelmSchemas(mhs, hs, true)

	if err := mhs.Validate(); err != nil {
		return nil, fmt.Errorf("invalid schema: %w", err)
	}
	if len(mhs.Properties) == 0 {
		return nil, errors.New("empty schema")
	}

	resolvedData, err := mhs.ToJson()
	if err != nil {
		return nil, fmt.Errorf("failed to convert schema to JSON: %w", err)
	}

	return resolvedData, nil
}

package helmutil

import (
	"bufio"
	"bytes"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"

	"kcl-lang.io/kcl-go"
	"kcl-lang.io/kcl-go/pkg/tools/gen"

	"github.com/MacroPower/kclx/pkg/helm"
	helmchart "github.com/MacroPower/kclx/pkg/helm/models"
	"github.com/MacroPower/kclx/pkg/jsonschema"
	"github.com/MacroPower/kclx/pkg/kclutil"
)

var (
	SchemaDefaultRegexp = regexp.MustCompile(`(\s+\S+:\s+\S+(\s+\|\s+\S+)*)(\s+=\s+\S+)`)
	SchemaValuesRegexp  = regexp.MustCompile(`(\s+values\??\s*:\s+)(.*)`)
)

const initialMainContents = `import helm

charts: helm.Charts = {}
`

func (c *ChartPkg) Add(chart, repoURL, targetRevision, schemaPath string, genType jsonschema.GeneratorType) error {
	repoNetURL, err := url.Parse(repoURL)
	if err != nil {
		return fmt.Errorf("failed to parse repo_url %s: %w", repoURL, err)
	}
	enableOCI := repoNetURL.Scheme == ""

	hc := helmchart.Chart{
		ChartBase: helmchart.ChartBase{
			Chart:          chart,
			RepoURL:        repoNetURL.String(),
			TargetRevision: targetRevision,
		},
	}

	chartDir := path.Join(c.BasePath, hc.GetSnakeCaseName())

	if err := c.Init(); err != nil {
		return fmt.Errorf("failed to init before add: %w", err)
	}
	if err := os.MkdirAll(chartDir, 0o755); err != nil {
		return fmt.Errorf("failed to create charts directory: %w", err)
	}

	if err := c.generateAndWriteChartKCL(hc, chartDir); err != nil {
		return err
	}

	var jsonSchemaBytes []byte
	switch genType {
	case jsonschema.NoGeneratorType:
		break
	case jsonschema.URLGeneratorType, jsonschema.LocalPathGeneratorType:
		jsonSchemaBytes, err = jsonschema.DefaultReaderGenerator.FromPaths(schemaPath)
		if err != nil {
			return fmt.Errorf("failed to fetch schema from %s: %w", schemaPath, err)
		}
	case jsonschema.AutoGeneratorType, jsonschema.ValueInferenceGeneratorType, jsonschema.PathGeneratorType:
		fileMatcher := jsonschema.GetFileFilter(genType)
		if schemaPath != "" {
			fileMatcher = func(f string) bool {
				return filePathsEqual(f, schemaPath)
			}
		}
		jsonSchemaBytes, err = helm.DefaultHelm.GetValuesJSONSchema(&helm.TemplateOpts{
			ChartName:       chart,
			TargetRevision:  targetRevision,
			RepoURL:         repoURL,
			EnableOCI:       enableOCI,
			PassCredentials: false,
		}, jsonschema.GetGenerator(genType), fileMatcher)
		if err != nil {
			return fmt.Errorf("failed to generate schema: %w", err)
		}
	}

	if len(jsonSchemaBytes) != 0 {
		if err := c.writeValuesSchemaFiles(jsonSchemaBytes, chartDir); err != nil {
			return err
		}
	}

	chartConfig := []string{
		fmt.Sprintf(`chart="%s"`, chart),
		fmt.Sprintf(`repoURL="%s"`, repoNetURL.String()),
		fmt.Sprintf(`targetRevision="%s"`, targetRevision),
		fmt.Sprintf(`schemaGenerator="%s"`, genType),
	}
	if err := c.updateChartsFile(c.BasePath, hc.GetSnakeCaseName(), chartConfig...); err != nil {
		return err
	}

	_, err = kcl.FormatPath(c.BasePath)
	if err != nil {
		return fmt.Errorf("failed to format kcl files: %w", err)
	}

	return nil
}

func (c *ChartPkg) generateAndWriteChartKCL(hc helmchart.Chart, chartDir string) error {
	kclChart := &bytes.Buffer{}
	if err := hc.GenerateKCL(kclChart); err != nil {
		return fmt.Errorf("failed to generate chart.k: %w", err)
	}

	kclChartFixed := &bytes.Buffer{}
	kclChartScanner := bufio.NewScanner(kclChart)
	for kclChartScanner.Scan() {
		line := kclChartScanner.Text()
		if line == "schema Chart:" {
			line = "import helm\n\nschema Chart(helm.Chart):"
		} else if SchemaValuesRegexp.MatchString(line) {
			line = SchemaValuesRegexp.ReplaceAllString(line, "${1}Values | ${2}")
		}
		kclChartFixed.WriteString(line + "\n")
	}
	if err := kclChartScanner.Err(); err != nil {
		return fmt.Errorf("failed to scan kcl schema: %w", err)
	}

	if err := os.WriteFile(path.Join(chartDir, "chart.k"), kclChartFixed.Bytes(), 0o600); err != nil {
		return fmt.Errorf("failed to write chart.k: %w", err)
	}
	return nil
}

func (c *ChartPkg) writeValuesSchemaFiles(jsonSchema []byte, chartDir string) error {
	if err := os.WriteFile(path.Join(chartDir, "values.schema.json"), jsonSchema, 0o600); err != nil {
		return fmt.Errorf("failed to write values.schema.json: %w", err)
	}

	kclSchema := &bytes.Buffer{}
	if err := kclutil.Gen.GenKcl(kclSchema, "values", jsonSchema, &gen.GenKclOptions{
		Mode:                  gen.ModeJsonSchema,
		CastingOption:         gen.OriginalName,
		UseIntegersForNumbers: true,
	}); err != nil {
		return fmt.Errorf("failed to generate kcl schema: %w", err)
	}

	kclSchemaFixed := &bytes.Buffer{}
	scanner := bufio.NewScanner(kclSchema)
	for scanner.Scan() {
		line := scanner.Text()
		line = SchemaDefaultRegexp.ReplaceAllString(line, "$1")
		kclSchemaFixed.WriteString(line + "\n")
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to scan kcl schema: %w", err)
	}

	if err := os.WriteFile(path.Join(chartDir, "values.schema.k"), kclSchemaFixed.Bytes(), 0o600); err != nil {
		return fmt.Errorf("failed to write values.schema.k: %w", err)
	}
	return nil
}

func (c *ChartPkg) updateChartsFile(vendorDir, chartKey string, chartConfig ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	mainFile := path.Join(vendorDir, "charts.k")
	if !fileExists(mainFile) {
		if err := os.WriteFile(mainFile, []byte(initialMainContents), 0o600); err != nil {
			return fmt.Errorf("failed to write '%s': %w", mainFile, err)
		}
	}
	imports := []string{"helm"}
	specs := []string{}
	for _, cc := range chartConfig {
		specs = append(specs, fmt.Sprintf(`charts.%s.%s`, chartKey, cc))
	}
	_, err := kcl.OverrideFile(mainFile, specs, imports)
	if err != nil {
		return fmt.Errorf("failed to update '%s': %w", mainFile, err)
	}
	return nil
}

func filePathsEqual(f1, f2 string) bool {
	return filepath.Clean(f1) == filepath.Clean(f2)
}

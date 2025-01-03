package helmutil_test

import (
	"os"
	"path"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
	"kcl-lang.io/cli/pkg/options"
	"kcl-lang.io/kcl-go"

	"github.com/MacroPower/kclipper/pkg/helmmodels"
	"github.com/MacroPower/kclipper/pkg/helmtest"
	"github.com/MacroPower/kclipper/pkg/helmutil"
	"github.com/MacroPower/kclipper/pkg/jsonschema"
)

const (
	addBasePath = "testdata/add"
)

func TestHelmChartAdd(t *testing.T) {
	t.Parallel()

	chartPath := path.Join(addBasePath, "charts")
	os.RemoveAll(chartPath)

	ca := helmutil.NewChartPkg(chartPath, helmtest.DefaultTestClient)

	err := ca.Init()
	require.NoError(t, err)

	tcs := map[string]struct {
		chart *helmmodels.ChartConfig
	}{
		"podinfo": {
			chart: &helmmodels.ChartConfig{
				ChartBase: helmmodels.ChartBase{
					Chart:           "podinfo",
					RepoURL:         "https://stefanprodan.github.io/podinfo",
					TargetRevision:  "6.7.1",
					SchemaValidator: jsonschema.HelmValidatorType,
				},
				SchemaGenerator: jsonschema.AutoGeneratorType,
			},
		},
		"app-template": {
			chart: &helmmodels.ChartConfig{
				ChartBase: helmmodels.ChartBase{
					Chart:          "app-template",
					RepoURL:        "https://bjw-s.github.io/helm-charts/",
					TargetRevision: "3.6.0",
				},
				SchemaGenerator: jsonschema.ChartPathGeneratorType,
				SchemaPath:      "charts/common/values.schema.json",
			},
		},
	}
	for name, tc := range tcs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := ca.Add(tc.chart.Chart, tc.chart.RepoURL, tc.chart.TargetRevision,
				tc.chart.SchemaPath, tc.chart.SchemaGenerator, tc.chart.SchemaValidator)
			require.NoError(t, err)

			depsOpt, err := options.LoadDepsFrom(chartPath, true)
			require.NoError(t, err)
			results, err := kcl.Test(
				&kcl.TestOptions{
					PkgList:  []string{chartPath},
					FailFast: true,
				},
				*depsOpt,
			)
			require.NoError(t, err)

			require.Emptyf(t, results.Info, "expected no errors, got %d: %#v", len(results.Info), results)
		})
	}
}

func TestDefaultReplacement(t *testing.T) {
	t.Parallel()

	tcs := map[string]struct {
		input string
		want  string
		re    *regexp.Regexp
		repl  string
	}{
		"simple": {
			input: `    key: str = "default"`,
			want:  `    key: str`,
			re:    helmutil.SchemaDefaultRegexp,
			repl:  "$1",
		},
		"two types": {
			input: `    key: str | int = 1`,
			want:  `    key: str | int`,
			re:    helmutil.SchemaDefaultRegexp,
			repl:  "$1",
		},
		"many types": {
			input: `    key: str | int | {str:any} = {}`,
			want:  `    key: str | int | {str:any}`,
			re:    helmutil.SchemaDefaultRegexp,
			repl:  "$1",
		},
		"strange default": {
			input: `    key: str = "*/20 * * * *"`,
			want:  `    key: str`,
			re:    helmutil.SchemaDefaultRegexp,
			repl:  "$1",
		},
		"block string default": {
			input: `    default is r"""{{ include "bjw-s.common.lib.chart.names.fullname" $ }}"""`,
			want:  `    default is "{{ include "bjw-s.common.lib.chart.names.fullname" $ }}"`,
			re:    helmutil.SchemaInvalidDocRegexp,
			repl:  `${1}"${2}"${3}`,
		},
		"values": {
			input: `    values?: any`,
			want:  `    values?: x any`,
			re:    helmutil.SchemaValuesRegexp,
			repl:  "${1}x ${2}",
		},
		"values docs": {
			input: `    values : any, foobar`,
			want:  `    values : x any, foobar`,
			re:    helmutil.SchemaValuesRegexp,
			repl:  "${1}x ${2}",
		},
	}
	for name, tc := range tcs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := tc.re.ReplaceAllString(tc.input, tc.repl)
			require.Equalf(t, tc.want, got, "expected %q, got %q", tc.want, got)
		})
	}
}

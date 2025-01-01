package helm_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	"kcl-lang.io/kcl-go/pkg/spec/gpyrpc"
	"kcl-lang.io/lib/go/native"

	argocli "github.com/MacroPower/kclx/pkg/argoutil/cli"
	_ "github.com/MacroPower/kclx/pkg/plugin/helm"
)

var testDataDir string

func init() {
	argocli.SetLogLevel("warn")

	//nolint:dogsled
	_, filename, _, _ := runtime.Caller(0)
	dir := filepath.Dir(filename)
	testDataDir = filepath.Join(dir, "testdata")
}

func TestPluginHelmTemplate(t *testing.T) {
	t.Parallel()

	tcs := map[string]struct {
		kclFile     string
		resultsFile string
	}{
		"Simple": {
			kclFile:     "input/simple.k",
			resultsFile: "output/simple.json",
		},
	}
	for name, tc := range tcs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			inputKCLFile := filepath.Join(testDataDir, tc.kclFile)
			wantResultsFile := filepath.Join(testDataDir, tc.resultsFile)

			inputKCL, err := os.ReadFile(inputKCLFile)
			require.NoError(t, err)

			want, err := os.ReadFile(wantResultsFile)
			require.NoError(t, err)

			client := native.NewNativeServiceClient()
			result, err := client.ExecProgram(&gpyrpc.ExecProgram_Args{
				KFilenameList: []string{"main.k"},
				KCodeList:     []string{string(inputKCL)},
				Args:          []*gpyrpc.Argument{},
			})
			require.NoError(t, err)
			require.Empty(t, result.GetErrMessage())

			got := result.GetJsonResult()

			require.JSONEq(t, string(want), got)
		})
	}
}

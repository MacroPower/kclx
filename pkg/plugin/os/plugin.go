package os

import (
	"fmt"

	"kcl-lang.io/kcl-go/pkg/plugin"

	"github.com/MacroPower/kclipper/pkg/os"
)

func Register() {
	plugin.RegisterPlugin(Plugin)
}

var Plugin = plugin.Plugin{
	Name: "os",
	MethodMap: map[string]plugin.MethodSpec{
		"exec": {
			Body: func(args *plugin.MethodArgs) (*plugin.MethodResult, error) {
				name := args.StrArg(0)
				strArgs := []string{}
				for _, v := range args.ListArg(1) {
					strArgs = append(strArgs, fmt.Sprint(v))
				}
				strEnvs := []string{}
				if _, ok := args.KwArgs["env"]; ok {
					for k, v := range args.MapKwArg("env") {
						strEnvs = append(strEnvs, fmt.Sprintf("%s=%s", k, v))
					}
				}

				exec, err := os.Exec(name, strArgs, strEnvs)
				if err != nil {
					return nil, fmt.Errorf("failed to exec %s: %w", name, err)
				}

				return &plugin.MethodResult{V: map[string]string{
					"stdout": exec.Stdout,
					"stderr": exec.Stderr,
				}}, nil
			},
		},
	},
}

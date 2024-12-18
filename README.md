# kclx

KCL Extended.

[KCL](https://github.com/kcl-lang/kcl) is an open-source, constraint-based record and functional language that enhances the writing of complex configurations, including those for cloud-native scenarios. The KCL website can be found [here](https://kcl-lang.io/).

KCL Extended serves to wrap upstream KCL releases with additional features and [plugins](https://www.kcl-lang.io/docs/next/reference/plugin/overview), and provide up-to-date multi-architecture Docker images for x86 and arm64.

## Installation

Binaries are posted in [releases](https://github.com/MacroPower/kclx/releases).

Images are available [here](https://github.com/MacroPower/kclx/pkgs/container/kclx). e.g. `ghcr.io/macropower/kclx:latest`

The command and binary is still just `kcl`, so that it can be used as a drop-in replacement for official KCL binaries.

Versions are tagged independently of upstream KCL, e.g. kclx `v0.1.0` maps to kcl `v0.10.10`, but kclx releases still follow semver with consideration for upstream KCL changes. e.g., bumping upstream KCL's major version will bump this project's major version as well. I considered using a version strategy like `v0.1.0-kcl0.10.0`, but decided against it for simplicity and compatibility with other tools (like goreleaser, your renovate config, etc.)

To use kclx with ArgoCD, you can follow [this guide](https://www.kcl-lang.io/docs/user_docs/guides/gitops/gitops-quick-start) to set up the KCL ConfigManagementPlugin. You just need to substitute the official kcl image with a kclx image.

## Included Plugins

### Helm

Execute `helm template` and return the resulting Kubernetes resources. This plugin uses ArgoCD's Helm source implementation on the backend, and is very fast once the upstream chart has been cached (<100ms even on my older arm-based system). E.g.:

```py
helm.template(
    chart="example",
    target_revision="0.1.0",
    repo_url="https://example.com/charts",
)
```

You can then use these resources in your KCL code (e.g., via merging in some changes, referencing the resources elsewhere, etc.). The resources are output as a `dict`, with the key being `({group}_)?{kind}_{name}`, lowercase and with special characters replaced with underscores. e.g., `Service/my-app` -> `service_my_app`, `apps/Deployment/my-app` -> `apps_deployment_my_app`.

```py
helm.template(
  chart="example",
  target_revision="0.1.0",
  repo_url="https://example.com/charts",
  valuesObject={
    replicas: 3
  }
) | {
  apps_deployment_example.spec.strategy.type = RollingUpdate
}
```

This plugin is similar to [kcfoil](https://github.com/cakehappens/kcfoil)'s helm plugin. kcfoil's Helm plugin is based on Tanka's Helm implementation, whereas kclx's Helm plugin is based on ArgoCD's Helm source implementation. So they both expose a helm template function, but the exposed parameters and backend implementations are completely different.

The biggest difference:

- Tanka and ergo kcfoil's plugin expect Helm Charts to be found inside the bounds of a project. i.e., you must "vendor" your Charts, or in other words, you must put your Charts somewhere adjacent to your KCL codes so that it can be referred to using a relative path. This has many advantages but may be cumbersome in some cases.
    - e.g., `helm.template("example", "./charts/example")`
- ArgoCD's Helm source implementation, and ergo this plugin as well, allows you to specify a URL to a Helm Chart index, which is useful for fetching Charts from the internet, and it is more heavily optimized for caching fetched results as well. Though, it will likely always be slower versus a vendoring implementation.
    - e.g., `helm.template("example", "0.1.0", "https://example.com/charts")`

Both plugins mirror many aspects of Tanka and ArgoCD respectively, including in their overall style, argument usage, and so on. So, the interfaces will feel familiar to users of either tool. I recommend you choose the one that is more familiar to you, and/or best fits your use case.

### OS

Run a command on the host OS. This can be useful for integrating with other tools that do not have a native KCL plugin available, e.g. by installing them in your container. E.g.:

`os.exec("command", ["arg"])` -> `{"stdout": "x", "stderr": "y"}`

You can parse stdout using one of KCL's native functions e.g. `json.decode` or `yaml.decode`.

## Contributing

[Tasks](https://taskfile.dev) are available (run `task help`).

If you are using an arm64 Mac, you can use [Devbox](https://www.jetify.com/docs/devbox/) to create a Nix environment pre-configured with all the necessary tools and dependencies for Go, Zig, etc. Otherwise, you can still use the included Devbox, but CGO probably won't work.

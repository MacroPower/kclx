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
) # -> [{"group": "apps", "kind": "Deployment", ...}, ...]
```

You can then use these resources in your KCL code (e.g., via merging in some changes, referencing the resources elsewhere, etc.). You can also very flexibly patch the templated resources with a lambda function. E.g.:

```py
import regex
import kcl_plugin.helm

_chart = helm.template(
  chart="example",
  target_revision="0.1.0",
  repo_url="https://example.com/charts",
  values={
    replicas: 3
  }
)

patch = lambda resource: {str:} -> {str:} {
  if resource.kind == "Deployment":
    resource.spec.strategy.type = RollingUpdate

  if regex.match(resource.metadata.name, "^example-.*$"):
    resource.metadata.annotations: {
      "example.com/added" = "by kcl patch"
    }

  resource
}

{"resources": [patch(r) for r in _chart]}
```

I have not been able to get native support for the `kcl_plugin` import in the kcl-language-server, but it is still possible to get very good integration in your editor of choice, just with a bit of extra work. For example, completion, validation, and docs (e.g. on hover) for plugin parameters. To accomplish this, I have created a KCL module which wraps the kclx helm plugin. Using this is completely optional, but is a significant quality of life improvement.

```py
import helm

helm.template(helm.Chart {
  chart = "example"
  targetRevision = "0.1.0"
  repoURL = "https://example.com/charts"
  values = {
    replicas: 3
  }
})
```

To install the module, run the following to add it to your kcl.mod file:

```sh
kcl mod add oci://ghcr.io/macropower/kclx/helm
```

:warning: This must be completed AFTER installing kclx. Just adding the helm module will not provide you with the underlying plugin, and you will get an error when you call the template function.

You can also generate a schema for the dict used in the `values` argument. If you do this, you should automate it in some way, so that you can keep the values schema up-to-date with the chart.

```bash
# Install the `helm schema` plugin.
# This is a fork which has been modified to work better with `kcl import`.
helm plugin install https://github.com/MacroPower/helm-values-schema-json.git

# Add a configuration file for `helm schema`.
# Specify one or more URLs pointing to example values.yaml files for the chart.
cat <<EOF > .schema.yaml
input:
  - https://example.com/charts/example/values.yaml
schemaRoot:
  title: Values
schema:
  additionalProperties: true
EOF

# Generate the schema.
helm schema
kcl import -m jsonschema values.schema.json --force
rm values.schema.json
```

This will generate a file `values.schema.k` with a root schema called `Values`. You can use this schema in your KCL code to get completion and validation for any data passed to the `values` argument.

```py
import helm

helm.template(helm.Chart {
  chart = "example"
  targetRevision = "0.1.0"
  repoURL = "https://example.com/charts"
  values = Values { # <- Uses the Values schema from values.schema.k
    replicas: 3
  }
})
```

This won't be perfect, since `helm schema` is just doing its best to infer the schema from the union of all inputs. Using `additionalProperties: true` will allow you to drift from the schema somewhat by allowing extra fields to be added to any schema. This is useful if some input you need to use was not present in the example values.yaml file, and thus was not added to the schema.

If you happen to find a chart that does actually have a full values.schema.json file (or uses a common library which has one, like [bjw-s/common](https://github.com/bjw-s/helm-charts)), you can just pass that directly to `kcl import` rather than generating it with `helm schema`. This will produce much better results, but unfortunately I have found that not many charts include a jsonschema.

#### Comparisons

**To the [Helm KCL Plugin](https://github.com/kcl-lang/helm-kcl):**

Don't let the names fool you. The [helm-kcl](https://github.com/kcl-lang/helm-kcl) plugin is a plugin for Helm, allowing you to use `KCLRun` resources in your Helm Charts. This kclx Helm plugin is a plugin for KCL, allowing you to template Helm Charts in your KCL code. i.e., they integrate in inverse directions.

**To the [KCFoil Helm Plugin](https://github.com/cakehappens/kcfoil):**

This plugin is similar to [kcfoil](https://github.com/cakehappens/kcfoil)'s helm plugin. kcfoil's Helm plugin is based on Tanka's Helm implementation, whereas kclx's Helm plugin is based on ArgoCD's Helm source implementation. So they both expose a helm template function, but the exposed parameters and backend implementations are completely different.

The biggest difference:

- Tanka and ergo kcfoil's plugin expect Helm Charts to be found inside the bounds of a project. i.e., you must "vendor" your Charts, or in other words, you must put your Charts somewhere adjacent to your KCL codes so that it can be referred to using a relative path. This has many advantages but may be cumbersome in some cases.
    - e.g., `helm.template("example", "./charts/example")`
- ArgoCD's Helm source implementation, and ergo this plugin as well, allows you to specify a URL to a Helm Chart index, which is useful for fetching Charts from the internet, and it is more heavily optimized for caching fetched results as well. Though, it will likely always be slower versus a vendoring implementation.
    - e.g., `helm.template("example", "0.1.0", "https://example.com/charts")`

Both plugins mirror many aspects of Tanka and ArgoCD respectively, including in their overall style, argument usage, and so on. So, the interfaces will feel familiar to users of either tool. I recommend you choose the one that is more familiar to you, and/or best fits your use case.

### HTTP

Includes the HTTP plugin from [kcl-lang/kcl-plugin](https://github.com/kcl-lang/kcl-plugin), which can be used to GET external resources. E.g.:

`http.get("https://example.com")` -> `{"body": "<...>", "status": 200}`

You can parse the body using one of KCL's native functions e.g. `json.decode` or `yaml.decode`.

### OS

Run a command on the host OS. This can be useful for integrating with other tools that do not have a native KCL plugin available, e.g. by installing them in your container. E.g.:

`os.exec("command", ["arg"])` -> `{"stdout": "x", "stderr": "y"}`

You can parse stdout using one of KCL's native functions e.g. `json.decode` or `yaml.decode`.

## Contributing

[Tasks](https://taskfile.dev) are available (run `task help`).

If you are using an arm64 Mac, you can use [Devbox](https://www.jetify.com/docs/devbox/) to create a Nix environment pre-configured with all the necessary tools and dependencies for Go, Zig, etc. Otherwise, you can still use the included Devbox, but CGO probably won't work.

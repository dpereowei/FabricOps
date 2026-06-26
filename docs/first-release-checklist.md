# First Release Checklist

Use this checklist before publishing a FabricOps release tag or pointing users at release install commands.

## Prepare

- Confirm the worktree is clean.
- Choose the SemVer release version, for example `0.1.0`.
- Confirm `SUPPORTED_FEATURES.md` reflects the current operator behavior.
- Confirm `README.md` install commands use the intended release version.

## Build And Publish Images

```bash
make docker-buildx-release VERSION=0.1.0

config/samples/chaincodes/node_settlement/build_and_push.sh
config/samples/chaincodes/go_settlement/build_and_push.sh
config/samples/chaincodes/java_settlement/build_and_push.sh
```

`docker-buildx-release` is the preferred manager image release path because it publishes all configured `PLATFORMS`. Use `docker-build-release` and `docker-push-release` only for a local single-platform sanity build when needed.

The published release image names are:

- `ghcr.io/dpereowei/fabricops:<version>`
- `ghcr.io/dpereowei/fabricops-node-settlement:<version>`
- `ghcr.io/dpereowei/fabricops-go-settlement:<version>`
- `ghcr.io/dpereowei/fabricops-java-settlement:<version>`

## Verify Public GHCR Visibility

Run the unauthenticated registry check after pushing images:

```bash
make release-check-ghcr VERSION=0.1.0
```

This check asks GHCR for anonymous pull tokens and then reads image manifests without Docker credentials. It should pass for the manager image and all sample chaincode images before release docs, bundles, or charts reference those tags.

If a newly published GHCR package is still private, open the package settings on GitHub and change its visibility to public, then rerun the check. GitHub documents the package visibility flow in [Configuring a package's access control and visibility](https://docs.github.com/en/packages/learn-github-packages/configuring-a-packages-access-control-and-visibility): personal-account packages are private on first publish, and public container packages allow anonymous pulls.

## Generate Release Artifacts

```bash
make build-installer-release VERSION=0.1.0
helm lint dist/chart
helm template fabricops dist/chart --namespace fabricops-system >/tmp/fabricops-chart.yaml
```

Confirm the generated bundle uses the public manager image:

```bash
grep 'image: ghcr.io/dpereowei/fabricops:0.1.0' dist/install.yaml
```

Commit the generated `dist/install.yaml` changes for the release tag.

## Fresh Cluster Proof

Validate both distribution paths on clean kind clusters:

```bash
kind create cluster --name fabricops-release-bundle
kubectl apply -f dist/install.yaml
kubectl rollout status deployment/fabricops-controller-manager -n fabricops-system --timeout=120s
kubectl apply -k config/samples
kubectl wait fabricnetwork/fabricnetwork-sample -n default --for=condition=Ready --timeout=20m
config/samples/chaincodes/node_settlement/invoke_smoke.sh
```

```bash
kind create cluster --name fabricops-release-helm
make helm-deploy-release VERSION=0.1.0
kubectl apply -k config/samples
kubectl wait fabricnetwork/fabricnetwork-sample -n default --for=condition=Ready --timeout=20m
config/samples/chaincodes/node_settlement/invoke_smoke.sh
```

## Publish

- Create and push the release tag.
- Create the GitHub release.
- Re-run `make release-check-ghcr VERSION=<version>` after the release is visible.

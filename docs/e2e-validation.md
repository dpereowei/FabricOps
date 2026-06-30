# E2E Validation

FabricOps e2e validation provisions a real kind cluster, installs the in-cluster manager, applies the sample `FabricNetwork`, waits for `Ready=True`, and invokes the Node settlement CCaaS chaincode.

## Kind

Run the full local e2e proof from the repository root:

```bash
make test-e2e
```

The default cluster name is `fabricops-test-e2e`. Override it with:

```bash
make test-e2e KIND_CLUSTER=fabricops-e2e
```

By default, the target deletes the kind cluster after the test run. Keep the cluster for inspection with:

```bash
make test-e2e E2E_SKIP_CLEANUP=true
```

When the test completes, it has validated:

- manager image build and kind image load
- generated install bundle deployment
- sample `FabricNetwork` reconciliation to `Ready=True`
- channel bootstrap, chaincode lifecycle, and CCaaS workload readiness
- committed Node settlement chaincode invokes plus queries through both sample peers

Clean up a retained e2e cluster with:

```bash
make cleanup-test-e2e KIND_CLUSTER=fabricops-e2e
```

## OrbStack

For fast local smoke testing against OrbStack:

```bash
kubectl config use-context orbstack
make docker-build IMG=controller:latest
make build-installer IMG=controller:latest
kubectl apply -f dist/install.yaml
kubectl rollout status deployment/fabricops-controller-manager -n fabricops-system --timeout=120s
kubectl apply -k config/samples
kubectl wait fabricnetwork/fabricnetwork-sample -n default --for=condition=Ready --timeout=20m
config/samples/chaincodes/node_settlement/invoke_smoke.sh
```

Remove the sample resources from the current cluster with:

```bash
make cleanup-sample
```

## GitHub Actions

The repository has two kind-backed CI smokes:

- `.github/workflows/test-e2e.yml` runs `make test-e2e` against the generated install bundle.
- `.github/workflows/test-chart.yml` installs the Helm chart, applies the sample network, waits for `Ready=True`, and runs the Node settlement invoke smoke.

The unit/envtest workflow also runs `go mod tidy` and generated-file drift checks before the repository is ready to release.

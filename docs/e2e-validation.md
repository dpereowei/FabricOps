# E2E Validation

FabricOps e2e validation provisions a real kind cluster, installs the in-cluster manager, applies the sample `FabricNetwork`, waits for `Ready=True`, and invokes the sample settlement CCaaS chaincode across Node, Go, and Java runtimes.

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
- local Node, Go, and Java settlement chaincode image builds and kind image loads
- generated install bundle deployment
- sample `FabricNetwork` reconciliation to `Ready=True`
- channel bootstrap, chaincode lifecycle, and CCaaS workload readiness
- committed Node settlement chaincode invokes plus queries through BankA and BankB endorsement sets
- private data collection lifecycle wiring, transient private write, authorized private read, non-member private read rejection, and non-member private hash query
- declarative sequence upgrades from the Node image to the Go image and then the Java image
- committed Go and Java settlement chaincode invokes plus queries through BankA and BankB endorsement sets

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
make build-fabricopsctl
bin/fabricopsctl wait -n default --timeout 20m fabricnetwork-sample
bin/fabricopsctl invoke -n default --org BankA --peer BankA/peer0 --peer BankB/peer0 \
  --channel settlement --chaincode settlement --function createSettlement \
  --args '["orbstack-001","alice","bob","100","USD"]' fabricnetwork-sample
bin/fabricopsctl query -n default --org BankA --peer BankA/peer0 \
  --channel settlement --chaincode settlement --function readSettlement \
  --args '["orbstack-001"]' fabricnetwork-sample
```

Enable the private-data smoke after loading a Node settlement image that includes the private settlement transactions:

```bash
PRIVATE_SMOKE_ENABLED=true config/samples/chaincodes/node_settlement/invoke_smoke.sh
```

Remove the sample resources from the current cluster with:

```bash
make cleanup-sample
```

## GitHub Actions

The repository has two kind-backed CI smokes:

- `.github/workflows/test-e2e.yml` runs `make test-e2e` against the generated install bundle and the Node, Go, and Java sample chaincodes.
- `.github/workflows/test-chart.yml` installs the Helm chart, applies the sample network, waits for `Ready=True` with `fabricopsctl`, and invokes/queries the Node settlement chaincode with `fabricopsctl`.

The unit/envtest workflow also runs `go mod tidy` and generated-file drift checks before the repository is ready to release.

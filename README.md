[![CII Best Practices](https://bestpractices.coreinfrastructure.org/projects/13370/badge)](https://bestpractices.coreinfrastructure.org/projects/13370) [![Go Report Card](https://goreportcard.com/badge/github.com/dpereowei/fabricops)](https://goreportcard.com/report/github.com/dpereowei/fabricops) ![GitHub release (latest SemVer)](https://img.shields.io/github/v/release/dpereowei/fabricops?sort=semver)

![FabricOps](./fabricops-lockup-ink.png#gh-light-mode-only)
![FabricOps](./logo.png#gh-dark-mode-only)

FabricOps is a Kubernetes operator for provisioning multi-organization Hyperledger Fabric networks from declarative configuration.

The long-term goal is automated Fabric infrastructure on Kubernetes: Terraform for cluster/cloud infrastructure, a custom operator for Fabric CAs, orderers, peers, channels, and Chaincode-as-a-Service, plus TLS certificate lifecycle management and Prometheus-based health visibility.

## Installation

FabricOps installs into an existing Kubernetes cluster. End users only need `kubectl`; Helm is optional.

Requirements:

- Kubernetes cluster, such as EKS, kind, minikube, or OrbStack
- `kubectl` configured for the target cluster
- Default storage class for the sample network's persistent volumes
- Helm 3 or later, only if installing with Helm

### Install From Release Bundle

Install the latest published release bundle:

```bash
kubectl apply -f https://github.com/dpereowei/fabricops/releases/download/v0.1.0/install.yaml
kubectl rollout status deployment/fabricops-controller-manager -n fabricops-system --timeout=120s
```

The bundle installs the `FabricNetwork` CRD, RBAC, ServiceAccount, manager Deployment, and metrics Service. The manager image is pinned to the release tag:

```text
ghcr.io/dpereowei/fabricops:0.1.0
```

### Install With Helm

Install the release chart directly from the GitHub release:

```bash
helm upgrade --install fabricops \
  https://github.com/dpereowei/fabricops/releases/download/v0.1.0/fabricops-0.1.0.tgz \
  --namespace fabricops-system \
  --create-namespace \
  --wait
```

The chart installs CRDs, RBAC, the manager Deployment, and the metrics Service. By default it uses `ghcr.io/dpereowei/fabricops:<chart-appVersion>`.

Override the manager image if needed:

```bash
helm upgrade --install fabricops \
  https://github.com/dpereowei/fabricops/releases/download/v0.1.0/fabricops-0.1.0.tgz \
  --namespace fabricops-system \
  --create-namespace \
  --set manager.image.repository=ghcr.io/dpereowei/fabricops \
  --set manager.image.tag=0.1.0 \
  --wait
```

### Render Then Apply

If you want to review the Kubernetes objects before applying them:

```bash
helm template fabricops \
  https://github.com/dpereowei/fabricops/releases/download/v0.1.0/fabricops-0.1.0.tgz \
  --namespace fabricops-system > fabricops-install.yaml

kubectl apply -f fabricops-install.yaml
kubectl rollout status deployment/fabricops-controller-manager -n fabricops-system --timeout=120s
```

### Create A Sample Network

After installing the operator, apply the sample `FabricNetwork`:

```bash
kubectl apply -f https://raw.githubusercontent.com/dpereowei/fabricops/v0.1.0/config/samples/fabricops_v1alpha1_fabricnetwork.yaml
kubectl wait fabricnetwork/fabricnetwork-sample -n default --for=condition=Ready --timeout=20m
```

Inspect the generated network:

```bash
kubectl get fabricnetwork fabricnetwork-sample -n default
kubectl get pods -n fo-sample-orderer
kubectl get pods -n fo-sample-banka
kubectl get pods -n fo-sample-bankb
```

FabricOps generates an in-cluster client connection profile for each peer org. For the sample BankA org:

```bash
kubectl get configmap fabricnetwork-sample-connection-profile -n fo-sample-banka -o jsonpath='{.data.connection\.yaml}'
kubectl get fabricnetwork fabricnetwork-sample -n default -o jsonpath='{.status.orgStatus[?(@.name=="BankA")].connectionProfileConfigMapName}'
kubectl get fabricnetwork fabricnetwork-sample -n default -o jsonpath='{.status.orgStatus[?(@.name=="BankA")].peerEndpoints}'
```

When building from source, `fabricopsctl` wraps the same status and profile lookup:

```bash
make build-fabricopsctl
bin/fabricopsctl status fabricnetwork-sample -n default
bin/fabricopsctl connection-profile fabricnetwork-sample -n default --org BankA --format yaml
```

### Uninstall

Delete `FabricNetwork` resources before removing the operator so FabricOps finalizers can clean up generated org namespaces:

```bash
kubectl delete fabricnetwork fabricnetwork-sample -n default --ignore-not-found
kubectl delete -f https://github.com/dpereowei/fabricops/releases/download/v0.1.0/install.yaml
```

For Helm installs:

```bash
kubectl delete fabricnetwork fabricnetwork-sample -n default --ignore-not-found
helm uninstall fabricops -n fabricops-system
```

## Release Artifacts

Release `v0.1.0` publishes:

- `install.yaml`: single-file Kubernetes install bundle
- `fabricops-0.1.0.tgz`: Helm chart archive
- `ghcr.io/dpereowei/fabricops:0.1.0`: multi-platform manager image
- `ghcr.io/dpereowei/fabricops-node-settlement:0.1.0`: Node CCaaS sample
- `ghcr.io/dpereowei/fabricops-go-settlement:0.1.0`: Go CCaaS sample
- `ghcr.io/dpereowei/fabricops-java-settlement:0.1.0`: Java CCaaS sample

## Capabilities

FabricOps supports:

- A namespaced `FabricNetwork` CRD at `fabricops.io/v1alpha1`
- Per-org Kubernetes namespaces with compact network-scoped names
- Fabric CA, orderer, peer, and CCaaS chaincode workloads
- Fabric CA registrar bootstrap, admin enrollment, and workload enrollment Secrets
- Fabric CA-backed MSP/TLS material for admins, orderers, and peers
- Persistent storage and resource defaults for Fabric workloads
- Declarative channel config generation, channel block generation, orderer joins, peer joins, and anchor peer updates
- CCaaS package metadata generation, install, approve, commit, and chaincode server workloads
- Per-peer-org client connection profile ConfigMaps for in-cluster Gateway/application clients
- Endpoint discovery in status for Fabric CAs, peers, orderers, operations Services, and peer chaincode Services
- `fabricopsctl` helper commands for status and connection profile lookup when built from source
- Kubernetes status conditions for component, identity, channel, chaincode, and observability readiness
- Fabric peer/orderer operations endpoints and optional Prometheus Operator `ServiceMonitor` resources
- Optional org-boundary NetworkPolicies for FabricOps-managed pods
- Finalizer-based cleanup for generated org namespaces

See [SUPPORTED_FEATURES.md](SUPPORTED_FEATURES.md) for the detailed
compatibility matrix.

## Namespace Layout

Org namespaces use this convention:

```text
fo-<network>-<org>
```

For example, the sample `FabricNetwork` named `fabricnetwork-sample` creates:

```text
fo-sample-orderer
fo-sample-banka
```

If the `FabricNetwork` lives outside the `default` namespace, the control namespace is included to avoid cluster-wide namespace collisions:

```text
fo-<control-namespace>-<network>-<org>
```

## For Contributors

Contributor requirements:

- Go >= 1.23
- Kubebuilder >= 4.15.0
- Docker with buildx
- Helm 3 or later
- kind, OrbStack, minikube, or another Kubernetes cluster

### Local Controller Run

Install the CRD, run the controller locally, and apply the sample network:

```bash
make install
make run
kubectl apply -k config/samples
```

In another terminal, inspect the reconciled network:

```bash
kubectl get fabricnetwork fabricnetwork-sample -n default
kubectl get pods -n fo-sample-orderer
kubectl get pods -n fo-sample-banka
```

### Local In-Cluster Bundle

Build the manager image, render the install bundle, deploy the operator, and apply the sample network:

```bash
make docker-build IMG=controller:latest
make build-installer IMG=controller:latest
kubectl apply -f dist/install.yaml
kubectl apply -k config/samples
```

The generated manager Deployment uses `imagePullPolicy: IfNotPresent` so local development clusters such as OrbStack can use the locally built `controller:latest` image. For kind, load the image into the target cluster before applying the bundle:

```bash
kind load docker-image controller:latest --name <cluster-name>
```

Inspect the installed manager and sample:

```bash
kubectl get deploy -n fabricops-system fabricops-controller-manager
kubectl get fabricnetwork fabricnetwork-sample -n default
kubectl get pods -n fo-sample-orderer
kubectl get pods -n fo-sample-banka
```

For a clean kind-cluster demo:

```bash
kind create cluster --name fabricops-packaging
kind load docker-image controller:latest --name fabricops-packaging
kubectl apply -f dist/install.yaml
kubectl rollout status deployment/fabricops-controller-manager -n fabricops-system --timeout=120s
kubectl apply -k config/samples
kubectl wait fabricnetwork/fabricnetwork-sample -n default --for=condition=Ready --timeout=20m
config/samples/chaincodes/node_settlement/invoke_smoke.sh
```

### Local Helm Install

Use the generated chart in `dist/chart` for local chart development:

```bash
make docker-build IMG=controller:latest
kind load docker-image controller:latest --name <cluster-name>
make helm-deploy IMG=controller:latest
make helm-status
kubectl apply -k config/samples
kubectl wait fabricnetwork/fabricnetwork-sample -n default --for=condition=Ready --timeout=20m
```

Override local Helm settings with `HELM_RELEASE`, `HELM_NAMESPACE`, `HELM_CHART_DIR`, and `HELM_EXTRA_ARGS`.

### Manager Images

Local development uses `controller:latest` so OrbStack and kind can run the manager image without a registry. Published manager images should use immutable SemVer tags at:

```text
ghcr.io/dpereowei/fabricops:<version>
```

For example:

```bash
make docker-buildx-release VERSION=0.1.0
make build-installer-release VERSION=0.1.0
```

`docker-buildx-release` publishes the manager image for all configured `PLATFORMS`. For local single-platform sanity checks, use `docker-build-release` and `docker-push-release`.

Use that same tag for Helm installs:

```bash
make helm-deploy-release VERSION=0.1.0
```

The release helpers derive `RELEASE_IMG` from `IMAGE_REGISTRY`, `IMAGE_REPOSITORY`, and `VERSION`. Override those variables if the image moves to another registry or repository.

Before publishing release instructions, verify that the manager image and sample chaincode images are publicly pullable from GHCR:

```bash
make release-check-ghcr VERSION=0.1.0
```

See [docs/first-release-checklist.md](docs/first-release-checklist.md) for the full release checklist.

### Terraform Examples

Local development infrastructure examples live under [examples/terraform](examples/terraform). The first example provisions a single-node kind cluster for FabricOps demos:

```bash
make docker-build IMG=controller:latest
cd examples/terraform/local-kind
terraform init
terraform apply
```

### E2E Validation

Run the repeatable kind-based e2e proof with:

```bash
make test-e2e
```

The e2e target builds the local manager and Node settlement chaincode images, loads them into kind, installs the generated bundle, applies the sample network, waits for `Ready=True`, and runs the Node settlement invoke/private-data smoke. See [docs/e2e-validation.md](docs/e2e-validation.md) for kind, OrbStack, and cleanup notes.

## Identity Secrets

FabricOps uses Fabric CA enrollment as the identity material path. The deterministic Secret contract is:

```text
<org>-ca-bootstrap: username, password, user-pass
<org>-admin-enrollment: username, password, user-pass
<workload>-enrollment: username, password, user-pass
<workload>-msp: config.yaml, cacert.pem, tlscacert.pem, signcert.pem, keystore.pem
<workload>-tls: ca.crt, server.crt, server.key
<org>-admin-msp: config.yaml, cacert.pem, tlscacert.pem, signcert.pem, keystore.pem
<org>-admin-tls: ca.crt, client.crt, client.key
```

Fabric CA pods receive `<org>-ca-bootstrap/user-pass` through `FABRIC_CA_SERVER_BOOTSTRAP_USER_PASS`. Admin enrollment Jobs use `<org>-admin-enrollment` to register and enroll the org admin identity, then publish enrolled MSP/TLS material to `<org>-admin-msp` and `<org>-admin-tls`. Workload enrollment Jobs use `<workload>-enrollment` to register and enroll orderer and peer identities, then publish enrolled material to `<workload>-msp` and `<workload>-tls`.

Orderers mount identity Secrets at `/var/hyperledger/orderer/msp` and `/var/hyperledger/orderer/tls`. Peers mount them at `/etc/hyperledger/fabric/peer/msp` and `/etc/hyperledger/fabric/peer/tls`.

## Storage

Persistent data is configured through `spec.global.storage.ca`, `spec.global.storage.orderer`, and `spec.global.storage.peer`. Each component accepts a `size` and optional `storageClassName`.

Default sizes are:

- CA: `1Gi`
- Orderer: `5Gi`
- Peer: `10Gi`

CA pods mount persistent data at `/etc/hyperledger/fabric-ca-server`. Orderer and peer pods mount persistent data at `/var/hyperledger/production`.

Fabric component instances run as singleton Deployments with `Recreate` rollout strategy and one PVC per instance.

## Observability

Peer and orderer workloads expose Fabric operations endpoints through separate `*-operations` Services. Prometheus metrics are enabled on those operations endpoints.

Prometheus Operator `ServiceMonitor` output is opt-in:

```yaml
spec:
  global:
    observability:
      serviceMonitor:
        enabled: true
        interval: 30s
        scrapeTimeout: 10s
        labels:
          release: prometheus
```

The `monitoring.coreos.com/v1` ServiceMonitor CRD must already be installed before enabling ServiceMonitor output.

## Network Policy

Org-boundary NetworkPolicies are opt-in:

```yaml
spec:
  global:
    networkPolicy:
      enabled: true
```

When enabled, FabricOps creates one `fabricops-org-boundary` NetworkPolicy per generated org namespace. The policy selects FabricOps-managed pods for that org, allows ingress and egress among pods in namespaces that belong to the same `FabricNetwork`, and keeps DNS plus Kubernetes API egress open for helper Jobs. When disabled, FabricOps removes its owned `fabricops-org-boundary` policies and leaves unowned same-name policies untouched.

Your cluster CNI must support Kubernetes NetworkPolicy enforcement. If Prometheus or other operational tools run outside the generated Fabric namespaces, add a separate allow policy for those clients.

## Cleanup

FabricOps adds a finalizer to each `FabricNetwork`. When a `FabricNetwork` is deleted, the controller deletes generated org namespaces after confirming their FabricOps ownership labels.

## Example Resource

```yaml
apiVersion: fabricops.io/v1alpha1
kind: FabricNetwork
metadata:
  name: fabricnetwork-sample
spec:
  global:
    fabricVersion: "3.1.0"
    tls: true
    networkPolicy:
      enabled: false
    storage:
      ca:
        size: 1Gi
      orderer:
        size: 5Gi
      peer:
        size: 10Gi
  orgs:
    - organization:
        name: Orderer
        domain: orderer.example.com
        mspName: OrdererMSP
      ca:
        db: sqlite
      orderers:
        - groupName: group1
          type: raft
          instances: 2
          prefix: orderer
    - organization:
        name: BankA
        domain: banka.example.com
        mspName: BankAMSP
      ca:
        db: sqlite
      peer:
        instances: 2
        db: LevelDB
        prefix: peer
  channels:
    - name: settlement
      orgs:
        - name: BankA
          peers: ["peer0", "peer1"]
  chaincodes:
    - name: settlement
      version: "0.0.1"
      channel: settlement
      image: ghcr.io/dpereowei/fabricops-node-settlement:0.1.0
      sequence: 1
      ccaas:
        replicas: 1
        containerPort: 7052
        servicePort: 7052
        dialTimeout: 10s
        imagePullPolicy: IfNotPresent
```

## References

FabricOps uses [hyperledger-labs/fablo](https://github.com/hyperledger-labs/fablo) as an implementation reference for Fabric network bootstrapping behavior, especially around generated config, Docker Compose topology, CA enrollment flow, peer/orderer configuration, and CCaaS support.

## License

This project is licensed under the Apache License 2.0. See the LICENSE file for details.

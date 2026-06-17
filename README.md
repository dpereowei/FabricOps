## FabricOps

FabricOps is a Kubernetes operator for provisioning multi-organization Hyperledger Fabric networks from declarative configuration.

The long-term goal is automated Fabric infrastructure on Kubernetes: Terraform for cluster/cloud infrastructure, a custom operator for Fabric CAs, orderers, peers, channels, and Chaincode-as-a-Service, plus TLS certificate lifecycle management and Prometheus-based health visibility.

### Requirements

- Go >= 1.23
- Kubebuilder >= 4.15.0
- Kubernetes cluster, such as OrbStack, kind, minikube, or EKS

### Current Status

The project is in an early operator milestone. Today it can:

- Define a `FabricNetwork` CRD at `fabricops.my.domain/v1alpha1`
- Reconcile a `FabricNetwork` with `global`, `orgs`, `channels`, and `chaincodes` config
- Create one Kubernetes namespace per Fabric org
- Create Fabric CA, orderer, and peer Deployments and Services
- Report per-org namespace and CA readiness in `.status.orgStatus`

Org namespaces use a compact network-scoped convention:

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

### Smoke Test

Against an OrbStack Kubernetes cluster, the current sample was installed and reconciled with:

```bash
make install
make run
kubectl apply -k config/samples
```

The operator successfully created separate org namespaces and placed each org's resources in its own namespace. The Fabric CA pods became healthy.

Known limitation: `.status.phase=Ready` currently means all org CAs are ready, not that orderers and peers are ready. The sample orderer and peer pods are expected to fail at this stage because real Fabric TLS/MSP material and peer/orderer runtime configuration are not implemented yet.

Observed gaps from the smoke test:

- Orderers require TLS for the current ordering-node configuration
- Peers need Kubernetes-safe chaincode listen/address configuration
- Network readiness must include orderer and peer readiness, not just CA readiness
- MSP/TLS secret generation and mounting is the next major milestone

### Example Resource

```yaml
apiVersion: fabricops.my.domain/v1alpha1
kind: FabricNetwork
metadata:
  name: fabricnetwork-sample
spec:
  global:
    fabricVersion: "3.1.0"
    tls: true
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
        db: postgres
      peer:
        instances: 2
        db: CouchDb
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
      image: settlement-engine:latest
```

### References

FabricOps will use [hyperledger-labs/fablo](https://github.com/hyperledger-labs/fablo) as an implementation reference for Fabric network bootstrapping behavior, especially around generated config, Docker Compose topology, CA enrollment flow, peer/orderer configuration, and CCaaS support.

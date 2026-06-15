Requirements

golang >= v1.23
kubebuilder >= v4.15.0
## FabricOps (WIP)

FabricOps is a Kubernetes operator for provisioning and managing Hyperledger Fabric infrastructure using a declarative CRD-based model.

### Current Milestone

The project currently implements a minimal but functional controller:

- Defines a `FabricNetwork` Custom Resource Definition (CRD)
- Runs a Kubernetes controller using Kubebuilder
- Watches `FabricNetwork` resources
- Automatically provisions a Kubernetes Namespace per FabricNetwork instance

### Example Behavior

Creating a resource:

```yaml
apiVersion: fabricops.my.domain/v1alpha1
kind: FabricNetwork
metadata:
  name: fabricnetwork-sample
spec:
  orgName: org1
  domain: example.com
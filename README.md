## FabricOps (WIP)

FabricOps is a Kubernetes operator for provisioning Hyperledger Fabric infrastructure using a CRD-driven model.

### Requirements

- Go >= 1.23
- Kubebuilder >= 4.15.0
- Kubernetes cluster (kind/minikube/EKS/etc)

---

### Current Milestone

This stage implements a minimal working controller:

- Defines a `FabricNetwork` Custom Resource Definition (CRD)
- Runs a Kubebuilder-based controller using controller-runtime
- Watches `FabricNetwork` resources
- Creates a dedicated Kubernetes Namespace per `FabricNetwork` instance
- Updates CR status to reflect reconciliation progress

---

### What actually happens today

When a `FabricNetwork` is created:

1. Controller receives the event
2. It computes a target namespace:  
   `fabric-network-<name>`
3. It creates the namespace if it does not exist
4. It updates `.status.phase` and `.status.message`

---

### Example Resource

```yaml
apiVersion: fabricops.my.domain/v1alpha1
kind: FabricNetwork
metadata:
  name: fabricnetwork-sample
spec: {}
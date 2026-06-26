# Terraform Examples

These examples are small development entry points for the infrastructure side of FabricOps. They are intentionally separate from the operator reconciliation logic.

## Local Kind Cluster

`local-kind` creates a single-node kind cluster and can optionally load a locally built FabricOps manager image into it.

```bash
make docker-build IMG=controller:latest
cd examples/terraform/local-kind
terraform init
terraform apply
```

After the cluster exists, install FabricOps from the repository root:

```bash
make docker-build IMG=controller:latest
kind load docker-image controller:latest --name fabricops-dev
kubectl apply -f dist/install.yaml
kubectl apply -k config/samples
```

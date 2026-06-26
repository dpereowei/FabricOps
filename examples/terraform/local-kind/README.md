# Local Kind Terraform Example

This example provisions a single-node kind cluster for local FabricOps demos. It uses Terraform's built-in `terraform_data` resource with `local-exec`, so it does not need a third-party Terraform provider.

## Requirements

- Terraform >= 1.4
- Docker
- kind
- kubectl

## Usage

```bash
terraform init
terraform apply
```

By default, the cluster is named `fabricops-dev` and Terraform loads `controller:latest` into it. Build that image from the repository root before applying:

```bash
make docker-build IMG=controller:latest
```

To skip image loading:

```bash
terraform apply -var='load_manager_image=false'
```

After the cluster is ready, return to the repository root and install FabricOps:

```bash
kubectl config use-context kind-fabricops-dev
make build-installer IMG=controller:latest
kubectl apply -f dist/install.yaml
kubectl apply -k config/samples
kubectl wait fabricnetwork/fabricnetwork-sample -n default --for=condition=Ready --timeout=20m
```

Destroy the cluster with:

```bash
terraform destroy
```

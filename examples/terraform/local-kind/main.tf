terraform {
  required_version = ">= 1.4.0"
}

resource "terraform_data" "cluster" {
  input = {
    cluster_name = var.cluster_name
    kind_config  = abspath("${path.module}/kind-config.yaml")
  }

  provisioner "local-exec" {
    interpreter = ["/usr/bin/env", "bash", "-ec"]
    command     = <<-EOT
      if kind get clusters | grep -qx "${self.input.cluster_name}"; then
        echo "kind cluster '${self.input.cluster_name}' already exists"
      else
        kind create cluster --name "${self.input.cluster_name}" --config "${self.input.kind_config}"
      fi
    EOT
  }

  provisioner "local-exec" {
    when        = destroy
    interpreter = ["/usr/bin/env", "bash", "-ec"]
    command     = <<-EOT
      if kind get clusters | grep -qx "${self.input.cluster_name}"; then
        kind delete cluster --name "${self.input.cluster_name}"
      else
        echo "kind cluster '${self.input.cluster_name}' is already gone"
      fi
    EOT
  }
}

resource "terraform_data" "manager_image" {
  count = var.load_manager_image ? 1 : 0

  input = {
    cluster_name  = terraform_data.cluster.input.cluster_name
    manager_image = var.manager_image
  }

  provisioner "local-exec" {
    interpreter = ["/usr/bin/env", "bash", "-ec"]
    command     = <<-EOT
      docker image inspect "${self.input.manager_image}" >/dev/null
      kind load docker-image "${self.input.manager_image}" --name "${self.input.cluster_name}"
    EOT
  }

  depends_on = [terraform_data.cluster]
}

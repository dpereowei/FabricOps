output "cluster_name" {
  description = "The kind cluster name."
  value       = terraform_data.cluster.input.cluster_name
}

output "kubectl_context" {
  description = "The kubectl context created by kind."
  value       = "kind-${terraform_data.cluster.input.cluster_name}"
}

output "manager_image_loaded" {
  description = "Whether Terraform attempted to load the local manager image into kind."
  value       = var.load_manager_image
}

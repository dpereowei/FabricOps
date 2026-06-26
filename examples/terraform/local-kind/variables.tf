variable "cluster_name" {
  description = "Name of the local kind cluster."
  type        = string
  default     = "fabricops-dev"
}

variable "manager_image" {
  description = "Local FabricOps manager image to load into the kind cluster."
  type        = string
  default     = "controller:latest"
}

variable "load_manager_image" {
  description = "Whether to load manager_image into the kind cluster after creation."
  type        = bool
  default     = true
}

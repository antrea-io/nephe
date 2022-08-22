variable aks_client_id {}
variable aks_client_secret {}
variable aks_client_subscription_id{}
variable aks_client_tenant_id{}
variable cloud_controller_identity_id{
  default = ""
}

variable owner {
  type = string
  default = null
}

variable aks_open_port {
  default = 22
}

variable "aks_worker_count" {
  default = 2
}

variable "aks_worker_type" {
  default = "Standard_DS2_v2"
}

variable "ssh_public_key" {
  default = "~/.ssh/id_rsa.pub"
}

variable location {
  default = "West US 2"
}

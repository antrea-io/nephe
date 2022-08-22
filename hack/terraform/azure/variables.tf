variable azure_client_id {}
variable azure_client_secret {}
variable azure_client_subscription_id {}
variable azure_client_tenant_id {}

variable owner {
  type    = string
  default = null
}

variable azure_open_port {
  default = 22
}

variable "ssh_public_key" {
  default = "~/.ssh/id_rsa.pub"
}

variable location {
  default = "West US 2"
}

variable network_address_space {
  default = "10.0.0.0/16"
}

variable subnet_prefix {
  default = "10.0.1.0/24"
}

variable azure_vm_type {
  default = "Standard_DS1_v2"
}

variable "azure_vm_os_types" {
  type = list(object({
    name      = string
    offer     = string
    publisher = string
    sku       = string
    init      = string
  }))
  default = [
    {
      name      = "ubuntu-host"
      offer     = "UbuntuServer"
      publisher = "Canonical"
      sku       = "16.04-LTS"
      init      = "init_script_ubuntu.sh"
    },
    {
      name      = "rhel-host"
      offer     = "RHEL"
      publisher = "RedHat"
      sku       = "8.1-ci"
      init      = "init_script_rhel.sh"
    },
    {
      name      = "centos-host"
      offer     = "CentOS"
      publisher = "OpenLogic"
      sku       = "8_1"
      init      = "init_script_centos.sh"
    }
  ]
}

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

variable azure_win_vm_type {
  default = "Standard_D2s_v3"
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
      name      = "ubuntu-host1"
      offer     = "0001-com-ubuntu-server-focal"
      publisher = "canonical"
      sku       = "20_04-lts-gen2"
      init      = "init_script_ubuntu.sh"
    },
    {
      name      = "rhel-host2"
      offer     = "RHEL"
      publisher = "RedHat"
      sku       = "91-gen2"
      init      = "init_script_rhel.sh"
    },
    {
      name      = "centos-host3"
      offer     = "CentOS"
      publisher = "OpenLogic"
      sku       = "8_5"
      init      = "init_script_centos.sh"
    }
  ]
}

variable "azure_vm_os_types_agented" {
  type = list(object({
    name      = string
    offer     = string
    publisher = string
    sku       = string
    init      = string
  }))
  default = [
    {
      name      = "ubuntu-host1"
      offer     = "0001-com-ubuntu-server-focal"
      publisher = "Canonical"
      sku       = "20_04-lts-gen2"
      init      = "init_script_ubuntu.sh"
    },
    {
      name      = "ubuntu-host2"
      offer     = "0001-com-ubuntu-server-focal"
      publisher = "Canonical"
      sku       = "20_04-lts-gen2"
      init      = "init_script_ubuntu.sh"
    },
    {
      name      = "ubuntu-host3"
      offer     = "0001-com-ubuntu-server-focal"
      publisher = "Canonical"
      sku       = "20_04-lts-gen2"
      init      = "init_script_ubuntu.sh"
    },
  ]
}

variable "azure_vm_os_types_agented_windows" {
  type = list(object({
    name      = string
    image     = string
    init      = string
    rg        = string
    gallery   = string
  }))
  default = [
    {
      name      = "win-1"
      image     = "WindowsNepheCI"
      init      = "init_script_windows.ps1"
      rg        = "nephe-ci"
      gallery   = "NepheGallery"
    },
    {
      name      = "win-2"
      image     = "WindowsNepheCI"
      init      = "init_script_windows.ps1"
      rg        = "nephe-ci"
      gallery   = "NepheGallery"
    },
    {
      name      = "win-3"
      image     = "WindowsNepheCI"
      init      = "init_script_windows.ps1"
      rg        = "nephe-ci"
      gallery   = "NepheGallery"
    }
  ]
}

variable "with_agent" {
  type    = bool
  default = false
}

variable "with_windows" {
  type    = bool
  default = false
}

variable "namespace" {
  type    = string
  default = "vm-ns"
}

variable "antrea_version" {
  type    = string
  default = "v1.13.0"
}

variable "antrea_agent_k8s_config" {
  type    = string
  default = "antrea-agent.kubeconfig"
}

variable "antrea_agent_antrea_config" {
  type    = string
  default = "antrea-agent.antrea.kubeconfig"
}

variable "install_vm_agent_wrapper" {
  type    = string
  default = "install-vm-agent-wrapper.sh"
}

variable "username" {
  type    = string
  default = "azureuser"
}

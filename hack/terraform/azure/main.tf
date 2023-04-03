terraform {
  required_providers {
    azurerm = {
      version = "~> 3.11.0"
    }
    random = {
      version = "~> 3.1"
    }
  }
}

provider "azurerm" {
  client_id       = var.azure_client_id
  client_secret   = var.azure_client_secret
  subscription_id = var.azure_client_subscription_id
  tenant_id       = var.azure_client_tenant_id
  features {
    resource_group {
      prevent_deletion_if_contains_resources = false
    }
  }
}

resource "azurerm_resource_group" "vm" {
  name     = local.resource_group_name
  location = var.location
}

locals {
  resource_group_name = "nephe-vnet-${var.owner}-${random_string.suffix.result}"
  azure_vm_os_types   = var.with_agent ? (var.with_windows ? var.azure_vm_os_types_agented_windows : var.azure_vm_os_types_agented) : var.azure_vm_os_types
}

locals {
  vnet_name_random = "nephe-vnet-${random_id.suffix.hex}"
  password_random  = "nephe-${random_string.special.result}"
}

resource "random_id" "suffix" {
  byte_length = 8
}

resource "random_string" "suffix" {
  length  = 4
  special = false
}

resource "random_string" "special" {
  length           = 8
  upper            = true
  numeric          = true
  special          = true
  override_special = "!@$*-?"
}

data "template_file" user_data {
  count    = length(local.azure_vm_os_types)
  template = file(local.azure_vm_os_types[count.index].init)
  vars     = {
    WITH_AGENT               = var.with_agent
    K8S_CONF                 = var.with_agent ? file(var.antrea_agent_k8s_config) : ""
    ANTREA_CONF              = var.with_agent ? file(var.antrea_agent_antrea_config) : ""
    INSTALL_VM_AGENT_WRAPPER = var.with_agent ? file(var.install_vm_agent_wrapper) : ""
    NAMESPACE                = var.namespace
    ANTREA_VERSION           = var.antrea_version
    SSH_PUBLIC_KEY           = file(var.ssh_public_key)
  }
}

data "azurerm_shared_image" "agent_image" {
  count               = var.with_windows ? length(var.azure_vm_os_types_agented_windows) : 0
  name                = var.azure_vm_os_types_agented_windows[count.index].image
  gallery_name        = var.azure_vm_os_types_agented_windows[count.index].gallery
  resource_group_name = var.azure_vm_os_types_agented_windows[count.index].rg
}

resource "azurerm_virtual_machine_extension" "win_script" {
  count                = var.with_windows ? length(var.azure_vm_os_types_agented_windows) : 0
  name                 = "win_script"
  virtual_machine_id   = module.vm_cluster[count.index].vm_ids[0]
  publisher            = "Microsoft.Compute"
  type                 = "CustomScriptExtension"
  type_handler_version = "1.9"

  settings = <<SETTINGS
  {
      "commandToExecute": "powershell -command \"[System.Text.Encoding]::UTF8.GetString([System.Convert]::FromBase64String('${base64encode(data.template_file.user_data[count.index].rendered)}')) | Out-File -filepath init_script.ps1\" && powershell -ExecutionPolicy Unrestricted -File init_script.ps1"
  }
  SETTINGS
}

# Copy the files since custom_data is unavailable
# in compute/azurerm module for Windows VM.
resource "null_resource" "win-remote-exec" {
  count    = var.with_windows ? length(var.azure_vm_os_types_agented_windows) : 0
  triggers = {
    vm_id = module.vm_cluster[count.index].vm_ids[0]
  }

  connection {
    type     = "ssh"
    user     = var.username
    password = local.password_random
    insecure = true
    host     = module.vm_cluster[count.index].public_ip_address[0]
  }

  provisioner "file" {
    source      = var.install_vm_agent_wrapper
    destination = "/tmp/install-vm-agent-wrapper.ps1"
  }
}

module "vm_cluster" {
  source                  = "Azure/compute/azurerm"
  version                 = "4.0.0"
  resource_group_name     = azurerm_resource_group.vm.name
  count                   = length(local.azure_vm_os_types)
  nb_instances            = 1
  vm_os_publisher         = var.with_windows ? "" : local.azure_vm_os_types[count.index].publisher
  vm_os_offer             = var.with_windows ? "" : local.azure_vm_os_types[count.index].offer
  vm_os_sku               = var.with_windows ? "" : local.azure_vm_os_types[count.index].sku
  vm_hostname             = "${local.azure_vm_os_types[count.index].name}-${var.owner}"
  vm_os_id                = var.with_windows ? data.azurerm_shared_image.agent_image[count.index].id : ""
  vm_size                 = var.with_windows ? var.azure_win_vm_type : var.azure_vm_type
  vnet_subnet_id          = module.network.vnet_subnets[0]
  enable_ssh_key          = var.with_windows ? false : true
  ssh_key                 = var.ssh_public_key
  # TODO: Reevaluate security rule creation after upgrading Azure/compute/azurerm
  # and verify if https://github.com/antrea-io/nephe/issues/199 is fixed.
  # remote_port             = var.with_agent ? "*" : "80"
  remote_port             = "80"
  source_address_prefixes = ["0.0.0.0/0"]
  is_windows_image        = var.with_windows ? true : false
  admin_username          = var.username
  admin_password          = local.password_random

  custom_data = var.with_windows ? "" : data.template_file.user_data[count.index].rendered

  nb_public_ip      = 1
  allocation_method = "Static"

  tags = {
    Terraform   = "true"
    Environment = "nephe"
    Name        = local.azure_vm_os_types[count.index].name
  }
}

module "network" {
  source              = "Azure/vnet/azurerm"
  version             = "3.1.0"
  resource_group_name = azurerm_resource_group.vm.name
  address_space       = [var.network_address_space]
  subnet_prefixes     = [var.subnet_prefix]
  subnet_names        = ["subnet1"]
  vnet_name           = local.vnet_name_random
  vnet_location       = var.location
}

# TODO: Reevaluate security rule creation after upgrading Azure/compute/azurerm
# and verify if https://github.com/antrea-io/nephe/issues/199 is fixed.
resource "azurerm_network_security_rule" "agent_allow_all" {
  count                       = var.with_agent ? length(local.azure_vm_os_types) : 0
  name                        = "agent_allow_all"
  resource_group_name         = azurerm_resource_group.vm.name
  priority                    = 102
  direction                   = "Inbound"
  access                      = "Allow"
  protocol                    = "Tcp"
  source_port_range           = "*"
  destination_port_range      = "*"
  source_address_prefixes     = ["0.0.0.0/0"]
  destination_address_prefix  = "*"
  network_security_group_name = module.vm_cluster[count.index].network_security_group_name
  depends_on = [module.vm_cluster]
}

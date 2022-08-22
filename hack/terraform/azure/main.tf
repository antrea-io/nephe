terraform {
  required_providers {
    azurerm = {
      version = "~>2.36"
    }
    random = {
      version = "~> 2.1"
    }
  }
}

provider "azurerm" {
  client_id       = var.azure_client_id
  client_secret   = var.azure_client_secret
  subscription_id = var.azure_client_subscription_id
  tenant_id       = var.azure_client_tenant_id
  features {}
}

resource "azurerm_resource_group" "vm" {
  name     = local.resource_group_name
  location = var.location
}

locals {
  resource_group_name = "nephe-vnet-${var.owner}-${random_string.suffix.result}"
}

locals {
  vnet_name_random = "nephe-vnet-${random_id.suffix.hex}"
}

resource "random_id" "suffix" {
  byte_length = 8
}

resource "random_string" "suffix" {
  length  = 4
  special = false
}

data "template_file" user_data {
  count    = length(var.azure_vm_os_types)
  template = file(var.azure_vm_os_types[count.index].init)
}

module "vm_cluster" {
  source                  = "Azure/compute/azurerm"
  resource_group_name     = azurerm_resource_group.vm.name
  count                   = length(var.azure_vm_os_types)
  nb_instances            = 1
  vm_os_publisher         = var.azure_vm_os_types[count.index].publisher
  vm_os_offer             = var.azure_vm_os_types[count.index].offer
  vm_os_sku               = var.azure_vm_os_types[count.index].sku
  vm_hostname             = "${var.azure_vm_os_types[count.index].name}-${var.owner}"
  vm_size                 = var.azure_vm_type
  vnet_subnet_id          = module.network.vnet_subnets[0]
  enable_ssh_key          = true
  ssh_key                 = var.ssh_public_key
  remote_port             = 80
  source_address_prefixes = ["0.0.0.0/0"]

  custom_data = data.template_file.user_data[count.index].rendered

  nb_public_ip      = 1
  allocation_method = "Static"

  tags = {
    Terraform   = "true"
    Environment = "nephe"
    Name        = var.azure_vm_os_types[count.index].name
  }
}

module "network" {
  source              = "Azure/vnet/azurerm"
  resource_group_name = azurerm_resource_group.vm.name
  address_space       = [var.network_address_space]
  subnet_prefixes     = [var.subnet_prefix]
  subnet_names        = ["subnet1"]
  vnet_name           = local.vnet_name_random
}

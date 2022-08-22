terraform {
  required_version = ">= 0.12.0"
  required_providers {
    azurerm = {
      version = "=2.78.0"
    }
    external = {
      version = "~> 1.2"
    }
    random = {
      version = "~> 2.1"
    }
  }
}

provider "azurerm" {
  client_id       = var.aks_client_id
  client_secret   = var.aks_client_secret
  subscription_id = var.aks_client_subscription_id
  tenant_id       = var.aks_client_tenant_id
  features {}
}

resource "azurerm_resource_group" "k8s" {
  name     = local.resource_group_name
  location = var.location
}

# get the auto-generated NSG name
data "external" "aks_nsg_id" {
  program = [
    "bash",
    "aks_nsg_id",
    local.resource_group_name
  ]
  depends_on = [azurerm_kubernetes_cluster.k8s]
}

# get the auto-generated resource group name
data "external" "aks_rg_name" {
  program = [
    "bash",
    "aks_rg_name",
    local.resource_group_name
  ]
  depends_on = [azurerm_kubernetes_cluster.k8s]
}

locals {
  owner_name = substr(var.owner, 0, 5)
}

locals {
  resource_group_name = "nephe-aks-${local.owner_name}-${random_string.suffix.result}"
}

resource "random_string" "suffix" {
  length  = 4
  special = false
}

resource "azurerm_network_security_rule" "openport" {
  name                        = "openport"
  priority                    = 1001
  direction                   = "Inbound"
  access                      = "Allow"
  protocol                    = "Tcp"
  source_port_range           = "*"
  destination_port_range      = var.aks_open_port
  source_address_prefix       = "*"
  destination_address_prefix  = "*"
  resource_group_name         = data.external.aks_rg_name.result.output
  network_security_group_name = data.external.aks_nsg_id.result.output
}

resource "azurerm_kubernetes_cluster" "k8s" {
  name                = local.resource_group_name
  location            = azurerm_resource_group.k8s.location
  resource_group_name = azurerm_resource_group.k8s.name
  dns_prefix          = azurerm_resource_group.k8s.name

  linux_profile {
    admin_username = "azureuser"

    ssh_key {
      key_data = file(var.ssh_public_key)
    }
  }

  default_node_pool {
    name                  = "agentpool"
    node_count            = var.aks_worker_count
    vm_size               = var.aks_worker_type
    enable_node_public_ip = true
  }

  network_profile {
    network_plugin = "azure"
  }

  role_based_access_control {
    enabled = true
  }

  identity {
    type = "SystemAssigned"
  }

  kubernetes_version = "1.21.9"

  tags = {
    Environment = "nephe"
  }
}

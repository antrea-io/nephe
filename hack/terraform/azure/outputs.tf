output "vm_ids" {
  description = "List of IDs of instances"
  value       = module.vm_cluster[*].vm_ids[0]
}

output "primary_nics" {
  description = "List of primary NICs of vm instances"
  value       = module.vm_cluster[*].network_interface_ids[0]
}

output "vnet_security_group_ids" {
  description = "List of VNet security group ids assigned to the instances"
  value       = module.vm_cluster[*].network_security_group_id
}

output "resource_group_name" {
  description = "Resource group name"
  value       = azurerm_resource_group.vm.name
}

output "vnet_name" {
  description = "VNet name"
  value       = module.network.vnet_name
}

output "vnet_id" {
  description = "VNet id"
  value       = module.network.vnet_id
}

output "tags" {
  description = "List of tags"
  value       = [
    { Name = var.with_agent ? (var.with_windows ? var.azure_vm_os_types_agented_windows[0].name : var.azure_vm_os_types_agented[0].name) : var.azure_vm_os_types[0].name },
    { Name = var.with_agent ? (var.with_windows ? var.azure_vm_os_types_agented_windows[1].name : var.azure_vm_os_types_agented[1].name) : var.azure_vm_os_types[1].name },
    { Name = var.with_agent ? (var.with_windows ? var.azure_vm_os_types_agented_windows[2].name : var.azure_vm_os_types_agented[2].name) : var.azure_vm_os_types[2].name }
  ]
}

output "public_ips" {
  description = "List of public IPs"
  value       = module.vm_cluster[*].public_ip_address[0]
}

output "private_ips" {
  description = "List of private IPs"
  value       = module.vm_cluster[*].network_interface_private_ip[0]
}

output "location" {
  description = "VNet location"
  value       = var.location
}

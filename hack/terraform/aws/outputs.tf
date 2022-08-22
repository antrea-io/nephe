output "vm_ids" {
  description = "List of IDs of instances"
  value       = module.ec2_cluster[*].id[0]
}

output "primary_nics" {
  description = "List of primary NICs of vm instances"
  value       = module.ec2_cluster[*].primary_network_interface_id[0]
}

output "vpc_security_group_ids" {
  description = "List of VPC security group ids assigned to the instances"
  value       = module.ec2_cluster[*].vpc_security_group_ids[0]
}

output "vpc_id" {
  description = "VPC id"
  value       = module.vpc.vpc_id
}

output "vpc_name" {
  description = "VPC name"
  value       = module.vpc.name
}

output "tags" {
  description = "List of tags"
  value       = module.ec2_cluster[*].tags[0]
}

output "public_ips" {
  description = "List of public IPs"
  value       = module.ec2_cluster[*].public_ip[0]
}

output "private_ips" {
  description = "List of private IPs"
  value       = module.ec2_cluster[*].private_ip[0]
}

output "region" {
  description = "VPC region"
  value       = var.region
}

terraform {
  required_providers {
    aws = {
      version = ">= 3.34"
    }
    random = {
      version = "~> 2.1"
    }
  }
}

provider "aws" {}

##################################################################
# Data sources to get VPC, subnet, security group and AMI details
##################################################################

locals {
  vpc_name        = "nephe-vpc-${var.owner}-${random_string.suffix.result}"
  aws_vm_os_types = var.with_agent ? (var.with_windows ? var.aws_vm_os_types_agented_windows : var.aws_vm_os_types_agented) : var.aws_vm_os_types
  aws_vm_type     = var.with_windows ? var.aws_win_vm_type : var.aws_vm_type
}

data "aws_subnets" "all" {
  filter {
    name   = "vpc-id"
    values = [module.vpc.vpc_id]
  }
  depends_on = [module.vpc.public_subnet_arns]
}

data "aws_ami" aws_ami {
  count       = length(local.aws_vm_os_types)
  most_recent = true
  filter {
    name   = "name"
    values = [local.aws_vm_os_types[count.index].ami_name_search]
  }
  owners = [local.aws_vm_os_types[count.index].ami_owner]
}

data "template_file" user_data {
  count    = length(local.aws_vm_os_types)
  template = file(local.aws_vm_os_types[count.index].init)
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

data "aws_region" "current" {}

module "vpc" {
  source  = "terraform-aws-modules/vpc/aws"
  version = ">=2.77.0"

  name = local.vpc_name
  cidr = var.vpc_cidr

  azs            = ["${data.aws_region.current.name}b"]
  public_subnets = [var.vpc_public_subnet]

  #enable_nat_gateway = true
  #enable_vpn_gateway = true

  tags = {
    Terraform   = "true"
    Environment = "nephe"
  }
}

module "security_group" {
  source          = "terraform-aws-modules/security-group/aws"
  version         = ">=3.18.0"
  count           = length(var.aws_security_groups_postfix)
  name            = "nephe-at-${var.aws_security_groups_postfix[count.index]}"
  vpc_id          = module.vpc.vpc_id
  use_name_prefix = false

  tags = {
    Terraform   = "true"
    Environment = "nephe"
  }
}

resource "aws_default_security_group" "default_security_group" {
  vpc_id = module.vpc.vpc_id

  ingress {
    protocol    = var.with_agent ? "-1" : "tcp"
    from_port   = var.with_agent ? 0 : 80
    to_port     = var.with_agent ? 0 : 80
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    protocol    = "-1"
    from_port   = 0
    to_port     = 0
    cidr_blocks = ["0.0.0.0/0"]
  }
  tags = {
    Terraform   = "true"
    Environment = "nephe"
  }
}

resource "random_string" "suffix" {
  length  = 8
  special = false
}

module "ec2_cluster" {
  count                       = length(local.aws_vm_os_types)
  source                      = "terraform-aws-modules/ec2-instance/aws"
  version                     = "~> 2.0"
  name                        = "${module.vpc.vpc_id}-${local.aws_vm_os_types[count.index].name}-${var.owner}"
  ami                         = data.aws_ami.aws_ami[count.index].id
  instance_type               = local.aws_vm_type
  key_name                    = var.aws_key_pair_name
  monitoring                  = true
  subnet_id                   = tolist(data.aws_subnets.all.ids)[0]
  associate_public_ip_address = true
  user_data                   = data.template_file.user_data[count.index].rendered

  tags = {
    Terraform   = "true"
    Environment = "nephe"
    Login       = local.aws_vm_os_types[count.index].login
  }
}

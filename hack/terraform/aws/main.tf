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

provider "aws" {
  region     = var.region
  access_key = var.aws_access_key_id
  secret_key = var.aws_access_key_secret
}

##################################################################
# Data sources to get VPC, subnet, security group and AMI details
##################################################################

locals {
  vpc_name = "nephe-vpc-${var.owner}-${random_string.suffix.result}"
}

data "aws_subnets" "all" {
  filter {
    name   = "vpc-id"
    values = [module.vpc.vpc_id]
  }
  depends_on = [module.vpc.public_subnet_arns]
}

data "aws_ami" aws_ami {
  count       = length(var.aws_vm_os_types)
  most_recent = true
  filter {
    name   = "name"
    values = [var.aws_vm_os_types[count.index].ami_name_search]
  }
  owners = [var.aws_vm_os_types[count.index].ami_owner]
}

data "template_file" user_data {
  count    = length(var.aws_vm_os_types)
  template = file(var.aws_vm_os_types[count.index].init)
}

module "vpc" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "2.77.0"

  name = local.vpc_name
  cidr = var.vpc_cidr

  azs            = ["${var.region}b"]
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
  version         = "3.18.0"
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
    protocol    = "tcp"
    from_port   = 80
    to_port     = 80
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
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
  count                       = length(var.aws_vm_os_types)
  source                      = "terraform-aws-modules/ec2-instance/aws"
  version                     = "~> 2.0"
  instance_count              = 1
  name                        = "${module.vpc.vpc_id}-${var.aws_vm_os_types[count.index].name}-${var.owner}"
  ami                         = data.aws_ami.aws_ami[count.index].id
  instance_type               = var.aws_vm_type
  key_name                    = var.aws_key_pair_name
  monitoring                  = true
  subnet_id                   = tolist(data.aws_subnets.all.ids)[0]
  associate_public_ip_address = true
  user_data                   = data.template_file.user_data[count.index].rendered

  tags = {
    Terraform   = "true"
    Environment = "nephe"
    Login       = var.aws_vm_os_types[count.index].login
  }
}

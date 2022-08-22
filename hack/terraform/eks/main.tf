terraform {
  required_version = ">= 0.13.1"
  required_providers {
    aws = {
      version = "~> 4.4"
    }
    random = {
      version = "~> 2.1"
    }
    template = {
      version = "~> 2.1"
    }
    local = {
      version = "~>2.1.0"
    }
    null = {
      version = "~>3.1"
    }
    kubernetes = {
      version = "~>2.11.0"
    }
  }
}

provider "aws" {
  region = var.region
}

provider "kubernetes" {
  host                   = data.aws_eks_cluster.cluster.endpoint
  cluster_ca_certificate = base64decode(data.aws_eks_cluster.cluster.certificate_authority.0.data)
  token                  = data.aws_eks_cluster_auth.cluster.token
}

data "aws_eks_cluster" "cluster" {
  name = module.eks.cluster_id
}

data "aws_eks_cluster_auth" "cluster" {
  name = module.eks.cluster_id
}

data "aws_caller_identity" "current" {}

data "aws_availability_zones" "available" {}

locals {
  vpc_name = "nephe-eks-vpc-${var.owner}-${random_string.suffix.result}"
}

locals {
  cluster_name = "nephe-eks-${var.owner}-${random_string.suffix.result}"
}

resource "random_string" "suffix" {
  length  = 4
  special = false
}

resource "aws_security_group" "all_worker_mgmt" {
  name_prefix = "nephe-eks"
  vpc_id      = module.vpc.vpc_id

  ingress {
    from_port = var.eks_open_port_begin
    to_port   = var.eks_open_port_end
    protocol  = "tcp"

    cidr_blocks = [
      "0.0.0.0/0",
      "172.16.0.0/12",
      "192.168.0.0/16",
    ]
  }
}

module "vpc" {
  source               = "terraform-aws-modules/vpc/aws"
  version              = "2.77.0"
  name                 = local.vpc_name
  cidr                 = "10.10.0.0/16"
  azs                  = data.aws_availability_zones.available.names
  public_subnets       = ["10.10.4.0/24", "10.10.5.0/24", "10.10.6.0/24"]
  # enable_nat_gateway   = false
  enable_dns_hostnames = true

  tags = {
    Terraform   = "true"
    Environment = "nephe"
  }

  public_subnet_tags = {
    "kubernetes.io/cluster/${local.cluster_name}" = "shared"
    "kubernetes.io/role/elb"                      = "1"
  }
}

module "eks" {
  source          = "terraform-aws-modules/eks/aws"
  version         = "~>17.24.0"
  cluster_name    = local.cluster_name
  subnets         = module.vpc.public_subnets
  cluster_version = "1.21"

  tags = {
    Terraform   = "true"
    Environment = "nephe"
  }

  vpc_id = module.vpc.vpc_id

  worker_groups = [
    {
      name                      = "worker-group"
      instance_type             = var.eks_worker_type
      asg_desired_capacity      = var.eks_worker_count
      public_ip                 = true
      iam_instance_profile_name = var.eks_iam_instance_profile_name
      key_name                  = var.eks_key_pair_name
    },
  ]

  worker_additional_security_group_ids = [aws_security_group.all_worker_mgmt.id]
  manage_cluster_iam_resources         = false
  cluster_iam_role_name                = var.eks_cluster_iam_role_name
  manage_worker_iam_resources          = false

  workers_group_defaults = {
    # Bug in eks module, it uses gp3 type volume that is not supported yet.
    root_volume_type = "gp2"
  }

  kubeconfig_api_version                    = "client.authentication.k8s.io/v1beta1"
  kubeconfig_aws_authenticator_command      = "aws"
  kubeconfig_aws_authenticator_command_args = ["eks", "get-token", "--cluster-name", local.cluster_name]
}

variable aws_key_pair_name {}
variable eks_iam_instance_profile_name {}
variable eks_cluster_iam_role_name {}
variable owner {
  type    = string
  default = null
}

variable eks_open_port_begin {
  default = 22
}

variable eks_open_port_end {
  default = 22
}

variable eks_worker_count {
  default = 2
}

variable eks_worker_type {
  default = "t2.medium"
}

variable "region" {
  default = "us-west-1"
}

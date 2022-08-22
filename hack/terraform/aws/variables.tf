variable aws_access_key_id {}
variable aws_access_key_secret {}
variable aws_key_pair_name {}

variable owner {
  type    = string
  default = null
}

variable "region" {
  default = "us-west-1"
}

variable aws_vm_type {
  default = "t2.micro"
}

variable peer_vpc_id {
  default = ""
}

variable vpc_cidr {
  default = "10.0.0.0/16"
}

variable vpc_public_subnet {
  default = "10.0.1.0/24"
}

variable "aws_vm_os_types" {
  type = list(object({
    name            = string
    # ami = string
    login           = string
    init            = string
    ami_name_search = string
    ami_owner       = string
  }))
  default = [
    {
      name            = "ubuntu1"
      login           = "ubuntu"
      init            = "init_script_ubuntu.sh"
      ami_name_search = "ubuntu/images/hvm-ssd/ubuntu-xenial-16.04-amd64-server-*"
      ami_owner       = "099720109477"
    },
    {
      name            = "ubuntu2"
      login           = "ubuntu"
      init            = "init_script_ubuntu.sh"
      ami_name_search = "ubuntu/images/hvm-ssd/ubuntu-xenial-16.04-amd64-server-*"
      ami_owner       = "099720109477"
    },
    {
      name            = "amzn"
      login           = "ec2-user"
      init            = "init_script_amzn.sh"
      ami_name_search = "amzn-ami-hvm-*x86_64*"
      ami_owner       = "137112412989"
    }
  ]
}

variable "aws_security_groups_postfix" {
  type    = list(string)
  default = [
    "default-vm-deny-all-apply-to",
    "default-vm-allow-all-apply-to",
  ]
}

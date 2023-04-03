variable aws_key_pair_name {}

variable owner {
  type    = string
  default = null
}

variable aws_vm_type {
  default = "t2.micro"
}

variable aws_win_vm_type {
  default = "t3.medium"
}

variable "ssh_public_key" {
  default = "~/.ssh/id_rsa.pub"
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
    login           = string
    init            = string
    ami_name_search = string
    ami_owner       = string
  }))
  default = [
    {
      name            = "ubuntu-host1"
      login           = "ubuntu"
      init            = "init_script_ubuntu.sh"
      ami_name_search = "ubuntu/images/hvm-ssd/ubuntu-focal-20.04-amd64-server-*"
      ami_owner       = "099720109477"
    },
    {
      name            = "rhel-host2"
      login           = "ec2-user"
      init            = "init_script_rhel.sh"
      ami_name_search = "RHEL_HA-8.4.0_HVM-20210504-x86_64-2-Hourly2-GP2"
      ami_owner       = "309956199498"
    },
    {
      name            = "amzn-host3"
      login           = "ec2-user"
      init            = "init_script_amzn.sh"
      ami_name_search = "amzn-ami-hvm-*x86_64*"
      ami_owner       = "137112412989"
    }
  ]
}

variable "aws_vm_os_types_agented" {
  type = list(object({
    name            = string
    login           = string
    init            = string
    ami_name_search = string
    ami_owner       = string
  }))
  default = [
    {
      name            = "ubuntu-host1"
      login           = "ubuntu"
      init            = "init_script_ubuntu.sh"
      ami_name_search = "ubuntu/images/hvm-ssd/ubuntu-focal-20.04-amd64-server-*"
      ami_owner       = "099720109477"
    },
    {
      name            = "ubuntu-host2"
      login           = "ubuntu"
      init            = "init_script_ubuntu.sh"
      ami_name_search = "ubuntu/images/hvm-ssd/ubuntu-focal-20.04-amd64-server-*"
      ami_owner       = "099720109477"
    },
    {
      name            = "rhel-host3"
      login           = "ec2-user"
      init            = "init_script_rhel.sh"
      ami_name_search = "RHEL_HA-8.4.0_HVM-20210504-x86_64-2-Hourly2-GP2"
      ami_owner       = "309956199498"
    },
  ]
}

variable "aws_vm_os_types_agented_windows" {
  type = list(object({
    name            = string
    login           = string
    init            = string
    ami_name_search = string
    ami_owner       = string
  }))
  default = [
    {
      name            = "windows2019-1"
      login           = "Administrator"
      init            = "init_script_windows.ps1"
      ami_name_search = "NepheWin2019Image"
      ami_owner       = "092722883498"
    },
    {
      name            = "windows2019-2"
      login           = "Administrator"
      init            = "init_script_windows.ps1"
      ami_name_search = "NepheWin2019Image"
      ami_owner       = "092722883498"
    },
    {
      name            = "windows2019-3"
      login           = "Administrator"
      init            = "init_script_windows.ps1"
      ami_name_search = "NepheWin2019Image"
      ami_owner       = "092722883498"
    },
  ]
}

variable "aws_security_groups_postfix" {
  type    = list(string)
  default = [
    "default-vm-deny-all-apply-to",
    "default-vm-allow-all-apply-to",
  ]
}

variable "with_agent" {
  type    = bool
  default = false
}

variable "with_windows" {
  type    = bool
  default = false
}

variable "namespace" {
  type    = string
  default = "vm-ns"
}

variable "antrea_version" {
  type    = string
  default = "v1.11.0"
}

variable "antrea_agent_k8s_config" {
  type    = string
  default = "antrea-agent.kubeconfig"
}

variable "antrea_agent_antrea_config" {
  type    = string
  default = "antrea-agent.antrea.kubeconfig"
}

variable "install_vm_agent_wrapper" {
  type    = string
  default = "install-vm-agent-wrapper.sh"
}

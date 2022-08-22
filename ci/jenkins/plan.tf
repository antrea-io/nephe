variable "vsphere_user" {
  type    = string
  default = "Administrator@vsphere.local"
}

variable "vsphere_password" {
  type = string
}

variable "vsphere_server" {
  type = string
}

variable "vsphere_datacenter" {
  type = string
}

variable "vsphere_datastore" {
  type = string
}

variable "vsphere_compute_cluster" {
  type = string
}

variable "vsphere_resource_pool" {
  type = string
}

variable "vsphere_network" {
  type = string
}

variable "vsphere_virtual_machine" {
  type = string
}

variable "vm_count" {
  type = number
}

variable "testbed_name" {
  type = string
}

locals {
  public_key_content  = file("${path.module}/id_rsa.pub")
  private_key_content = file("${path.module}/id_rsa")
}

provider "vsphere" {
  user           = "${var.vsphere_user}"
  password       = "${var.vsphere_password}"
  vsphere_server = "${var.vsphere_server}"

  # If you have a self-signed cert
  allow_unverified_ssl = true
}

data "vsphere_datacenter" "dc" {
  name = "${var.vsphere_datacenter}"
}

data "vsphere_datastore" "vsan_datastore" {
  name          = "${var.vsphere_datastore}"
  datacenter_id = "${data.vsphere_datacenter.dc.id}"
}

data "vsphere_compute_cluster" "cluster" {
  name          = "${var.vsphere_compute_cluster}"
  datacenter_id = "${data.vsphere_datacenter.dc.id}"
}

data "vsphere_resource_pool" "pool" {
  name          = "${var.vsphere_resource_pool}"
  datacenter_id = "${data.vsphere_datacenter.dc.id}"
}

data "vsphere_network" "network" {
  name          = "${var.vsphere_network}"
  datacenter_id = "${data.vsphere_datacenter.dc.id}"
}

data "vsphere_virtual_machine" "template" {
  name          = "${var.vsphere_virtual_machine}"
  datacenter_id = "${data.vsphere_datacenter.dc.id}"
}

resource "vsphere_virtual_machine" "vms" {
  count = "${var.vm_count}"

  name             = "${var.testbed_name}-${count.index}"
  resource_pool_id = "${data.vsphere_resource_pool.pool.id}"
  datastore_id     = "${data.vsphere_datastore.vsan_datastore.id}"

  num_cpus = 4
  memory   = 6144
  guest_id = "ubuntu64Guest"

  network_interface {
    network_id = "${data.vsphere_network.network.id}"
  }

  disk {
    label = "disk0"
    size  = 40
  }

  cdrom {
    client_device = true
  }

  clone {
    template_uuid = "${data.vsphere_virtual_machine.template.id}"
  }

  vapp {
    properties = {
      "public-keys" = "${local.public_key_content}"
      "hostname"    = "${var.testbed_name}-${count.index}"
    }
  }

  provisioner "file" {
    source      = "${path.module}/id_rsa"
    destination = "/home/ubuntu/.ssh/id_rsa"
    connection {
      type        = "ssh"
      user        = "ubuntu"
      host        = "${self.default_ip_address}"
      private_key = "${local.private_key_content}"
    }
  }

  provisioner "file" {
    source      = "${path.module}/id_rsa.pub"
    destination = "/home/ubuntu/.ssh/id_rsa.pub"
    connection {
      type        = "ssh"
      user        = "ubuntu"
      host        = "${self.default_ip_address}"
      private_key = "${local.private_key_content}"
    }
  }

  provisioner "remote-exec" {
    inline = [
      "sudo sysctl -w net.ipv6.conf.all.disable_ipv6=1",
      "sudo sysctl -w net.ipv6.conf.default.disable_ipv6=1",
      "sudo sysctl -w net.ipv6.conf.lo.disable_ipv6=1",
      "sudo ntpdate 10.112.16.181",
      "sudo apt update",
      "sudo apt install -y python3 python3-pip",
      "chmod 0600 /home/ubuntu/.ssh/id_rsa",
    ]
    connection {
      type        = "ssh"
      user        = "ubuntu"
      host        = "${self.default_ip_address}"
      private_key = "${local.private_key_content}"
    }
  }
}

output "vm_name_ips" {
  value = zipmap(vsphere_virtual_machine.vms.*.name, vsphere_virtual_machine.vms.*.default_ip_address)
}

output "vm_ips" {
  value = vsphere_virtual_machine.vms.*.default_ip_address
}

output "vm_masters" {
  value = [vsphere_virtual_machine.vms[0].name]
}

output "vm_jumper" {
  value = [vsphere_virtual_machine.vms[0].name]
}

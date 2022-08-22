#!/usr/bin/python3
import json
import os
import sys

_POD_CIDR = "192.168.0.0/16"
_SERVICE_CIDR = "10.96.0.0/12"

def _list(vm_name_ips, vm_masters, vm_jumper, _argv):
    id_rsa_path = os.path.abspath(os.path.join(
        os.path.dirname(__file__), 'id_rsa'))
    common_vars = {
        "test_user": "jenkins",
        "ansible_user": "ubuntu",
        "ansible_private_key_file": id_rsa_path,
        }
    jumper_group_vars = {}
    jumper_group_vars.update(common_vars)
    jumper_group = {
            "hosts": list(vm_jumper),
            "vars": jumper_group_vars,
            "children": [],
            }

    master_group_vars = {
        "k8s_pod_network_cidr": _POD_CIDR,
        "k8s_service_network_cidr": _SERVICE_CIDR,
        "k8s_ip_family": "v4",
        "k8s_api_server_ip": vm_name_ips[list(vm_masters)[0]],
        "kube_proxy_mode" : "iptables",
        "kube_proxy_ipvs_strict_arp": "false",
        }
    master_group_vars.update(common_vars)
    master_group = {
        "hosts": list(vm_masters),
        "vars": master_group_vars,
        "children": [],
        }

    worker_group = {
        "hosts": [vm for vm in vm_name_ips.keys()
                  if (vm not in vm_masters) and (vm not in vm_jumper)],
        "vars": common_vars}
    return {
        "controlplane": master_group,
        "workers": worker_group,
        "jumper": jumper_group,
        "children": [],
        }


def _host(vm_name_ips, vm_masters, vm_jumper, argv):
    vm = argv[0]
    common_vars = {
        "node_ip": vm_name_ips[vm],
        "node_name": vm,
        "ansible_host": vm_name_ips[vm],
        "node_ipv4": vm_name_ips[vm],
        "node_ipv6": "",
        }
    return common_vars


def _get_tfstate():
    tfenv_path = os.path.abspath(os.path.join(
        os.path.dirname(__file__), '.terraform/environment'))
    try:
        with open(tfenv_path) as f:
            workspace = f.read().strip()
    except Exception:
        workspace = 'default'

    if workspace == 'default':
        tfstate_path = os.path.abspath(os.path.join(
            os.path.dirname(__file__), 'terraform.tfstate'))
    else:
        tfstate_path = os.path.abspath(os.path.join(
            os.path.dirname(__file__),
            'terraform.tfstate.d/%s/terraform.tfstate' % workspace))

    with open(tfstate_path) as f:
        return json.loads(f.read())


def main():
    if len(sys.argv) < 2 or sys.argv[1] not in ("--list", "--host"):
        print("Usage\n%s --list\n%s --host <hostname>" %
              (sys.argv[0], sys.argv[0]))
        return 1

    tfstate = _get_tfstate()
    if not tfstate["resources"]:
        print("{}")
        return 0

    vm_name_ips = tfstate["outputs"]["vm_name_ips"]["value"]
    vm_masters = set(tfstate["outputs"]["vm_masters"]["value"])
    vm_jumper = set(tfstate["outputs"]["vm_jumper"]["value"])

    r = {"--list": _list, "--host": _host}[sys.argv[1]](
        vm_name_ips, vm_masters, vm_jumper, sys.argv[2:])

    print(json.dumps(r))

    return 0


if __name__ == '__main__':
    sys.exit(main())

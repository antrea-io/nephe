# Agented VM User Guide

## Table of Contents

<!-- toc -->
- [Import Agented And Agentless VMs](#import-agented-and-agentless-vms)
- [VirtualMachine CR Creation](#virtualmachine-cr-creation)
- [ExternalNode CR Creation](#externalnode-cr-creation)
- [ExternalEntities CR Creation](#externalentities-cr-creation)
- [Install Antrea-Agent On Public Cloud VM](#install-antrea-agent-on-public-cloud-vm)
  - [Installation On Linux VMs](#installation-on-linux-vms)
  - [Installation On Windows VMs](#installation-on-windows-vms)
- [Troubleshoot VM Agent](#troubleshoot-vm-agent)
  - [Linux VM](#linux-vm)
  - [Windows VM](#windows-vm)
<!-- /toc -->

Nephe supports auto-onboarding of public cloud VMs, that can run
`antrea-agent` on VMs. When a CloudEntitySelector selector is configured to
import agented VMs, `nephe-controller` will auto-create an ExternalNode CR
corresponding to each VirtualMachine CR. `Antrea-Controller` watches on the
ExternalNode CR and then creates a corresponding ExternalEntity CR. Upon
onboarding the agented VMs, user can define Antrea NetworkPolicies using
ExternalEntity selector.

For more information about the `ExternalNode` feature, please refer to antrea
[ExternalNode](https://github.com/antrea-io/antrea/blob/main/docs/external-node.md)
documentation.

Note: While applying antrea manifests, make sure to enable `externalnode`
feature in the `antrea-controller` configuration. Also, make sure that VM
can connect to the Kubernetes API server and Antrea API server.

## Import Agented And Agentless VMs

To import public cloud VMs that can run in agented or agentless configuration,
the user needs to specify the `agented` flag in CloudEntitySelector CR.

```bash
cat <<EOF | kubectl apply -f -
apiVersion: crd.cloud.antrea.io/v1alpha1
kind: CloudEntitySelector
metadata:
  name: cloudentityselector-aws-sample
  namespace: vm-ns
spec:
  accountName: cloudprovideraccount-aws-sample
  vmSelector:
    - vmMatch:
        - matchID: "i-0a20bae92ddcdb60b"
        - matchID: "i-05e3fb66922d56e0a"
        - matchID: "i-0033eb4a6c846451d"
      agented: false
    - vmMatch:
        - matchID: "i-019459b33d951b62e"
      agented: true
EOF
```

## VirtualMachine CR Creation

The `nephe-controller` will poll the cloud inventory and create VirtualMachine
CR. Based on the `agented` flag specified in the CES, VirtualMachines will
either be imported as agented or as agentless. In the example above
`i-019459b33d951b62e` is imported as an agented VM, while the remaining three
VMs are imported as agentless VMs.

```bash
kubectl get vm -A
```

```text
# Output
NAMESPACE   NAME                  CLOUD-PROVIDER   VIRTUAL-PRIVATE-CLOUD   STATE     AGENTED
vm-ns       i-0033eb4a6c846451d   AWS              vpc-0d6bb6a4a880bd9ad   running   false
vm-ns       i-019459b33d951b62e   AWS              vpc-0d6bb6a4a880bd9ad   running   true
vm-ns       i-05e3fb66922d56e0a   AWS              vpc-0d6bb6a4a880bd9ad   running   false
vm-ns       i-0a20bae92ddcdb60b   AWS              vpc-0d6bb6a4a880bd9ad   running   false
```

```bash
kubectl describe vm i-019459b33d951b62e -n vm-ns
```

```text
# Output
Name:         i-019459b33d951b62e
Namespace:    vm-ns
Labels:       <none>
Annotations:  cloud-assigned-id: i-019459b33d951b62e
              cloud-assigned-name: ubuntu2004
              cloud-assigned-vpc-id: vpc-0d6bb6a4a880bd9ad
API Version:  crd.cloud.antrea.io/v1alpha1
Kind:         VirtualMachine
Metadata:
  Creation Timestamp:  2022-09-27T12:18:23Z
  Generation:          1
  Managed Fields:
    API Version:  crd.cloud.antrea.io/v1alpha1
    Fields Type:  FieldsV1
    fieldsV1:
      f:metadata:
        f:annotations:
          .:
          f:cloud-assigned-id:
          f:cloud-assigned-name:
          f:cloud-assigned-vpc-id:
        f:ownerReferences:
          .:
          k:{"uid":"42467d8c-9ae7-433b-879c-fdd7b2b843e1"}:
    Manager:      nephe-controller
    Operation:    Update
    Time:         2022-09-27T12:18:23Z
    API Version:  crd.cloud.antrea.io/v1alpha1
    Fields Type:  FieldsV1
    fieldsV1:
      f:status:
        .:
        f:agented:
        f:networkInterfaces:
        f:provider:
        f:state:
        f:tags:
          .:
          f:Name:
        f:virtualPrivateCloud:
    Manager:      nephe-controller
    Operation:    Update
    Subresource:  status
    Time:         2022-09-27T12:18:23Z
  Owner References:
    API Version:           crd.cloud.antrea.io/v1alpha1
    Block Owner Deletion:  true
    Controller:            true
    Kind:                  CloudEntitySelector
    Name:                  cloudentityselector-aws-sample
    UID:                   42467d8c-9ae7-433b-879c-fdd7b2b843e1
  Resource Version:        7908985
  UID:                     da9c4826-e9d6-43d7-87e0-44743f881fb1
Status:
  Agented:  true
  Network Interfaces:
    Ips:
      Address:       10.0.1.45
      Address Type:  InternalIP
      Address:       54.193.202.222
      Address Type:  ExternalIP
    Mac:             02:96:fc:89:17:ed
    Name:            eni-056b0d6da3f592943
  Provider:          AWS
  State:             running
  Tags:
    Name:                 ubuntu2004
  Virtual Private Cloud:  vpc-0d6bb6a4a880bd9ad
Events:                   <none>
```

## ExternalNode CR Creation

The `Nephe Controller` will create an ExternalNode CR corresponding to each
VirtualMachine CR, which has the `agented` flag enabled. In the below example,
since VM `i-019459b33d951b62e` is configured with agented flag set to true,
a corresponding ExternalNode `virtualmachine-i-019459b33d951b62e` is created.

```bash
kubectl get en -A
```

```text
# Output
NAMESPACE   NAME                                 AGE
vm-ns       virtualmachine-i-019459b33d951b62e   6s
```

## ExternalEntities CR Creation

For each ExternalNode CR, `antrea-controller` will create a corresponding
ExternalEntity CR. In this example, the ExternalEntity
`virtualmachine-i-019459b33d951b62e-5df40` corresponds to the ExternalNode
`virtualmachine-i-019459b33d951b62e`.

```bash
kubectl get ee -A
```

```text
# Output
NAMESPACE   NAME                                       AGE
vm-ns       virtualmachine-i-0033eb4a6c846451d         9s
vm-ns       virtualmachine-i-019459b33d951b62e-5df40   9s
vm-ns       virtualmachine-i-05e3fb66922d56e0a         9s
vm-ns       virtualmachine-i-0a20bae92ddcdb60b         9s
```

## Install Antrea-Agent On Public Cloud VM

Nephe provides a wrapper install script, to facilitate easier installation on
the public cloud VMs. The wrapper install script sets the Environment variable
`NODE_NAME` to be same as ExternalNode name. It downloads the `antrea-agent`
install script from antrea repository and triggers installation.

The wrapper install script requires 4 arguments:
Namespace            - Specifies the Namespace to be used by the `antrea-agent`.
It should match with the ExternalNode Namespace.
Antrea version       - Specifies the Antrea version to be used.
Kubeconfig           - Provides access to the Kubernetes API server.
Antrea Kubeconfig    - Provides access to the Antrea API server.

To enable and configure ExternalNode feature in your cluster, please refer to
antrea [ExternalNode](https://github.com/antrea-io/antrea/blob/main/docs/external-node.md#install-antrea-agent-on-vm)
documentation.

Note: `Nephe-Controller` will auto create an ExternalNode corresponding to each
VirtualMachine CR, when agented flag is set.

### Installation On Linux VMs

Download the [wrapper install script](../hack/install-vm-agent-wrapper.sh) from a
[list of nephe releases](https://github.com/antrea-io/nephe/releases) and run
the installation. For any given release <TAG> (e.g. v0.2.0), you can download
the script as follows:

```bash
https://github.com/antrea-io/nephe/releases/download/v0.1.0/nephe.yml
curl https://github.com/antrea-io/nephe/releases/download/<TAG>/install-vm-agent-wrapper.sh --output install-vm-agent-wrapper.sh
./install-vm-agent-wrapper.sh --ns vm-ns --antrea-version v1.9.0 --kubeconfig ./antrea-agent.kubeconfig \
  --antrea-kubeconfig ./antrea-agent.antrea.kubeconfig
```

### Installation On Windows VMs

Download the [wrapper install script](../hack/install-vm-agent-wrapper.ps1) from a
[list of nephe releases](https://github.com/antrea-io/nephe/releases) and run
the installation. For any given release <TAG> (e.g. v0.2.0), you can download the script
as follows:

```powershell
curl.exe "-s" "-L" "https://github.com/antrea-io/nephe/releases/download/<TAG>/install-vm-agent-wrapper.ps1" -o install-vm-agent-wrapper.ps1
.\install-vm-agent-wrapper.ps1 -Namespace vm-ns -AntreaVersion v1.9.0 -KubeConfigPath .\antrea-agent.kubeconfig `
  -AntreaKubeConfigPath .\antrea-agent.antrea.kubeconfig
```

## Troubleshoot VM Agent

Run the `ovs-vsctl show` command on the VM, to validate the interface is moved
into OVS.

```text
root@ip-10-0-1-45:~# ovs-vsctl show
2d2efb51-3f63-4413-8a6e-ee27a7707da9
    Bridge br-int
        datapath_type: system
        Port "eth0~"
            Interface "eth0~"
        Port eth0
            Interface eth0
                type: internal
    ovs_version: "2.13.8"
```

### Linux VM

- The `antrea-agent` logs are located in `/var/log/antrea/antrea-agent.log`.
- The `antrea-agent` configuration files are located in `/etc/antrea`.

### Windows VM

- The `antrea-agent` logs are located in `C:\antrea-agent\logs`.
- The `antrea-agent` configuration files are located in `C:\antrea-agent\conf`.

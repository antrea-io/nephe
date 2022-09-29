# Agented VM User Guide

Nephe supports auto on-boarding of public cloud VMs, that can run
`antrea-agent` on VMs. When a CloudEntitySelector selector is configured to
import agented VMs, `nephe-controller` will auto create an ExternalNode CR
corresponding to each VirtualMachine CR. Antrea-Controller watches on the
ExternalNode CR and then create a corresponding ExternalEntity CR. Once VMs are
onboarded on to the nephe, user can define Antrea NetworkPolicies for these
VMs using ExternalEntity selector.

For more information about `ExternalNode`, please refer to antrea
[ExternalNode](https://github.com/antrea-io/antrea/blob/main/docs/external-node.md)
documentation.

Note: While applying antrea manifests, make sure to enable `externalnode`
feature in the `antrea-controller` configuration. Also make sure that VM
can connect to antrea API server

## Table of Contents

<!-- toc -->
- [Import Agented and Agentless VMs](#import-agented-and-agentless-vms)
- [VirtualMachine CR Creation](#virtualmachine-cr-creation)
- [ExternalNode CR Creation](#externalnode-cr-creation)
- [ExternalEntities CR Creation](#externalentities-cr-creation)
- [Install Antrea-Agent on PublicCloud VM](#install-antrea-agent-on-publiccloud-vm)
- [Troubleshoot](#troubleshoot)
  - [Ubuntu VM](#ubuntu-vm)
  - [Windows VM](#windows-vm)
<!-- /toc -->

## Import Agented and Agentless VMs

To import PublicCloud VMs that can run in agented/agentless configuration,
user needs to specify `agented` flag in CloudEntitySelector CR.

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

The account poller will poll the cloud inventory and create VirtualMachine CR.
Based on the `agented` flag, specified in the CES, VirtualMachines will be
imported as Agented/Agentless. In this example `i-019459b33d951b62e` is imported
as agented VM, while the remaining three VMs are imported as agentless VMs.

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
VirtualMachine CR, which has `agented` flag enabled. In the below example,
since VM `i-019459b33d951b62e` is configured with agented flag set to true,
an ExternalNode `vm-i-019459b33d951b62e` is created accordingly.

```bash
kubectl get en -A
```

```text
#Output
NAMESPACE   NAME                     AGE
vm-ns       vm-i-019459b33d951b62e   6s
```

## ExternalEntities CR Creation

For each ExternalNode CR, `antrea-controller` will create a corresponding ExternalEntity CR.
In this example, the ExternalEntity `vm-i-019459b33d951b62e-5df40` corresponds to the ExternalNode
`vm-i-019459b33d951b62e`. While the other three ExternalEntities are created by `nephe-controller`
with ExternalNode field set as `nephe-controller`.

```bash
kubectl get ee -A
```

```text
# Output
NAMESPACE   NAME                           AGE
vm-ns       vm-i-0033eb4a6c846451d         9s
vm-ns       vm-i-019459b33d951b62e-5df40   9s
vm-ns       vm-i-05e3fb66922d56e0a         9s
vm-ns       vm-i-0a20bae92ddcdb60b         9s
```

## Install Antrea-Agent on PublicCloud VM

Nephe provides a wrapper install script, to facilitate easier installation on
the PublicCloud VMs. The wrapper install script sets the Environment variable
`NODE_NAME` to be same as ExternalNode name; Downloads the `antrea-agent`
install script from antrea repository and triggers installation.

The wrapper install script requires 4 arguments
NameSpace            - It specifies the Namespace to be used by the antrea-agent.
It should match to the ExternalNode Namespace.
AntreaVersion        - It specifies the antrea version to be used.
KubeConfigPath       - It provides access to kubernetes API server.
AntreaKubeConfigPath - It provides access to antrea API server,

For more information on how to generate these kubeconfig files, please refer to
antrea [ExternalNode](https://github.com/antrea-io/antrea/blob/main/docs/external-node.md#install-antrea-agent-on-vm)
documentation.

```powershell
.\install-wrapper.ps1 -Namespace vm-ns -AntreaVersion v1.8.0 `
-KubeConfigPath C:\Users\nsxadmin\Desktop\antrea-agent.kubeconfig `
-AntreaKubeConfigPath C:\Users\nsxadmin\Desktop\antrea-agent.antrea.kubeconfig
```

```bash
./install-wrapper.sh --ns vm-ns --antrea-version v1.8.0 \
  --kubeconfig /root/antrea-agent.kubeconfig \
  --antrea-kubeconfig /root/antrea-agent.antrea.kubeconfig --bin /root/antrea-agent
```

NOTE: For Ubuntu, `antrea-agent` binaries are not published as part of release
assets in antrea 1.8 release. So user has to manually generate `antrea-agent`
binary and copy on to the VM.

## Troubleshoot

Run the `ovs-vsctl show` command on the VM, to validate the interface is moved into OVS.

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

### Ubuntu VM

- The `antrea-agent` logs are located in `/var/log/antrea/antrea-agent.log`.
- The `antrea-agent` configuration files are located in `/etc/antrea`.

### Windows VM

- The `antrea-agent` logs are located in `C:\antrea-agent\logs`
- The `antrea-agent` configuration files are located in `C:\antrea-agent\conf`.

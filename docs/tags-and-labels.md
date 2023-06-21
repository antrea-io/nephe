# Tags and Labels

## Table of Contents

<!-- toc -->
- [Virtual Machine](#virtual-machine)
  - [Labels](#labels)
  - [Cloud Tags](#cloud-tags)
  - [Sample VirtualMachine Output](#sample-virtualmachine-output)
    - [AWS](#aws)
    - [Azure](#azure)
- [External Entity](#external-entity)
  - [Sample ExternalEntity Output](#sample-externalentity-output)
    - [AWS](#aws-1)
    - [Azure](#azure-1)
  - [Labels](#labels-1)
  - [Cloud Tags](#cloud-tags-1)
    - [Usage](#usage)
<!-- /toc -->

## Virtual Machine

Virtual Machines are onboarded by specifying the matching criteria in `CloudEntitySelector` CR. The virtual machines from
cloud are imported as `VirtualMachine` objects in `CloudEntitySelector` namespace. `VirtualMachine` object `Name` is
a unique resource name within the Kubernetes cluster. In Azure, the ASCII sum of all characters in the VM Resource ID added as a
suffix to the Virtual Machine name. In AWS, instance ID of the Virtual Machine is used as the `Name`.

### Labels

Meaning of some label fields in `VirtualMachine` objects differ based on the cloud provider type.

| Label        | Description                               | Azure                                                      | AWS            |
|--------------|-------------------------------------------|------------------------------------------------------------|----------------|
| cloud-vm-uid | Unique identifier of VM on cloud          | VM `vmId` property                                         | Instance ID    |
| cloud-vpc-id | Unique identifier of VPC on cloud         | VNET `resourceGuid` property                               | VPC ID         |
| vpc-name     | VPC object name within Kubernetes cluster | VNET `name` suffixed with ascii sum of VNET `Resource ID`  | VPC ID         |

Following labels are generic and related to user configuration.

1. `ces-name`: Name of `CloudEntitySelector` CR to which the imported VM belongs to.
2. `ces-namespace`: Namespace of `CloudEntitySelector` CR to which the imported VM belongs to.
3. `cpa-name`: Name of `CloudProviderAccount` CR to which the imported VM belongs to.
4. `cpa-namespace`: Namespace of `CloudProviderAccount` CR to which the imported VM belongs to.

### Cloud Tags

The tags of `VirtualMachine` object are converted to labels in `ExternalEntity` CR. Therefore, only tags that comply to
the Kubernetes labels format are imported in the VM object.

*Key field specifications*:

- Required field.
- Must be 63 characters or fewer(4 characters are allocated to the prefix "tag-" in labels, hence only 59 characters
  can be used in key).
- First and last character must be an alphanumeric character ([a-z0-9A-Z]), with dashes (-), underscores (_), dots (.),
  and alphanumerics in between.

*Value field specifications*:

- Optional field.
- Must be 63 characters or fewer.
- Unless empty, must begin and end with an alphanumeric character, could contain dashes (-), underscores (_), dots (.),
  and alphanumerics between.

### Sample VirtualMachine Output

#### AWS

```bash
kubectl describe vm i-0c2e4054f435927a9 -n sample-ns
```

```yaml
# Output
Name:         i-0c2e4054f435927a9
Namespace:    sample-ns
Labels:       nephe.antrea.io/ces-name=cloudentityselector-aws-sample
              nephe.antrea.io/ces-namespace=sample-ns
              nephe.antrea.io/cloud-vm-uid=i-0c2e4054f435927a9
              nephe.antrea.io/cloud-vpc-uid=vpc-0882e6544e1bb0564
              nephe.antrea.io/cpa-name=cloudprovideraccount-aws-sample
              nephe.antrea.io/cpa-namespace=sample-ns
              nephe.antrea.io/vpc-name=vpc-0882e6544e1bb0564
Annotations:  <none>
API Version:  runtime.cloud.antrea.io/v1alpha1
Kind:         VirtualMachine
Metadata:
  Creation Timestamp:  <nil>
  UID:                 938d3805-faa9-48f3-9e54-20a19c1f3d55
Status:
  Agented:         false
  Cloud Id:        i-0c2e4054f435927a9
  Cloud Name:      ubuntu-vm
  Cloud Vpc Id:    vpc-0882e6544e1bb0564
  Cloud Vpc Name:  test-vpc
  Network Interfaces:
    Ips:
      Address:       10.10.6.108
      Address Type:  InternalIP
      Address:       54.219.53.133
      Address Type:  ExternalIP
    Mac:             06:3b:b6:bf:e2:9d
    Name:            eni-06dc156b39555a674
  Provider:          AWS
  Region:            us-west-1
  State:             running
  Tags:
    Name:  ubuntu-vm
Events:    <none>
```

#### Azure

```bash
kubectl describe vm vm1-11610 -n sample-ns
```

```yaml
# Output
Name:         vm1-11610
Namespace:    sample-ns
Labels:       nephe.antrea.io/ces-name=cloudentityselector-azure-sample
              nephe.antrea.io/ces-namespace=sample-ns
              nephe.antrea.io/cloud-vm-uid=d33fa6ce-9a12-48bd-ac0e-d842a2f24838
              nephe.antrea.io/cloud-vpc-uid=c440672a-47eb-462c-a567-4c6bc75eaabe
              nephe.antrea.io/cpa-name=cloudprovideraccount-azure-sample
              nephe.antrea.io/cpa-namespace=sample-ns
              nephe.antrea.io/vpc-name=test-vnet-13196
Annotations:  <none>
API Version:  runtime.cloud.antrea.io/v1alpha1
Kind:         VirtualMachine
Metadata:
  Creation Timestamp:  <nil>
  UID:                 3a7f4cbc-ab54-4a34-890c-51419a7b64a2
Status:
  Agented:         false
  Cloud Id:        /subscriptions/58bbb0ce-e26a-4bbc-a49c-284f63c25e2e/resourcegroups/test-rg/providers/microsoft.compute/virtualmachines/vm1
  Cloud Name:      vm1
  Cloud Vpc Id:    /subscriptions/58bbb0ce-e26a-4bbc-a49c-284f63c25e2e/resourcegroups/archana-rg/providers/microsoft.network/virtualnetworks/test-vnet
  Cloud Vpc Name:  test-vnet
  Network Interfaces:
    Ips:
      Address:       10.0.0.4
      Address Type:  InternalIP
      Address:       20.245.116.185
      Address Type:  ExternalIP
    Mac:             00-22-48-0A-00-8E
    Name:            /subscriptions/58bbb0ce-e26a-4bbc-a49c-284f63c25e2e/resourcegroups/archana-rg/providers/microsoft.network/networkinterfaces/vm1156
  Provider:          Azure
  Region:            westus
  State:             running
  Tags:
    Cloud:  azure
    Name:   vm1
Events:              <none>
```

## External Entity

For each imported `VirtualMachine` object, an `ExternalEntity` CR is created. All tags from the `VirtualMachine`
objects are imported onto `ExternalEntity` CR as labels, which can be used to configure ANPs.

### Sample ExternalEntity Output

```bash
kubectl describe ee virtualmachine-i-0c2e4054f435927a9 -n sample-ns
```

#### AWS

```yaml
Name:         virtualmachine-i-0c2e4054f435927a9
Namespace:    sample-ns
Labels:       nephe.antrea.io/cloud-region=us-west-1
              nephe.antrea.io/cloud-vm-name=ubuntu-vm
              nephe.antrea.io/cloud-vm-uid=i-0c2e4054f435927a9
              nephe.antrea.io/cloud-vpc-name=test-vpc
              nephe.antrea.io/cloud-vpc-uid=vpc-0882e6544e1bb0564
              nephe.antrea.io/kind=virtualmachine
              nephe.antrea.io/namespace=sample-ns
              nephe.antrea.io/owner-vm=i-0c2e4054f435927a9
              nephe.antrea.io/owner-vm-vpc=vpc-0882e6544e1bb0564
              nephe.antrea.io/tag-Name=ubuntu-vm
Annotations:  <none>
API Version:  crd.antrea.io/v1alpha2
Kind:         ExternalEntity
Metadata:
  Creation Timestamp:  2023-06-21T14:39:35Z
  Generation:          1
  Managed Fields:
    API Version:  crd.antrea.io/v1alpha2
    Fields Type:  FieldsV1
    fieldsV1:
      f:metadata:
        f:labels:
          .:
          f:nephe.antrea.io/cloud-region:
          f:nephe.antrea.io/cloud-vm-name:
          f:nephe.antrea.io/cloud-vm-uid:
          f:nephe.antrea.io/cloud-vpc-name:
          f:nephe.antrea.io/cloud-vpc-uid:
          f:nephe.antrea.io/kind:
          f:nephe.antrea.io/namespace:
          f:nephe.antrea.io/owner-vm:
          f:nephe.antrea.io/owner-vm-vpc:
          f:nephe.antrea.io/tag-KubernetesCluster:
          f:nephe.antrea.io/tag-Name:
      f:spec:
        .:
        f:endpoints:
        f:externalNode:
    Manager:         nephe-controller
    Operation:       Update
    Time:            2023-06-21T14:39:35Z
  Resource Version:  1717
  UID:               a85c6f4f-2d98-4bbc-b402-134bbe567dad
Spec:
  Endpoints:
    Ip:           172.20.34.127
    Ip:           54.151.31.160
  External Node:  nephe-controller
Events:           <none>
```

#### Azure

```bash
kubectl describe ee virtualmachine-vm1-11610 -n sample-ns
```

```yaml
# Output
Name:         virtualmachine-vm1-11610
Namespace:    sample-ns
Labels:       nephe.antrea.io/cloud-region=westus
              nephe.antrea.io/cloud-vm-name=vm1
              nephe.antrea.io/cloud-vm-uid=d33fa6ce-9a12-48bd-ac0e-d842a2f24838
              nephe.antrea.io/cloud-vpc-name=test-vnet
              nephe.antrea.io/cloud-vpc-uid=c440672a-47eb-462c-a567-4c6bc75eaabe
              nephe.antrea.io/kind=virtualmachine
              nephe.antrea.io/namespace=sample-ns
              nephe.antrea.io/owner-vm=vm1-11610
              nephe.antrea.io/owner-vm-vpc=test-vnet-13196
              nephe.antrea.io/tag-Cloud=azure
              nephe.antrea.io/tag-Name=vm1
Annotations:  <none>
API Version:  crd.antrea.io/v1alpha2
Kind:         ExternalEntity
Metadata:
  Creation Timestamp:  2023-04-17T19:50:18Z
  Generation:          1
  Managed Fields:
    API Version:  crd.antrea.io/v1alpha2
    Fields Type:  FieldsV1
    fieldsV1:
      f:metadata:
        f:labels:
          .:
          f:nephe.antrea.io/cloud-region:
          f:nephe.antrea.io/cloud-vm-name:
          f:nephe.antrea.io/cloud-vm-uid:
          f:nephe.antrea.io/cloud-vpc-name:
          f:nephe.antrea.io/cloud-vpc-uid:
          f:nephe.antrea.io/kind:
          f:nephe.antrea.io/namespace:
          f:nephe.antrea.io/owner-vm:
          f:nephe.antrea.io/owner-vm-vpc:
      f:spec:
        .:
        f:endpoints:
        f:externalNode:
    Manager:         nephe-controller
    Operation:       Update
    Time:            2023-04-17T19:50:18Z
  Resource Version:  556954
  UID:               f0309a24-632b-4ebd-bbeb-9b205151a311
Spec:
  Endpoints:
    Ip:           10.0.0.4
    Ip:           20.245.116.185
  External Node:  nephe-controller
Events:           <none>
```

### Labels

| Label                          | Description                                                                               | Azure                                                                  | AWS                                                  |
|--------------------------------|-------------------------------------------------------------------------------------------|------------------------------------------------------------------------|------------------------------------------------------|
| nephe.antrea.io/cloud-vm-uid   | Unique identifier of VM on cloud                                                          | VM `vmId` property                                                     | Instance ID                                          |
| nephe.antrea.io/cloud-vpc-uid  | Unique identifier of VPC on cloud                                                         | VNET `resourceGuid` property                                           | VPC ID                                               |
| nephe.antrea.io/cloud-vpc-name | VPC name on cloud                                                                         | VNET `name` property                                                   | VPC `Name` tag                                       |
| nephe.antrea.io/cloud-vm-name  | VM name on cloud                                                                          | VM `name` property                                                     | VM `Name` tag                                        |
| nephe.antrea.io/owner-vm       | VM object Name within Kubernetes cluster                                                  | VM `name` property suffixed with ascii sum of VM `Resource ID`         | Instance ID                                          |
| nephe.antrea.io/owner-vm-vpc   | VPC object(belongs to VM) Name within Kubernetes cluster                                  | VNET `name` property suffixed with ascii sum of VNET `Resource ID`     | VPC ID                                               |
 | nephe.antrea.io/cloud-region   | Region configured in `CloudProviderAccount` CR and VMs are imported from this region      | Region from configured `CloudProviderAccount` CR                       | Region from configured `CloudProviderAccount` CR     |
| nephe.antrea.io/tag-Name       | Prefix "tag-" indicates VM Tags from cloud, value of label is the corresponding tag value | Tags on cloud with key name "Name"                                     | Tag on cloud with key name "Name"                    |

### Cloud Tags

The tags on the `VirtualMachine` object are imported as labels in `ExternalEntity` CR. Label name starting with a prefix
"tag-" are the `tags` from imported `VirtualMachine` objects. In the above example, `nephe.antrea.io/tag-Cloud` and
`nephe.antrea.io/tag-Name` labels correspond to tags in `VirtualMachine` objects.

#### Usage

Labels in `ExternalEntity` can be directly used in To, From, AppliedTo section as externalEntitySelectors in Antrea
`NetworkPolicy`. Following ANP demonstrates use of the label `nephe.antrea.io/tag-Name` in an `appliedTo` field.

```bash

cat <<EOF | kubectl apply -f -
apiVersion: crd.antrea.io/v1alpha1
kind: NetworkPolicy
metadata:
name: vm-anp
namespace: sample-ns
spec:
appliedTo:
- externalEntitySelector:
  matchLabels:
    nephe.antrea.io/tag-Name: ubuntu-vm
  ingress:
- action: Allow
  from:
    - ipBlock:
      cidr: 0.0.0.0/0
      ports:
    - protocol: TCP
      port: 22

EOF
```

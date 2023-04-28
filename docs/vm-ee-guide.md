# Onboard

## Table of Contents

<!-- toc -->
- [VirtualMachines](#virtualmachines)
    - [Labels](#labels)
    - [Cloud Tags](#cloud-tags)
    - [Sample VirtualMachine Output](#sample-virtualmachine-output)
        - [AWS](#aws)
        - [Azure](#azure)
- [ExternalEntity](#externalentity)
    - [Sample ExternalEntity Output](#sample-externalentity-output)
        - [Azure](#azure-1)
    - [Labels](#labels-1)
    - [Cloud Tags](#cloud-tags-1)
        - [Usage](#usage)
<!-- /toc -->

## VirtualMachines

VirtualMachines are onboarded by specifying the matching criteria in `CloudEntitySelector` CR. The virtual machines from
cloud are imported as `VirtualMachine` objects in `CloudEntitySelector` namespace. `VirtualMachine` object `Name` is a
unique resource name within the kubernetes cluster. In Azure, ascii sum of the characters in VM Resource ID added as
suffix to name of Virtual Machine and in AWS, `instance ID` of Virtual Machine is used as `Name`.

### Labels

Meaning of some label fields in `VirtualMachine` objects differ based on the cloud provider type.

| Label        | Description                               | Azure                                    | AWS                          |
|--------------|-------------------------------------------|------------------------------------------|------------------------------|
| cloud-vm-uid | Unique identifier of VM                   | `vmId` field on cloud                    | `Instance ID` of VM on cloud |
| cloud-vpc-id | Unique identifier of VPC                  | `resourceGuid` field on cloud            | `VPC ID` of VPC in cloud     |
| vpc-name     | VPC object Name within Kubernetes cluster | vpc `Name` and hash of vpc resource `id` | `VPC ID` of VPC in cloud     |

Following labels are generic and related to user configuration.

1. `cpa-name`: Name of `CloudProviderAccount` CR to which the imported vm belongs to.
2. `cpa-namespace`: Namespace of `CloudProviderAccount` CR to which the imported vm belongs to.

### Cloud Tags

`VirtualMachine` object tags are converted to labels in `ExternalEntity` CR, hence only the tags which comply to
Kubernetes labels format are imported as tags in vm object.

*Key field specifications*:

- Required field
- Must be 63 characters or fewer(4 characters are allocated to the prefix "tag-" in labels, hence only 59 characters
  can be used in key)
- First and last character must be an alphanumeric character ([a-z0-9A-Z]), with dashes (-), underscores (_), dots (.),
  and alphanumerics in between.

*Value field specifications*:

- Optional field
- Must be 63 characters or fewer
- Unless empty, must begin and end with an alphanumeric character, could contain dashes (-), underscores (_), dots (.),
  and alphanumerics between.

### Sample VirtualMachine Output

#### AWS

```bash
kubectl describe vm i-0c2e4054f435927a9 -n sample-ns
```

```text
# Output
Name:         i-0c2e4054f435927a9
Namespace:    sample-ns
Labels:       nephe.io/cloud-vm-uid=i-0c2e4054f435927a9
              nephe.io/cloud-vpc-uid=vpc-0882e6544e1bb0564
              nephe.io/cpa-name=cloudprovideraccount-aws-sample
              nephe.io/cpa-namespace=sample-ns
              nephe.io/vpc-name=vpc-0882e6544e1bb0564
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

```text
# Output
Name:         vm1-11610
Namespace:    sample-ns
Labels:       nephe.io/cloud-vm-uid=d33fa6ce-9a12-48bd-ac0e-d842a2f24838
              nephe.io/cloud-vpc-uid=c440672a-47eb-462c-a567-4c6bc75eaabe
              nephe.io/cpa-name=cloudprovideraccount-azure-sample
              nephe.io/cpa-namespace=sample-ns
              nephe.io/vpc-name=test-vnet-13196
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

## ExternalEntity

For each imported `VirtualMachine`, an `ExternalEntity` CR is created. All the labels and tags from the `VirtualMachine`
objects are imported on to `ExternalEntity` CR as labels, which can be used to configure ANPs.

### Sample ExternalEntity Output

#### Azure

```bash
kubectl describe ee virtualmachine-vm1-11610 -n sample-ns
```

```text
# Output
Name:         virtualmachine-vm1-11610
Namespace:    sample-ns
Labels:       nephe.io/cloud-region=westus
              nephe.io/cloud-vm-name=vm1
              nephe.io/cloud-vm-uid=d33fa6ce-9a12-48bd-ac0e-d842a2f24838
              nephe.io/cloud-vpc-name=test-vnet
              nephe.io/cloud-vpc-uid=c440672a-47eb-462c-a567-4c6bc75eaabe
              nephe.io/kind=virtualmachine
              nephe.io/namespace=sample-ns
              nephe.io/owner-vm=vm1-11610
              nephe.io/owner-vm-vpc=test-vnet-13196
              nephe.io/tag-Cloud=azure
              nephe.io/tag-Name=vm1
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
          f:nephe.io/cloud-region:
          f:nephe.io/cloud-vm-name:
          f:nephe.io/cloud-vm-uid:
          f:nephe.io/cloud-vpc-name:
          f:nephe.io/cloud-vpc-uid:
          f:nephe.io/kind:
          f:nephe.io/namespace:
          f:nephe.io/owner-vm:
          f:nephe.io/owner-vm-vpc:
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

Labels with a `cloud-` prefix reflect the values fetched from cloud.

| Label                   | Description                                              | Azure                                    | AWS                          |
|-------------------------|----------------------------------------------------------|------------------------------------------|------------------------------|
| nephe.io/cloud-vm-uid   | Unique identifier of VM                                  | `vmId` field on cloud                    | `Instance ID` of VM on cloud |
| nephe.io/cloud-vpc-uid  | Unique identifier of VPC                                 | `resourceGuid` field on cloud            | `VPC ID` of VPC in cloud     |
| nephe.io/cloud-vpc-name | User assigned VPC name on the cloud                      | `Name` field on cloud                    | `Name` tag                   |
| nephe.io/cloud-vm-name  | User assigned VM name on the cloud                       | `Name` field on cloud                    | `Name` tag                   |
| nephe.io/owner-vm       | VM object Name within Kubernetes cluster                 | vm `Name` and hash of vm resource `id`   | `Instance ID` of VM on cloud | 
| nephe.io/owner-vm-vpc   | VPC object(belongs to VM) Name within Kubernetes cluster | vpc `Name` and hash of vpc resource `id` | `VPC ID` of VPC in cloud     | 


nephe.io/cloud-region: Region configured in CPA and VMs are imported from this region.

### Cloud Tags

The tags on the `VirtualMachine` object are imported as labels in `ExternalEntity` CR. Label name starting with a prefix
"tag-" are the `tags` from imported `VirtualMachine` objects. In the above example, `nephe.io/tag-Cloud` and
`nephe.io/tag-Name` labels correspond to tags in `VirtualMachine` objects.

#### Usage

Labels in `ExternalEntity` can be directly used in To, From, AppliedTo section as externalEntitySelectors in Antrea
`NetworkPolicys`. Following ANP demonstrates use of the label `nephe.io/tag-Name` in an `appliedTo` field.

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
    nephe.io/tag-Name: ubuntu-vm
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
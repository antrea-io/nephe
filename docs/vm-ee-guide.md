

### VirtualMachine Import
Based on the `CloudEntitySelector` configured, virtual machines are polled
from cloud and imported as `VirtualMachine` objects in `CloudEntitySelector`
namespace.

`VirtualMachine` object `Name` is a unique resource name within the kubernetes cluster.
In Azure, ascii sum of the characters in VM Resource ID added as suffix to name of Virtual Machine and 
in AWS, `instance ID` of Virtual Machine is used as `Name`.

#### labels
Meaning of some label fields in `VirtualMachine` objects differ based on the cloud provider type.

| VM object label | Meaning of label                              | Azure                                                                                   | AWS                         |
|-----------------|-----------------------------------------------|-----------------------------------------------------------------------------------------|-----------------------------|
| cloud-vm-uid    | unique id identifying the VM                  | vmId field on cloud                                                   | VM instance ID  on cloud    |
| cloud-vpc-id    | unique id identifying the VPC                 | resourceGuid field on cloud                                                             | VPC ID field of VPC in cloud |
 | vpc-name        | Unique name of vpc within kuebernetes cluster | ascii sum of the characters in VPC Resource ID are added as a suffix to name of VPC     | VPC ID on cloud   |

Following labels are generic and related to user configuration.

- `cpa-name`: It is the name of CloudProviderAccount CR to which the imported vm belongs to.
- `cpa-namespace`: It is the namespace of CloudProviderAccount CR to which
the imported vm belongs to.

#### Tags in Status field:
`VirtualMachine` object tags are converted to labels in `ExternalEntity` CR, hence only the tags which comply to kubernetes label format are imported as tags in vm object.

Key field specifications:

 - required field
 - must be 63 characters or fewer(4 characters are allocated to the prefix "tag-" in labels, hence only 59 characters can be used in key)
 - beginning and ending with an alphanumeric character ([a-z0-9A-Z]) with dashes (-), underscores (_), dots (.), and alphanumerics between.

Value field specifications:

 - optional field
 - must be 63 characters or fewer
 - unless empty, must begin and end with an alphanumeric character, could contain dashes (-), underscores (_), dots (.), and alphanumerics between.

#### `VirtualMachine` describe output on AWS
```bash
kubectl describe vm i-0c2e4054f435927a9 -n sample-ns
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

#### `VirtualMachine` describe output on Azure

```bash
kubectl describe vm vm1-11610 -n sample-ns
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

### External Entity
For each imported `VirtualMachine`, External Entity is created. Labels in `ExternalEntity` can be used in
ANPs as source or destination.

#### External Entity describe output on Azure:
```bash
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

#### labels
label name with a prefix "cloud-" are the values imported directly from cloud.

| VM object field name    | Meaning of field                                         | Azure                                                                         | AWS                         |
|-------------------------|----------------------------------------------------------|-------------------------------------------------------------------------------|-----------------------------|
| nephe.io/cloud-vm-uid   | unique id identifying the VM                             | vmId field on cloud                                                           | VM instance ID  on cloud    |
| nephe.io/cloud-vpc-uid  | unique id identifying the VPC                            | resourceGuid field on cloud                                                   | VPC ID field of VPC in cloud |
| nephe.io/cloud-vpc-name | User assigned name on the cloud for the VPC              | name field on cloud                                                           | `name` tag                  |
 | nephe.io/cloud-vm-name | User assigned name on the cloud for the VM               | name field on cloud                                                           | `name` tag                  |
 | nephe.io/owner-vm      | unique resource name of vm within the kubernetes cluster | ascii sum of the characters in VM Resource ID added as suffix to name of VM   | VM Instance ID              | 
 | nephe.io/owner-vm-vpc | unique resource name of vpc(vm belongs) within the kubernetes cluster | ascii sum of the characters in VPC Resource ID added as suffix to name of VPC | VPC ID | 


nephe.io/cloud-region: Region configured in CPA and VMs are imported from this region.

Label name starting with a prefix "tag-" are the `tags` from imported
`vm` objects. In the above EE describe output nephe.io/tag-Cloud and nephe.io/tag-Name labels correspond to
`Tags` in VM objects.

Labels in `EE` can be directly used in To, From, AppliedTo section as externalEntitySelectors in `ANPs`.

Following ANP uses "nephe.io/tag-Name" in appliedTo.
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





# Deploying Nephe in EKS

## Table of Contents

<!-- toc -->
- [Prerequisites](#prerequisites)
- [Create an EKS cluster via terraform](#create-an-eks-cluster-via-terraform)
  - [Setup Terraform Environment](#setup-terraform-environment)
  - [Create an EKS cluster](#create-an-eks-cluster)
  - [Deploy Nephe Controller](#deploy-nephe-controller)
  - [Interact with EKS cluster](#interact-with-eks-cluster)
  - [Display EKS attributes](#display-eks-attributes)
  - [Destroy EKS cluster](#destroy-eks-cluster)
- [Create AWS VMs](#create-aws-vms)
  - [Setup Terraform Environment](#setup-terraform-environment-1)
  - [Create VMs](#create-vms)
  - [Display VPC attributes](#display-vpc-attributes)
  - [Destroy VMs](#destroy-vms)
<!-- /toc -->

## Prerequisites

1. Install and configure [AWS CLI](https://docs.aws.amazon.com/cli/latest/userguide/cli-chap-install.html).
2. Install [Terraform](https://learn.hashicorp.com/terraform/getting-started/install.html). Recommend v1.2.2.
3. Install `jq`, `pv`, and `bzip2`.
4. Set the below environment variables.

   ```bash
   export TF_VAR_owner=YOUR_NAME
   export TF_VAR_eks_cluster_iam_role_name=YOUR_EKS_ROLE
   export TF_VAR_eks_iam_instance_profile_name=YOUR_EKS_WORKER_NODE_PROFILE
   export TF_VAR_eks_key_pair_name=YOUR_KEY_PAIR_TO_ACCESS_WORKER_NODE
   ```

   - `TF_VAR_owner` may be set so that you can identify your own cloud resources.
      It should be one word, with no spaces and in lower case.
   - `TF_VAR_eks_cluster_iam_role_name` may be created following this [AWS guide](https://docs.aws.amazon.com/eks/latest/userguide/service_IAM_role.html#create-service-role).
   - `TF_VAR_eks_iam_instance_profile_name` may be created following this [AWS guide](https://docs.aws.amazon.com/eks/latest/userguide/create-node-role.html).
   - `TF_VAR_eks_key_pair_name` must be configured following this
      [AWS cli documentation](https://awscli.amazonaws.com/v2/documentation/api/latest/reference/ec2/import-key-pair.html).
      The public key imported to AWS should be `~/.ssh/id_rsa.pub`. Follow the
      documentation to create a new key then import if this key doesn't exist.
      This key pair will be used to access worker node via ssh.

## Create an EKS cluster via terraform

### Setup Terraform Environment

```bash
./hack/install-cloud-tools.sh
```

The [install cloud tools script](../hack/install-cloud-tools.sh) copies the
required bash and terraform scripts to the user home directory, under
`~/terraform/`.

### Create an EKS cluster

Create an EKS cluster using the provided terraform scripts. Once the EKS cluster
is created, worker nodes are accessible via their external IP using ssh.
Terraform state files and other runtime info will be stored under
`~/tmp/terraform-eks/`. You can also create an EKS cluster in other ways and
deploy prerequisites manually.

This also deploys `cert-manager v1.8.2` and `Antrea v1.8`.

```bash
~/terraform/eks create
```

### Deploy Nephe Controller

To deploy the latest version of nephe (built from the main branch), use the
checked-in [deployment yaml](../config/nephe.yml):

```bash
~/terraform/eks kubectl apply -f https://raw.githubusercontent.com/antrea-io/nephe/main/config/nephe.yml
```

### Interact with EKS cluster

Issue kubectl commands to EKS cluster using the helper scripts. To run kubectl
commands directly, set `KUBECONFIG` environment variable.

```bash
~/terraform/eks kubectl ...
export KUBECONFIG=~/tmp/terraform-eks/kubeconfig
```

Loading locally built `antrea/nephe` images to EKS cluster.

```bash
docker tag antrea/nephe:latest projects.registry.vmware.com/antrea/nephe:latest
~/terraform/eks load projects.registry.vmware.com/antrea/nephe
```

### Display EKS attributes

```bash
~/terraform/eks output
```

### Destroy EKS cluster

```bash
~/terraform/eks destroy
```

## Create AWS VMs

Additionally, you can also create compute VPC with 3 VMs using terraform
scripts for testing purpose. Each VM will have a public IP and an Apache Tomcat
server deployed on port 80. Use curl `<PUBLIC_IP>:80` to access a sample web
page. Create or obtain AWS key and secret and configure the below environment
variables.

```bash
export TF_VAR_region=YOUR_REGION
export TF_VAR_aws_access_key_id=YOUR_AWS_KEY
export TF_VAR_aws_access_key_secret=YOUR_AWS_KEY_SECRET
export TF_VAR_aws_key_pair_name=YOU_AWS_KEY_PAIR
```

### Setup Terraform Environment

```bash
./hack/install-cloud-tools.sh
```

### Create VMs

```bash
~/terraform/aws-tf create
```

Terraform state files and other runtime info will be stored under
`~/tmp/terraform-aws/`

### Display VPC attributes

```bash
~/terraform/aws-tf output
```

### Destroy VMs

```bash
~/terraform/aws-tf destroy
```

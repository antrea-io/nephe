# Deploying Nephe in Azure AKS

## Table of Contents

<!-- toc -->
- [Prerequisites](#prerequisites)
- [Create an AKS cluster via terraform](#create-an-aks-cluster-via-terraform)
  - [Setup Terraform Environment](#setup-terraform-environment)
  - [Create an AKS cluster](#create-an-aks-cluster)
  - [Deploy Nephe Controller](#deploy-nephe-controller)
  - [Interact with AKS cluster](#interact-with-aks-cluster)
  - [Display AKS attributes](#display-aks-attributes)
  - [Destroy AKS cluster](#destroy-aks-cluster)
- [Create Azure VMs](#create-azure-vms)
  - [Setup Terraform Environment](#setup-terraform-environment-1)
  - [Create VMs](#create-vms)
  - [Display VNET attributes](#display-vnet-attributes)
  - [Destroy VMs](#destroy-vms)
<!-- /toc -->

## Prerequisites

1. Install [Kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/) v1.24+.
2. Install [Terraform](https://learn.hashicorp.com/terraform/getting-started/install.html). Recommend v1.2.2.
3. Install `jq` and `pv`.
4. Create or obtain an azure service principal and set the below environment
   variables. Please refer to [Azure documentation](https://docs.microsoft.com/en-us/azure/active-directory/develop/howto-create-service-principal-portal#create-an-azure-active-directory-application)
   for more information.

   ```bash
   export TF_VAR_azure_client_id=YOUR_SERVICE_PRINCIPAL_ID
   export TF_VAR_azure_client_secret=YOUR_SERVICE_PRINCIPAL_SECRET
   export TF_VAR_azure_client_subscription_id=YOUR_SUBCRIPTION_ID
   export TF_VAR_azure_client_tenant_id=YOUR_TENANT_ID
   export TF_VAR_owner=YOUR_NAME
   ```

   Note: `TF_VAR_owner` may be set so that you can identify your own cloud
   resources. It should be one word, with no spaces and in lower case.

## Create an AKS cluster via terraform

### Setup Terraform Environment

```bash
./hack/install-cloud-tools.sh
```

The [install cloud tools](../hack/install-cloud-tools.sh) script copies the
required bash and terraform scripts to the user home directory, under
`~/terraform/`.

### Create an AKS cluster

Create an AKS cluster using the provided terraform scripts. Once the AKS cluster
is created, worker nodes are accessible via their external IP using ssh.
Terraform state files and other runtime info will be stored under
`~/tmp/terraform-aks/`. You can also create an AKS cluster in other ways and
deploy prerequisites manually.

This also deploys `cert-manager v1.8.2` and `antrea v1.9`.

```bash
~/terraform/aks create
```

### Deploy Nephe Controller

To deploy a released version of Nephe, pick a deployment manifest from the
[list of releases](https://github.com/antrea-io/nephe/releases). For any given
release <TAG> (e.g. v0.1.0), you can deploy Nephe as follows:

```bash
kubectl apply -f https://github.com/antrea-io/nephe/releases/download/<TAG>/nephe.yml
```

To deploy the latest version of Nephe (built from the main branch), use the
checked-in [deployment yaml](../config/nephe.yml):

```bash
~/terraform/aks kubectl apply -f https://raw.githubusercontent.com/antrea-io/nephe/main/config/nephe.yml
```

### Interact with AKS cluster

Issue kubectl commands to AKS cluster using the helper scripts. To run kubectl
commands directly, set `KUBECONFIG` environment variable.

```bash
~/terraform/aks kubectl ...
export KUBECONFIG=~/tmp/terraform-aks/kubeconfig
```

Loading locally built `antrea/nephe` image to AKS cluster.

```bash
docker tag antrea/nephe:latest projects.registry.vmware.com/antrea/nephe:latest
~/terraform/aks load projects.registry.vmware.com/antrea/nephe
```

### Display AKS attributes

```bash
~/terraform/aks output
```

### Destroy AKS cluster

```bash
~/terraform/aks destroy
```

## Create Azure VMs

Additionally, you can also create compute VNET with 3 VMs using terraform
scripts for testing purpose. Each VM will have a public IP and an Apache Tomcat
server deployed on port 80. Use curl `<PUBLIC_IP>:80` to access a sample web
page. Create or obtain Azure Service Principal credential and configure the
below environment variables, see [Prerequisites](#Prerequisites) section for
more details.

```bash
export TF_VAR_azure_client_id=YOUR_SERVICE_PRINCIPAL_ID
export TF_VAR_azure_client_secret=YOUR_SERVICE_PRINCIPAL_SECRET
export TF_VAR_azure_client_subscription_id=YOUR_SUBCRIPTION_ID
export TF_VAR_azure_client_tenant_id=YOUR_TENANT_ID
export TF_VAR_owner=YOUR_NAME
```

### Setup Terraform Environment

```bash
./hack/install-cloud-tools.sh
```

### Create VMs

```bash
~/terraform/azure-tf create
```

Terraform state files and other runtime info will be stored under
`~/tmp/terraform-azure/`.

### Display VNET attributes

```bash
~/terraform/azure-tf output
```

### Destroy VMs

```bash
~/terraform/azure-tf destroy
```

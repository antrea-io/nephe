# Running the Nephe tests

## Nephe test suite

We run 2 different categories of tests. The table below shows the different
categories of tests and indicates how to run them locally:

| Test category                 | Location                         | How to run locally                                          | Automation                                            |
| ----------------------------- |----------------------------------|-------------------------------------------------------------|-------------------------------------------------------|
| **unit tests**                | most Go packages                 | `make unit-test`                                            | [Github Actions](https://github.com/features/actions) |
| **integration tests**         | [test/integration](integration)  | `make integration-test-aws`/`make integration-test-azure`   | [Jenkins](../ci/jenkins/README.md)                    |

## Unit Test

Unit tests are placed under same directories with the implementations. The test
package name is `PACKAGE_test`. For instance, the unit test files `xxx_test.go`
under `pkg/cloud-provider` have the package name of `cloudprovider_test`. Unit
tests uses [go mock](https://github.com/golang/mock) to generate mock packages.
To generate mock code for a package, the package must implement interfaces, and
add `package/interfaces` to [mockgen](../hack/mockgen.sh).

To generate mock:

```bash
make mock
```

To run unit tests:

```bash
make unit-test
```

## Integration Test

The [test/integration](integration) directory contains all the integration tests. It uses
Ginkgo as the underlying framework. The integration tests can be run on a kind
cluster or on a AKS/EKS cluster. The keywords `focusAws`, `focusAzure` and
`focusCloud` are used in descriptions on any level of a test spec, to indicate
if this test spec should be run in zero or more test suites.

### Dependencies

1. Install [ginkgo v1.16.5](https://onsi.github.io/ginkgo/).

```bash
go install github.com/onsi/ginkgo/ginkgo@v1.16.5
export PATH=$PATH:$(go env GOPATH)/bin
```

2. Download the below docker images.

```bash
docker pull kennethreitz/httpbin
docker pull byrnedo/alpine-curl
```

3. Create a Kind cluster

```bash
ci/kind/kind-setup.sh create kind
```

### Running AWS Integration Test

Set the following variables to allow terraform scripts to create a compute VPC
with 3 VMs.

```bash
export TF_VAR_aws_access_key_id=YOUR_AWS_KEY
export TF_VAR_aws_access_key_secret=YOUR_AWS_KEY_SECRET
export TF_VAR_aws_key_pair_name=YOU_AWS_KEY_PAIR
export TF_VAR_region=YOUR_AWS_REGION
export TF_VAR_owner=YOUR_ID
```

To run integration test,

```bash
make integration-test-aws
```

### Running Azure Integration Test

Set the following variables to allow terraform scripts to create a compute VNET
with 3 VMs.

```bash
export TF_VAR_azure_client_id=YOUR_AZURE_CLIENT_ID
export TF_VAR_azure_client_subscription_id=YOUR_AZURE_CLIENT_SUBSCRIPTION_ID
export TF_VAR_azure_client_secret=YOUR_AZURE_CLIENT_SECRET
export TF_VAR_azure_client_tenant_id=YOUR_AZURE_TENANT_ID
```

To run integration test:

```bash
make integration-test-azure
```

### Running Integration Test on an existing Kind cluster

```bash
make
kind load docker-image antrea/nephe
make integration-test-aws
make integration-test-azure
```

### Running Integration Test on Cloud cluster

-  Deploy Nephe on an EKS cluster using [the EKS installation guide](../docs/eks-installation.md).
   Run integration tests on EKS cluster.

   ```bash
   ci/bin/integration.test -ginkgo.v -ginkgo.focus=".*test-cloud-cluster.*" -kubeconfig=$HOME/tmp/terraform-eks/kubeconfig -cloud-provider=AWS -cloud-cluster
   ```

    **Note**: If you want to run the test using AWS IAM role, set the variable.

    ```bash
    export TF_VAR_nephe_controller_role_arn=YOUR_IAM_ROLE
    ```

-  Deploy Nephe on an AKS cluster using [the AKS installation guide](../docs/eks-installation.md).
   Run integration tests on AKS cluster.

   ```bash
   ci/bin/integration.test -ginkgo.v -ginkgo.focus=".*test-cloud-cluster.*" -kubeconfig=$HOME/tmp/terraform-aks/kubeconfig -cloud-provider=Azure -cloud-cluster
   ```

**Note**: If Cloud cluster is not created using nephe [terraform scripts](../hack/terraform),
then update the `-kubeconfig` argument with your kubeconfig file.



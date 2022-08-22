# Developer Guide

Thank you for taking the time out to contribute to project Nephe!.
Nephe project scaffold is created via [Kubebuilder](https://github.com/kubernetes-sigs/kubebuilder).
To install `kubebuilder version 3`, please refer to
[kubebuilder quick start guide](https://book.kubebuilder.io/quick-start.html#installation).

## Prerequisites

The following tools are required to build, test and run `Nephe Controller`.

- [Docker](https://docs.docker.com/install/) version 20.10.17.
- [Golang](https://go.dev/dl/) version 1.17.
- [Kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/) version 1.24.1.
- [Kind](https://kind.sigs.k8s.io/docs/user/quick-start/) version 0.12.0.

NOTE: Only the above versions are validated by Nephe.

## Build

To build the `antrea/nephe` image, you can simply do:

1. Checkout your branch and `cd` into it.
2. Run `make` .

The `nephe-controller` binary is located in `./bin` directory and the docker
image `antrea/nephe:latest` is created or updated in the local docker repository.

To deploy the locally built image, tag and load the docker image on your cluster.

```bash
docker tag antrea/nephe:latest projects.registry.vmware.com/antrea/nephe:latest
# Load the projects.registry.vmware.com/antrea/nephe image on your cluster.
```

## Deployment

Nephe manifest deploys one `Nephe controller` Deployment. All the
`Nephe Controller` related resources are namespaced under `nephe-system`.

```bash
kubectl apply -f config/nephe.yml
```

```bash
kubectl get deployment -A
# Output
NAMESPACE            NAME                      READY   UP-TO-DATE   AVAILABLE   AGE
cert-manager         cert-manager              1/1     1            1           41m
cert-manager         cert-manager-cainjector   1/1     1            1           41m
cert-manager         cert-manager-webhook      1/1     1            1           41m
kube-system          antrea-controller         1/1     1            1           41m
nephe-system         nephe-controller          1/1     1            1           40m
kube-system          coredns                   2/2     2            2           43m
local-path-storage   local-path-provisioner    1/1     1            1           43m
```

## Notice

Project Nephe follows the Antrea Developer Guide. Also check out
[Nephe documents](docs) for more information about nephe. The following sections
have different content from [Antrea Developer Guide](https://github.com/antrea-io/antrea/blob/main/CONTRIBUTING.md).
Please read them carefully when referring to
[Antrea Developer Guide](https://github.com/antrea-io/antrea/blob/main/CONTRIBUTING.md).

## GitHub Workflow

When following [GitHub Workflow in Antrea](https://github.com/antrea-io/antrea/blob/main/CONTRIBUTING.md#github-workflow),
please make sure to change the name of repository from antrea to nephe.

## Getting your PR verified by CI

In Nephe, we have trigger phrases to run AWS and Azure tests on AKS, EKS and
Kind cluster. Only Nephe [maintainers](MAINTAINERS.md#nephe-maintainers) will
be able to add the trigger phrases in the PR. Please run the required tests
locally for your PR, and then reach out to maintainers.

Here are the trigger phrases for individual checks:

- `/nephe-test-e2e-aws`: Run end-to-end AWS tests on a Kind cluster with AWS VMs
- `/nephe-test-e2e-azure`: Run end-to-end Azure tests on a Kind cluster with Azure VMs
- `/nephe-test-e2e-eks`: Run end-to-end AWS tests on EKS cluster with AWS VMs
- `/nephe-test-e2e-aks`: Run end-to-end Azure tests on AKS cluster with Azure VMs

Here are the trigger phrases for groups of checks:

- `/nephe-test-e2e-kind`: Run end-to-end tests on a Kind cluster.
- `/nephe-test-e2e-all`: Runs all the end-to-end tests.

For more information about the tests we run as part of CI, please refer to
[ci/jenkins/README.md](ci/jenkins/README.md).

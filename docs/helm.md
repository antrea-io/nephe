# Installing Nephe with Helm

## Table of Contents

<!-- toc -->
- [Prerequisites](#prerequisites)
- [Installation](#installation)
<!-- /toc -->

Starting with Nephe v0.4, Nephe can be installed and updated using
[Helm](https://helm.sh/).

Helm installation is currently considered Alpha.

## Prerequisites

* Ensure that Helm 3 is [installed](https://helm.sh/docs/intro/install/). We
  recommend using a recent version of Helm if possible. Refer to the [Helm
  documentation](https://helm.sh/docs/topics/version_skew/) for compatibility
  between Helm and Kubernetes versions.
* Add the Antrea Helm chart repository:

  ```bash
  helm repo add antrea https://charts.antrea.io
  helm repo update
  ```

## Installation

To install the Nephe Helm chart, use the following command:

```bash
helm install nephe antrea/nephe --namespace nephe-system
```

This will install the latest available version of Nephe. You can also install a
specific version of Nephe (>= v0.4) with `--version <TAG>`.

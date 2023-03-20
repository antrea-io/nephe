# nephe

![Version: 0.5.0-dev](https://img.shields.io/badge/Version-0.5.0--dev-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: latest](https://img.shields.io/badge/AppVersion-latest-informational?style=flat-square)

Antrea managed security policies in the public cloud

**Homepage:** <https://antrea.io/>

## Source Code

* <https://github.com/antrea-io/nephe>

## Requirements

Kubernetes: `>= 1.16.0-0`

| Repository | Name | Version |
|------------|------|---------|
|  | crds | 0.5.0-dev |

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| cloudResourcePrefix | string | `"nephe"` | Specifies the prefix to be used while creating cloud resources. |
| cloudSyncInterval | int | `300` | Specifies the interval (in seconds) to be used for syncing cloud resources with controller. |
| crds | object | `{"enabled":true}` | Enable/Disable Nephe CRDs dependent chart. |
| image | object | `{"pullPolicy":"IfNotPresent","repository":"projects.registry.vmware.com/antrea/nephe","tag":""}` | Container image to use for Nephe Controller. |

----------------------------------------------
Autogenerated from chart metadata using [helm-docs v1.7.0](https://github.com/norwoodj/helm-docs/releases/v1.7.0)
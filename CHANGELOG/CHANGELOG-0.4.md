# Changelog 0.4

## 0.4.0 - 2023-03-20

### Added

- Add Config Map for Nephe controller. ([#111](https://github.com/antrea-io/nephe/pull/111) [#151](https://github.com/antrea-io/nephe/pull/151), [@Nithish555])
- Add startup dependency on each CR controller. ([#103](https://github.com/antrea-io/nephe/pull/103), [@Anandkumar26])
- Add Antrea NetworkPolicy details on the description field of cloud security group rules. ([#132](https://github.com/antrea-io/nephe/pull/132), [@Anandkumar26])
- Add managed field in the `vpc` object to indicate if VPC is managed. ([#123](https://github.com/antrea-io/nephe/pull/123), [@reachjainrahul])
- Add rest handler to watch `vpc` objects. ([#110](https://github.com/antrea-io/nephe/pull/110), [@bangqipropel])
- Release a new Nephe Helm chart for each Nephe release. ([#158](https://github.com/antrea-io/nephe/pull/158), [@reachjainrahul])

### Changed

- Move VPC info from Spec to Status in `vpc` object. ([#118](https://github.com/antrea-io/nephe/pull/118), [@reachjainrahul])
- Upgrade AWS and Azure SDKs. ([#126](https://github.com/antrea-io/nephe/pull/126), [@archanapholla] [@Anandkumar26])
- Update CRD
  * Add endpoint URL in `CloudProviderAccount` for AWS. ([#116](https://github.com/antrea-io/nephe/pull/116), [@reachjainrahul])
  * Add region field in `VirtualMachine`. ([#150](https://github.com/antrea-io/nephe/pull/150), [@shenmo3])
- Optimize virtual machines polling from cloud. ([#130](https://github.com/antrea-io/nephe/pull/130), [@archanapholla] [@Anandkumar26])
- Update terraform scripts to deploy AKS and EKS clusters with K8s version 1.25. ([#144](https://github.com/antrea-io/nephe/pull/144), [@Anandkumar26])

### Fixed

- Fix issues found in the controller scale test. ([#131](https://github.com/antrea-io/nephe/pull/131), [@reachjainrahul])
- Fix issues found during syncing cloud resources with nephe controller. ([#90](https://github.com/antrea-io/nephe/pull/90) [#121](https://github.com/antrea-io/nephe/pull/121), [@archanapholla])
- Fix stale `VirtualMachinePolicy` object issue for Azure and add integration test coverage. ([#108](https://github.com/antrea-io/nephe/pull/108), [@shenmo3])
- Cleanup unnecessary RBAC from nephe manifest. ([#148](https://github.com/antrea-io/nephe/pull/148), [@reachjainrahul])

[@Anandkumar26]: https://github.com/Anandkumar26
[@archanapholla]: https://github.com/archanapholla
[@bangqipropel]: https://github.com/bangqipropel
[@Nithish555]: https://github.com/Nithish555
[@reachjainrahul]: https://github.com/reachjainrahul
[@shenmo3]: https://github.com/shenmo3

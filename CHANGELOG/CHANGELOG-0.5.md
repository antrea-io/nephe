# Changelog 0.5

## 0.5.0 - 2023-06-02

### Added

- Add AWS session token support in AWS credentials. ([#180](https://github.com/antrea-io/nephe/pull/180), [@reachjainrahul])
- Add new labels and update existing labels on `VirtualMachine` and `Vpc` objects to have `nephe.antrea.io` suffix. ([#184](https://github.com/antrea-io/nephe/pull/184) [#223](https://github.com/antrea-io/nephe/pull/223), [@archanapholla] [@reachjainrahul])
- Add `Secret` watcher to handle AWS and Azure credentials updates. ([#169](https://github.com/antrea-io/nephe/pull/169) [#210](https://github.com/antrea-io/nephe/pull/210) [#231](https://github.com/antrea-io/nephe/pull/231), [@Anandkumar26] [@Nithish555])
- Add support for per-rule level appliedTo in Antrea `NetworkPolicy`. ([#227](https://github.com/antrea-io/nephe/pull/227), [@reachjainrahul])

### Changed

- Upgrade Antrea supported version to v1.12. ([#238](https://github.com/antrea-io/nephe/pull/238), [@reachjainrahul])
- Move VM objects from CRD to an in-memory cache and expose them via aggregated API server. ([#167](https://github.com/antrea-io/nephe/pull/167), [@reachjainrahul] [@Anandkumar26] [@archanapholla] [@shenmo3])
- Use Azure network interface API client for real-time data instead of resource graph query. ([#190](https://github.com/antrea-io/nephe/pull/190), [@reachjainrahul])
- Update `Vpc` and `VirtualMachine` aggregated API server REST handler to include more filters like CloudId, CloudVpcId, etc. ([#225](https://github.com/antrea-io/nephe/pull/225), [@Anandkumar26])
- Sort responses from aggregated API server in alphabetical order. ([#211](https://github.com/antrea-io/nephe/pull/211), [@reachjainrahul])
- Remove the AppliedToGroup field from the rule description in the cloud due to Azure's character limit on the description field. ([#230](https://github.com/antrea-io/nephe/pull/230), [@shenmo3])
- Allow user-defined rules on Nephe-managed cloud network security groups. ([#207](https://github.com/antrea-io/nephe/pull/207), [@shenmo3])
- Modify the `CloudProviderAccount` CRD to take regions as an array and the `CloudEntitySelector` CRD to reference `CloudProviderAccount`. ([#208](https://github.com/antrea-io/nephe/pull/208), [@archanapholla])
- Reorganize and refactor the code for better maintainability and readability. ([#202](https://github.com/antrea-io/nephe/pull/202), [@reachjainrahul] [@Anandkumar26])
- Remove unnecessary state of appliedTo group in network policy controller. ([#193](https://github.com/antrea-io/nephe/pull/193), [@reachjainrahul])
- Upgrade ginkgo to version v2.9.5. ([#205](https://github.com/antrea-io/nephe/pull/205), [@Nithish555])

### Fixed

- Skip handling of IPv6 addresses in Antrea `NetworkPolicy` as it's currently not supported. ([#222](https://github.com/antrea-io/nephe/pull/222), [@reachjainrahul])
- Fix an issue with Azure rule priorities that were being updated with each Antrea `NetworkPolicy` update. ([#220](https://github.com/antrea-io/nephe/pull/220), [@shenmo3])
- Fix a race condition in Antrea `NetworkPolicy` handling where re-adding the same policy while a previous deletion was in progress. ([#189](https://github.com/antrea-io/nephe/pull/189), [@reachjainrahul])
- Fix a bug that caused address groups to remain stuck in a pending delete state indefinitely. ([#194](https://github.com/antrea-io/nephe/pull/194), [@reachjainrahul])
- Capture errors while processing `CloudProviderAccount` CR in the status field. ([#186](https://github.com/antrea-io/nephe/pull/186), [@Anandkumar26])
- Fix a bug where updating region in `CloudProviderAccount` CR was not getting reflected in Azure. ([#149](https://github.com/antrea-io/nephe/pull/149), [@archanapholla])

[@Anandkumar26]: https://github.com/Anandkumar26
[@archanapholla]: https://github.com/archanapholla
[@Nithish555]: https://github.com/Nithish555
[@reachjainrahul]: https://github.com/reachjainrahul
[@shenmo3]: https://github.com/shenmo3

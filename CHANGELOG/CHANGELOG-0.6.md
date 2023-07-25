# Changelog 0.6

## 0.6.0 - 2023-07-25

### Added

- Add support to configure multiple `CloudEntitySelector` CRs for a single `CloudProviderAccount` CR. Also, these CRs can now be configured in different namespaces. ([#216](https://github.com/antrea-io/nephe/pull/216) [#269](https://github.com/antrea-io/nephe/pull/269), [@archanapholla])
- Add support to allow user tags on `VirtualMachine` objects. ([#272](https://github.com/antrea-io/nephe/pull/272), [@reachjainrahul])
- Add enforcement of Antrea `NetworkPolicy` containing IPV6 addresses. ([#246](https://github.com/antrea-io/nephe/pull/246), [@reachjainrahul])
- Add integration tests in Jenkins for upgrade workflow. ([#245](https://github.com/antrea-io/nephe/pull/245) [#168](https://github.com/antrea-io/nephe/pull/168), [@Anandkumar26], [@reachjainrahul])
- Add support for cloud credentials to be configured in any namespace for a `CloudProviderAccount` CR. ([#281](https://github.com/antrea-io/nephe/pull/281), [@reachjainrahul])
- Add support for filtering on label and field selectors of `VirtualMachine` and `Vpc` objects in the watch handler. ([#250](https://github.com/antrea-io/nephe/pull/250), [@reachjainrahul])
- Add pagination support for azure resource graph queries. ([#277](https://github.com/antrea-io/nephe/pull/277), [@archanapholla])

### Changed

- Compute cloud rule delta for Antrea `NetworkPolicy` in the plugin before realization. ([#276](https://github.com/antrea-io/nephe/pull/276), [@shenmo3])
- For Azure cloud, remove user-created security rules that fall within the Nephe priority range. ([#256](https://github.com/antrea-io/nephe/pull/256), [@shenmo3])
- Update retry workflow for failed Antrea `NetworkPolicy` in network policy controller. ([#275](https://github.com/antrea-io/nephe/pull/275) [#274](https://github.com/antrea-io/nephe/pull/274), [@reachjainrahul])
- Allow Nephe controller to directly connect to Antrea controller instead of relaying connection via K8s API server. ([#280](https://github.com/antrea-io/nephe/pull/280), [@reachjainrahul])
- Cleanup tracker code by unifying virtual machine policy and network policy tracker in one. ([#219](https://github.com/antrea-io/nephe/pull/219), [@Anandkumar26])
- Move cloud plugin level lock to an account level lock. ([#260](https://github.com/antrea-io/nephe/pull/260), [@reachjainrahul])
- Reorganize and refactor cloud plugins. ([#253](https://github.com/antrea-io/nephe/pull/253) [#258](https://github.com/antrea-io/nephe/pull/258),[@reachjainrahul])

### Fixed

- Fix network policy controller state when a `CloudProviderAccount` CR is deleted. ([#262](https://github.com/antrea-io/nephe/pull/262), [@Anandkumar26])
- Fix an issue to handle `AddressGroup` modification during restart. ([#247](https://github.com/antrea-io/nephe/pull/247), [@shenmo3])
- Fix an issue to handle `AppliedToGroup` delete and re-add. ([#259](https://github.com/antrea-io/nephe/pull/259), [@Anandkumar26])

[@Anandkumar26]: https://github.com/Anandkumar26
[@archanapholla]: https://github.com/archanapholla
[@reachjainrahul]: https://github.com/reachjainrahul
[@shenmo3]: https://github.com/shenmo3

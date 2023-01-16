# Changelog 0.3

## 0.3.0 - 2023-01-13

### Added

- Add Antrea NetworkPolicy rollback in case of any realization failure. ([#85](https://github.com/antrea-io/nephe/pull/85), [@shenmo3])
- Add VPC Poller feature to import cloud VPCs:
  * Automatically build VPC inventory whenever a `CloudProviderAccount` CR is added. ([#78](https://github.com/antrea-io/nephe/pull/78), [@archanapholla])
  * Expose VPC inventory via aggregated API server. ([#80](https://github.com/antrea-io/nephe/pull/80), [@bangqipropel])
  * Add integration test coverage. ([#86](https://github.com/antrea-io/nephe/pull/86), [@Anandkumar26] [@archanapholla])
- Add Antrea VM agent CI coverage for Windows platform. ([#72](https://github.com/antrea-io/nephe/pull/72), [@Anandkumar26])
- Add unit test coverage for `CloudProviderAccount` and `CloudEntitySelector` CR webhooks. ([#73](https://github.com/antrea-io/nephe/pull/73), [@archanapholla])

### Changed

- Support Antrea v1.10.0 release. ([#84](https://github.com/antrea-io/nephe/pull/84), [@reachjainrahul])
- Process each Antrea NetworkPolicy independently rather than processing all Antrea NetworkPolicies mapping to a cloud security group together. ([#85](https://github.com/antrea-io/nephe/pull/85), [@shenmo3])
- Update integration test to run Antrea VM agent CI in a container for Ubuntu and RHEL platforms. ([#70](https://github.com/antrea-io/nephe/pull/70), [@Anandkumar26])

### Fixed

- Fix Antrea NetworkPolicy realization status incase a Antrea NetworkPolicy is modified. ([#88](https://github.com/antrea-io/nephe/pull/88), [@reachjainrahul])

[@Anandkumar26]: https://github.com/Anandkumar26
[@archanapholla]: https://github.com/archanapholla
[@bangqipropel]: https://github.com/bangqipropel
[@reachjainrahul]: https://github.com/reachjainrahul
[@shenmo3]: https://github.com/shenmo3

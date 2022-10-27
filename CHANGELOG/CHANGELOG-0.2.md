# Changelog 0.2

## 0.2.0 - 2022-10-28

### Added

- Add Antrea VM agent support for Public Cloud VMs:
  * Add support to import agented VMs using External Nodes. ([#23](https://github.com/antrea-io/nephe/pull/23), [@Anandkumar26])
  * Add CI tests for agented Ubuntu 20.04 VMs. ([#42](https://github.com/antrea-io/nephe/pull/42), [@shenmo3])
  * Add integration tests for External Node. ([#37](https://github.com/antrea-io/nephe/pull/37), [@archanapholla])
- Add integration test for Antrea groups. ([#40](https://github.com/antrea-io/nephe/pull/40), [@reachjainrahul])
- Add Antrea NetworkPolicy rule realization status reporting. ([#51](https://github.com/antrea-io/nephe/pull/51) [#29](https://github.com/antrea-io/nephe/pull/29), [@reachjainrahul])
- Enable CodeCov reporting. ([#43](https://github.com/antrea-io/nephe/pull/43), [@reachjainrahul])

### Changed

- Update Go to v1.19. ([#16](https://github.com/antrea-io/nephe/pull/16), [@reachjainrahul])
- Support Antrea 1.9 release. ([#49](https://github.com/antrea-io/nephe/pull/49), [@reachjainrahul])
- Perform delta Antrea NetworkPolicy update on AWS security rule. ([#34](https://github.com/antrea-io/nephe/pull/34), [@shenmo3])
- Optimization to cloud security group update operations. ([#35](https://github.com/antrea-io/nephe/pull/35), [@archanapholla])
- Improve unit-test coverage. ([#36](https://github.com/antrea-io/nephe/pull/36) [#19](https://github.com/antrea-io/nephe/pull/19) [@bangqipropel])
- Update webhook validations. ([#44](https://github.com/antrea-io/nephe/pull/44), [@archanapholla]), ([#18](https://github.com/antrea-io/nephe/pull/18), [@Anandkumar26])

### Fixed

- Fix AWS dependency violation error. ([#26](https://github.com/antrea-io/nephe/pull/26), [@shenmo3])

[@reachjainrahul]: https://github.com/reachjainrahul
[@Anandkumar26]: https://github.com/Anandkumar26
[@shenmo3]: https://github.com/shenmo3
[@archanapholla]: https://github.com/archanapholla
[@bangqipropel]: https://github.com/bangqipropel

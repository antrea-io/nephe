# Changelog 0.2

## 0.2.0 - 2022-10-28

### Added

- Add support to import public cloud VMs which run Antrea on non-Kubernetes Nodes (ExternalNode).([#23](https://github.com/antrea-io/nephe/pull/23), [@Anandkumar26])
- Add install wrapper scripts for running Antrea on public cloud VMs in release assets.([#54](https://github.com/antrea-io/nephe/pull/54), [@reachjainrahul])
- Add integration tests for ExternalNode. ([#37](https://github.com/antrea-io/nephe/pull/37), [@archanapholla])
- Add CI tests for ExternalNode. ([#42](https://github.com/antrea-io/nephe/pull/42), [@shenmo3])
- Add support to create ANP using antrea group. ([#46](https://github.com/antrea-io/nephe/pull/46), [@reachjainrahul])
- Add integration test for antrea group. ([#40](https://github.com/antrea-io/nephe/pull/40), [@reachjainrahul])
- Add ANP rule realization status. ([#51](https://github.com/antrea-io/nephe/pull/51) [#29](https://github.com/antrea-io/nephe/pull/29), [@reachjainrahul])
- Add the following capabilities as part of ANP optimization:
  * Perform delta update on AWS security rule ([#34](https://github.com/antrea-io/nephe/pull/34), [@shenmo3])
  * Fix aws dependency violation error ([#26](https://github.com/antrea-io/nephe/pull/26), [@shenmo3])
  * Optimization changes for security group update operation. ([#35](https://github.com/antrea-io/nephe/pull/35), [@archanapholla])
- Reduce terraform dependencies for easier deployment. ([#10](https://github.com/antrea-io/nephe/pull/10), [@shenmo3])
- Added webhook validation for CloudEntitySelector. ([#44](https://github.com/antrea-io/nephe/pull/44), [@archanapholla])
- Added webhook validation for Secret. ([#18](https://github.com/antrea-io/nephe/pull/18), [@Anandkumar26])
- Improve unit-test coverage. ([#36](https://github.com/antrea-io/nephe/pull/36) [#19](https://github.com/antrea-io/nephe/pull/19) [@bangqipropel])
- Support antrea 1.9 release. ([#49](https://github.com/antrea-io/nephe/pull/49), [@reachjainrahul])
- Update Go to v1.19. ([#16](https://github.com/antrea-io/nephe/pull/16), [@reachjainrahul])
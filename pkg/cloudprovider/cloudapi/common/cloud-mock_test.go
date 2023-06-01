// // Copyright 2022 Antrea Authors.
// //
// // Licensed under the Apache License, Version 2.0 (the "License");
// // you may not use this file except in compliance with the License.
// // You may obtain a copy of the License at
// //
// //      http://www.apache.org/licenses/LICENSE-2.0
// //
// // Unless required by applicable law or agreed to in writing, software
// // distributed under the License is distributed on an "AS IS" BASIS,
// // WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// // See the License for the specific language governing permissions and
// // limitations under the License.
//

// Code generated by MockGen. DO NOT EDIT.
// Source: pkg/cloudprovider/cloudapi/common/cloud.go

// Package common is a generated GoMock package.
package common

import (
	reflect "reflect"

	v1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	v1alpha10 "antrea.io/nephe/apis/runtime/v1alpha1"
	securitygroup "antrea.io/nephe/pkg/cloudprovider/securitygroup"
	gomock "github.com/golang/mock/gomock"
	types "k8s.io/apimachinery/pkg/types"
	client "sigs.k8s.io/controller-runtime/pkg/client"
)

// MockCloudInterface is a mock of CloudInterface interface.
type MockCloudInterface struct {
	ctrl     *gomock.Controller
	recorder *MockCloudInterfaceMockRecorder
}

// MockCloudInterfaceMockRecorder is the mock recorder for MockCloudInterface.
type MockCloudInterfaceMockRecorder struct {
	mock *MockCloudInterface
}

// NewMockCloudInterface creates a new mock instance.
func NewMockCloudInterface(ctrl *gomock.Controller) *MockCloudInterface {
	mock := &MockCloudInterface{ctrl: ctrl}
	mock.recorder = &MockCloudInterfaceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockCloudInterface) EXPECT() *MockCloudInterfaceMockRecorder {
	return m.recorder
}

// AddAccountResourceSelector mocks base method.
func (m *MockCloudInterface) AddAccountResourceSelector(accNamespacedName *types.NamespacedName, selector *v1alpha1.CloudEntitySelector) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AddAccountResourceSelector", accNamespacedName, selector)
	ret0, _ := ret[0].(error)
	return ret0
}

// AddAccountResourceSelector indicates an expected call of AddAccountResourceSelector.
func (mr *MockCloudInterfaceMockRecorder) AddAccountResourceSelector(accNamespacedName, selector interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddAccountResourceSelector", reflect.TypeOf((*MockCloudInterface)(nil).AddAccountResourceSelector), accNamespacedName, selector)
}

// AddProviderAccount mocks base method.
func (m *MockCloudInterface) AddProviderAccount(client client.Client, account *v1alpha1.CloudProviderAccount) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AddProviderAccount", client, account)
	ret0, _ := ret[0].(error)
	return ret0
}

// AddProviderAccount indicates an expected call of AddProviderAccount.
func (mr *MockCloudInterfaceMockRecorder) AddProviderAccount(client, account interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddProviderAccount", reflect.TypeOf((*MockCloudInterface)(nil).AddProviderAccount), client, account)
}

// CreateSecurityGroup mocks base method.
func (m *MockCloudInterface) CreateSecurityGroup(securityGroupIdentifier *securitygroup.CloudResource, membershipOnly bool) (*string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CreateSecurityGroup", securityGroupIdentifier, membershipOnly)
	ret0, _ := ret[0].(*string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// CreateSecurityGroup indicates an expected call of CreateSecurityGroup.
func (mr *MockCloudInterfaceMockRecorder) CreateSecurityGroup(securityGroupIdentifier, membershipOnly interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CreateSecurityGroup", reflect.TypeOf((*MockCloudInterface)(nil).CreateSecurityGroup), securityGroupIdentifier, membershipOnly)
}

// DeleteSecurityGroup mocks base method.
func (m *MockCloudInterface) DeleteSecurityGroup(securityGroupIdentifier *securitygroup.CloudResource, membershipOnly bool) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DeleteSecurityGroup", securityGroupIdentifier, membershipOnly)
	ret0, _ := ret[0].(error)
	return ret0
}

// DeleteSecurityGroup indicates an expected call of DeleteSecurityGroup.
func (mr *MockCloudInterfaceMockRecorder) DeleteSecurityGroup(securityGroupIdentifier, membershipOnly interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DeleteSecurityGroup", reflect.TypeOf((*MockCloudInterface)(nil).DeleteSecurityGroup), securityGroupIdentifier, membershipOnly)
}

// DoInventoryPoll mocks base method.
func (m *MockCloudInterface) DoInventoryPoll(accountNamespacedName *types.NamespacedName) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DoInventoryPoll", accountNamespacedName)
	ret0, _ := ret[0].(error)
	return ret0
}

// DoInventoryPoll indicates an expected call of DoInventoryPoll.
func (mr *MockCloudInterfaceMockRecorder) DoInventoryPoll(accountNamespacedName interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DoInventoryPoll", reflect.TypeOf((*MockCloudInterface)(nil).DoInventoryPoll), accountNamespacedName)
}

// GetAccountStatus mocks base method.
func (m *MockCloudInterface) GetAccountStatus(accNamespacedName *types.NamespacedName) (*v1alpha1.CloudProviderAccountStatus, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetAccountStatus", accNamespacedName)
	ret0, _ := ret[0].(*v1alpha1.CloudProviderAccountStatus)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetAccountStatus indicates an expected call of GetAccountStatus.
func (mr *MockCloudInterfaceMockRecorder) GetAccountStatus(accNamespacedName interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetAccountStatus", reflect.TypeOf((*MockCloudInterface)(nil).GetAccountStatus), accNamespacedName)
}

// GetEnforcedSecurity mocks base method.
func (m *MockCloudInterface) GetEnforcedSecurity() []securitygroup.SynchronizationContent {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetEnforcedSecurity")
	ret0, _ := ret[0].([]securitygroup.SynchronizationContent)
	return ret0
}

// GetEnforcedSecurity indicates an expected call of GetEnforcedSecurity.
func (mr *MockCloudInterfaceMockRecorder) GetEnforcedSecurity() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetEnforcedSecurity", reflect.TypeOf((*MockCloudInterface)(nil).GetEnforcedSecurity))
}

// GetSecurityGroups mocks base method.
func (m *MockCloudInterface) GetSecurityGroups(namespacedName *types.NamespacedName) (map[string]*v1alpha10.SecurityGroup, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetSecurityGroups", namespacedName)
	ret0, _ := ret[0].(map[string]*v1alpha10.SecurityGroup)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetSecurityGroups indicates an expected call of GetSecurityGroups.
func (mr *MockCloudInterfaceMockRecorder) GetSecurityGroups(namespacedName interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetSecurityGroups", reflect.TypeOf((*MockCloudInterface)(nil).GetSecurityGroups), namespacedName)
}

// GetVpcInventory mocks base method.
func (m *MockCloudInterface) GetVpcInventory(accountNamespacedName *types.NamespacedName) (map[string]*v1alpha10.Vpc, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetVpcInventory", accountNamespacedName)
	ret0, _ := ret[0].(map[string]*v1alpha10.Vpc)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetVpcInventory indicates an expected call of GetVpcInventory.
func (mr *MockCloudInterfaceMockRecorder) GetVpcInventory(accountNamespacedName interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetVpcInventory", reflect.TypeOf((*MockCloudInterface)(nil).GetVpcInventory), accountNamespacedName)
}

// InstancesGivenProviderAccount mocks base method.
func (m *MockCloudInterface) InstancesGivenProviderAccount(namespacedName *types.NamespacedName) (map[string]*v1alpha10.VirtualMachine, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "InstancesGivenProviderAccount", namespacedName)
	ret0, _ := ret[0].(map[string]*v1alpha10.VirtualMachine)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// InstancesGivenProviderAccount indicates an expected call of InstancesGivenProviderAccount.
func (mr *MockCloudInterfaceMockRecorder) InstancesGivenProviderAccount(namespacedName interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "InstancesGivenProviderAccount", reflect.TypeOf((*MockCloudInterface)(nil).InstancesGivenProviderAccount), namespacedName)
}

// ProviderType mocks base method.
func (m *MockCloudInterface) ProviderType() ProviderType {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ProviderType")
	ret0, _ := ret[0].(ProviderType)
	return ret0
}

// ProviderType indicates an expected call of ProviderType.
func (mr *MockCloudInterfaceMockRecorder) ProviderType() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ProviderType", reflect.TypeOf((*MockCloudInterface)(nil).ProviderType))
}

// RemoveAccountResourcesSelector mocks base method.
func (m *MockCloudInterface) RemoveAccountResourcesSelector(accNamespacedName, selectorNamespacedName *types.NamespacedName) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "RemoveAccountResourcesSelector", accNamespacedName, selectorNamespacedName)
}

// RemoveAccountResourcesSelector indicates an expected call of RemoveAccountResourcesSelector.
func (mr *MockCloudInterfaceMockRecorder) RemoveAccountResourcesSelector(accNamespacedName, selectorNamespacedName interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RemoveAccountResourcesSelector", reflect.TypeOf((*MockCloudInterface)(nil).RemoveAccountResourcesSelector), accNamespacedName, selectorNamespacedName)
}

// RemoveProviderAccount mocks base method.
func (m *MockCloudInterface) RemoveProviderAccount(namespacedName *types.NamespacedName) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "RemoveProviderAccount", namespacedName)
}

// RemoveProviderAccount indicates an expected call of RemoveProviderAccount.
func (mr *MockCloudInterfaceMockRecorder) RemoveProviderAccount(namespacedName interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RemoveProviderAccount", reflect.TypeOf((*MockCloudInterface)(nil).RemoveProviderAccount), namespacedName)
}

// ResetInventoryCache mocks base method.
func (m *MockCloudInterface) ResetInventoryCache(accountNamespacedName *types.NamespacedName) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ResetInventoryCache", accountNamespacedName)
	ret0, _ := ret[0].(error)
	return ret0
}

// ResetInventoryCache indicates an expected call of ResetInventoryCache.
func (mr *MockCloudInterfaceMockRecorder) ResetInventoryCache(accountNamespacedName interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ResetInventoryCache", reflect.TypeOf((*MockCloudInterface)(nil).ResetInventoryCache), accountNamespacedName)
}

// UpdateSecurityGroupMembers mocks base method.
func (m *MockCloudInterface) UpdateSecurityGroupMembers(securityGroupIdentifier *securitygroup.CloudResource, computeResourceIdentifier []*securitygroup.CloudResource, membershipOnly bool) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "UpdateSecurityGroupMembers", securityGroupIdentifier, computeResourceIdentifier, membershipOnly)
	ret0, _ := ret[0].(error)
	return ret0
}

// UpdateSecurityGroupMembers indicates an expected call of UpdateSecurityGroupMembers.
func (mr *MockCloudInterfaceMockRecorder) UpdateSecurityGroupMembers(securityGroupIdentifier, computeResourceIdentifier, membershipOnly interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "UpdateSecurityGroupMembers", reflect.TypeOf((*MockCloudInterface)(nil).UpdateSecurityGroupMembers), securityGroupIdentifier, computeResourceIdentifier, membershipOnly)
}

// UpdateSecurityGroupRules mocks base method.
func (m *MockCloudInterface) UpdateSecurityGroupRules(appliedToGroupIdentifier *securitygroup.CloudResource, addRules, rmRules []*securitygroup.CloudRule) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "UpdateSecurityGroupRules", appliedToGroupIdentifier, addRules, rmRules)
	ret0, _ := ret[0].(error)
	return ret0
}

// UpdateSecurityGroupRules indicates an expected call of UpdateSecurityGroupRules.
func (mr *MockCloudInterfaceMockRecorder) UpdateSecurityGroupRules(appliedToGroupIdentifier, addRules, rmRules interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "UpdateSecurityGroupRules", reflect.TypeOf((*MockCloudInterface)(nil).UpdateSecurityGroupRules), appliedToGroupIdentifier, addRules, rmRules)
}

// MockAccountMgmtInterface is a mock of AccountMgmtInterface interface.
type MockAccountMgmtInterface struct {
	ctrl     *gomock.Controller
	recorder *MockAccountMgmtInterfaceMockRecorder
}

// MockAccountMgmtInterfaceMockRecorder is the mock recorder for MockAccountMgmtInterface.
type MockAccountMgmtInterfaceMockRecorder struct {
	mock *MockAccountMgmtInterface
}

// NewMockAccountMgmtInterface creates a new mock instance.
func NewMockAccountMgmtInterface(ctrl *gomock.Controller) *MockAccountMgmtInterface {
	mock := &MockAccountMgmtInterface{ctrl: ctrl}
	mock.recorder = &MockAccountMgmtInterfaceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockAccountMgmtInterface) EXPECT() *MockAccountMgmtInterfaceMockRecorder {
	return m.recorder
}

// AddAccountResourceSelector mocks base method.
func (m *MockAccountMgmtInterface) AddAccountResourceSelector(accNamespacedName *types.NamespacedName, selector *v1alpha1.CloudEntitySelector) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AddAccountResourceSelector", accNamespacedName, selector)
	ret0, _ := ret[0].(error)
	return ret0
}

// AddAccountResourceSelector indicates an expected call of AddAccountResourceSelector.
func (mr *MockAccountMgmtInterfaceMockRecorder) AddAccountResourceSelector(accNamespacedName, selector interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddAccountResourceSelector", reflect.TypeOf((*MockAccountMgmtInterface)(nil).AddAccountResourceSelector), accNamespacedName, selector)
}

// AddProviderAccount mocks base method.
func (m *MockAccountMgmtInterface) AddProviderAccount(client client.Client, account *v1alpha1.CloudProviderAccount) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AddProviderAccount", client, account)
	ret0, _ := ret[0].(error)
	return ret0
}

// AddProviderAccount indicates an expected call of AddProviderAccount.
func (mr *MockAccountMgmtInterfaceMockRecorder) AddProviderAccount(client, account interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddProviderAccount", reflect.TypeOf((*MockAccountMgmtInterface)(nil).AddProviderAccount), client, account)
}

// DoInventoryPoll mocks base method.
func (m *MockAccountMgmtInterface) DoInventoryPoll(accountNamespacedName *types.NamespacedName) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DoInventoryPoll", accountNamespacedName)
	ret0, _ := ret[0].(error)
	return ret0
}

// DoInventoryPoll indicates an expected call of DoInventoryPoll.
func (mr *MockAccountMgmtInterfaceMockRecorder) DoInventoryPoll(accountNamespacedName interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DoInventoryPoll", reflect.TypeOf((*MockAccountMgmtInterface)(nil).DoInventoryPoll), accountNamespacedName)
}

// GetAccountStatus mocks base method.
func (m *MockAccountMgmtInterface) GetAccountStatus(accNamespacedName *types.NamespacedName) (*v1alpha1.CloudProviderAccountStatus, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetAccountStatus", accNamespacedName)
	ret0, _ := ret[0].(*v1alpha1.CloudProviderAccountStatus)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetAccountStatus indicates an expected call of GetAccountStatus.
func (mr *MockAccountMgmtInterfaceMockRecorder) GetAccountStatus(accNamespacedName interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetAccountStatus", reflect.TypeOf((*MockAccountMgmtInterface)(nil).GetAccountStatus), accNamespacedName)
}

// GetVpcInventory mocks base method.
func (m *MockAccountMgmtInterface) GetVpcInventory(accountNamespacedName *types.NamespacedName) (map[string]*v1alpha10.Vpc, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetVpcInventory", accountNamespacedName)
	ret0, _ := ret[0].(map[string]*v1alpha10.Vpc)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetVpcInventory indicates an expected call of GetVpcInventory.
func (mr *MockAccountMgmtInterfaceMockRecorder) GetVpcInventory(accountNamespacedName interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetVpcInventory", reflect.TypeOf((*MockAccountMgmtInterface)(nil).GetVpcInventory), accountNamespacedName)
}

// RemoveAccountResourcesSelector mocks base method.
func (m *MockAccountMgmtInterface) RemoveAccountResourcesSelector(accNamespacedName, selectorNamespacedName *types.NamespacedName) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "RemoveAccountResourcesSelector", accNamespacedName, selectorNamespacedName)
}

// RemoveAccountResourcesSelector indicates an expected call of RemoveAccountResourcesSelector.
func (mr *MockAccountMgmtInterfaceMockRecorder) RemoveAccountResourcesSelector(accNamespacedName, selectorNamespacedName interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RemoveAccountResourcesSelector", reflect.TypeOf((*MockAccountMgmtInterface)(nil).RemoveAccountResourcesSelector), accNamespacedName, selectorNamespacedName)
}

// RemoveProviderAccount mocks base method.
func (m *MockAccountMgmtInterface) RemoveProviderAccount(namespacedName *types.NamespacedName) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "RemoveProviderAccount", namespacedName)
}

// RemoveProviderAccount indicates an expected call of RemoveProviderAccount.
func (mr *MockAccountMgmtInterfaceMockRecorder) RemoveProviderAccount(namespacedName interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RemoveProviderAccount", reflect.TypeOf((*MockAccountMgmtInterface)(nil).RemoveProviderAccount), namespacedName)
}

// ResetInventoryCache mocks base method.
func (m *MockAccountMgmtInterface) ResetInventoryCache(accountNamespacedName *types.NamespacedName) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ResetInventoryCache", accountNamespacedName)
	ret0, _ := ret[0].(error)
	return ret0
}

// ResetInventoryCache indicates an expected call of ResetInventoryCache.
func (mr *MockAccountMgmtInterfaceMockRecorder) ResetInventoryCache(accountNamespacedName interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ResetInventoryCache", reflect.TypeOf((*MockAccountMgmtInterface)(nil).ResetInventoryCache), accountNamespacedName)
}

// MockComputeInterface is a mock of ComputeInterface interface.
type MockComputeInterface struct {
	ctrl     *gomock.Controller
	recorder *MockComputeInterfaceMockRecorder
}

// MockComputeInterfaceMockRecorder is the mock recorder for MockComputeInterface.
type MockComputeInterfaceMockRecorder struct {
	mock *MockComputeInterface
}

// NewMockComputeInterface creates a new mock instance.
func NewMockComputeInterface(ctrl *gomock.Controller) *MockComputeInterface {
	mock := &MockComputeInterface{ctrl: ctrl}
	mock.recorder = &MockComputeInterfaceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockComputeInterface) EXPECT() *MockComputeInterfaceMockRecorder {
	return m.recorder
}

// GetSecurityGroups mocks base method.
func (m *MockComputeInterface) GetSecurityGroups(namespacedName *types.NamespacedName) (map[string]*v1alpha10.SecurityGroup, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetSecurityGroups", namespacedName)
	ret0, _ := ret[0].(map[string]*v1alpha10.SecurityGroup)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetSecurityGroups indicates an expected call of GetSecurityGroups.
func (mr *MockComputeInterfaceMockRecorder) GetSecurityGroups(namespacedName interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetSecurityGroups", reflect.TypeOf((*MockComputeInterface)(nil).GetSecurityGroups), namespacedName)
}

// InstancesGivenProviderAccount mocks base method.
func (m *MockComputeInterface) InstancesGivenProviderAccount(namespacedName *types.NamespacedName) (map[string]*v1alpha10.VirtualMachine, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "InstancesGivenProviderAccount", namespacedName)
	ret0, _ := ret[0].(map[string]*v1alpha10.VirtualMachine)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// InstancesGivenProviderAccount indicates an expected call of InstancesGivenProviderAccount.
func (mr *MockComputeInterfaceMockRecorder) InstancesGivenProviderAccount(namespacedName interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "InstancesGivenProviderAccount", reflect.TypeOf((*MockComputeInterface)(nil).InstancesGivenProviderAccount), namespacedName)
}

// MockSecurityInterface is a mock of SecurityInterface interface.
type MockSecurityInterface struct {
	ctrl     *gomock.Controller
	recorder *MockSecurityInterfaceMockRecorder
}

// MockSecurityInterfaceMockRecorder is the mock recorder for MockSecurityInterface.
type MockSecurityInterfaceMockRecorder struct {
	mock *MockSecurityInterface
}

// NewMockSecurityInterface creates a new mock instance.
func NewMockSecurityInterface(ctrl *gomock.Controller) *MockSecurityInterface {
	mock := &MockSecurityInterface{ctrl: ctrl}
	mock.recorder = &MockSecurityInterfaceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockSecurityInterface) EXPECT() *MockSecurityInterfaceMockRecorder {
	return m.recorder
}

// CreateSecurityGroup mocks base method.
func (m *MockSecurityInterface) CreateSecurityGroup(securityGroupIdentifier *securitygroup.CloudResource, membershipOnly bool) (*string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CreateSecurityGroup", securityGroupIdentifier, membershipOnly)
	ret0, _ := ret[0].(*string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// CreateSecurityGroup indicates an expected call of CreateSecurityGroup.
func (mr *MockSecurityInterfaceMockRecorder) CreateSecurityGroup(securityGroupIdentifier, membershipOnly interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CreateSecurityGroup", reflect.TypeOf((*MockSecurityInterface)(nil).CreateSecurityGroup), securityGroupIdentifier, membershipOnly)
}

// DeleteSecurityGroup mocks base method.
func (m *MockSecurityInterface) DeleteSecurityGroup(securityGroupIdentifier *securitygroup.CloudResource, membershipOnly bool) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DeleteSecurityGroup", securityGroupIdentifier, membershipOnly)
	ret0, _ := ret[0].(error)
	return ret0
}

// DeleteSecurityGroup indicates an expected call of DeleteSecurityGroup.
func (mr *MockSecurityInterfaceMockRecorder) DeleteSecurityGroup(securityGroupIdentifier, membershipOnly interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DeleteSecurityGroup", reflect.TypeOf((*MockSecurityInterface)(nil).DeleteSecurityGroup), securityGroupIdentifier, membershipOnly)
}

// GetEnforcedSecurity mocks base method.
func (m *MockSecurityInterface) GetEnforcedSecurity() []securitygroup.SynchronizationContent {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetEnforcedSecurity")
	ret0, _ := ret[0].([]securitygroup.SynchronizationContent)
	return ret0
}

// GetEnforcedSecurity indicates an expected call of GetEnforcedSecurity.
func (mr *MockSecurityInterfaceMockRecorder) GetEnforcedSecurity() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetEnforcedSecurity", reflect.TypeOf((*MockSecurityInterface)(nil).GetEnforcedSecurity))
}

// UpdateSecurityGroupMembers mocks base method.
func (m *MockSecurityInterface) UpdateSecurityGroupMembers(securityGroupIdentifier *securitygroup.CloudResource, computeResourceIdentifier []*securitygroup.CloudResource, membershipOnly bool) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "UpdateSecurityGroupMembers", securityGroupIdentifier, computeResourceIdentifier, membershipOnly)
	ret0, _ := ret[0].(error)
	return ret0
}

// UpdateSecurityGroupMembers indicates an expected call of UpdateSecurityGroupMembers.
func (mr *MockSecurityInterfaceMockRecorder) UpdateSecurityGroupMembers(securityGroupIdentifier, computeResourceIdentifier, membershipOnly interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "UpdateSecurityGroupMembers", reflect.TypeOf((*MockSecurityInterface)(nil).UpdateSecurityGroupMembers), securityGroupIdentifier, computeResourceIdentifier, membershipOnly)
}

// UpdateSecurityGroupRules mocks base method.
func (m *MockSecurityInterface) UpdateSecurityGroupRules(appliedToGroupIdentifier *securitygroup.CloudResource, addRules, rmRules []*securitygroup.CloudRule) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "UpdateSecurityGroupRules", appliedToGroupIdentifier, addRules, rmRules)
	ret0, _ := ret[0].(error)
	return ret0
}

// UpdateSecurityGroupRules indicates an expected call of UpdateSecurityGroupRules.
func (mr *MockSecurityInterfaceMockRecorder) UpdateSecurityGroupRules(appliedToGroupIdentifier, addRules, rmRules interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "UpdateSecurityGroupRules", reflect.TypeOf((*MockSecurityInterface)(nil).UpdateSecurityGroupRules), appliedToGroupIdentifier, addRules, rmRules)
}

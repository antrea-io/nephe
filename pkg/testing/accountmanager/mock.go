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
// Source: antrea.io/nephe/pkg/accountmanager (interfaces: Interface)

// Package accountmanager is a generated GoMock package.
package accountmanager

import (
	reflect "reflect"

	v1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	v1alpha10 "antrea.io/nephe/apis/runtime/v1alpha1"
	gomock "github.com/golang/mock/gomock"
	types "k8s.io/apimachinery/pkg/types"
)

// MockInterface is a mock of Interface interface.
type MockInterface struct {
	ctrl     *gomock.Controller
	recorder *MockInterfaceMockRecorder
}

// MockInterfaceMockRecorder is the mock recorder for MockInterface.
type MockInterfaceMockRecorder struct {
	mock *MockInterface
}

// NewMockInterface creates a new mock instance.
func NewMockInterface(ctrl *gomock.Controller) *MockInterface {
	mock := &MockInterface{ctrl: ctrl}
	mock.recorder = &MockInterfaceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockInterface) EXPECT() *MockInterfaceMockRecorder {
	return m.recorder
}

// AddAccount mocks base method.
func (m *MockInterface) AddAccount(arg0 *types.NamespacedName, arg1 v1alpha10.CloudProvider, arg2 *v1alpha1.CloudProviderAccount) (bool, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AddAccount", arg0, arg1, arg2)
	ret0, _ := ret[0].(bool)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// AddAccount indicates an expected call of AddAccount.
func (mr *MockInterfaceMockRecorder) AddAccount(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddAccount", reflect.TypeOf((*MockInterface)(nil).AddAccount), arg0, arg1, arg2)
}

// AddResourceFiltersToAccount mocks base method.
func (m *MockInterface) AddResourceFiltersToAccount(arg0, arg1 *types.NamespacedName, arg2 *v1alpha1.CloudEntitySelector) (bool, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AddResourceFiltersToAccount", arg0, arg1, arg2)
	ret0, _ := ret[0].(bool)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// AddResourceFiltersToAccount indicates an expected call of AddResourceFiltersToAccount.
func (mr *MockInterfaceMockRecorder) AddResourceFiltersToAccount(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddResourceFiltersToAccount", reflect.TypeOf((*MockInterface)(nil).AddResourceFiltersToAccount), arg0, arg1, arg2)
}

// RemoveAccount mocks base method.
func (m *MockInterface) RemoveAccount(arg0 *types.NamespacedName) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RemoveAccount", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// RemoveAccount indicates an expected call of RemoveAccount.
func (mr *MockInterfaceMockRecorder) RemoveAccount(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RemoveAccount", reflect.TypeOf((*MockInterface)(nil).RemoveAccount), arg0)
}

// RemoveResourceFiltersFromAccount mocks base method.
func (m *MockInterface) RemoveResourceFiltersFromAccount(arg0, arg1 *types.NamespacedName) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RemoveResourceFiltersFromAccount", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// RemoveResourceFiltersFromAccount indicates an expected call of RemoveResourceFiltersFromAccount.
func (mr *MockInterfaceMockRecorder) RemoveResourceFiltersFromAccount(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RemoveResourceFiltersFromAccount", reflect.TypeOf((*MockInterface)(nil).RemoveResourceFiltersFromAccount), arg0, arg1)
}

// SyncAllAccounts mocks base method.
func (m *MockInterface) SyncAllAccounts() {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "SyncAllAccounts")
}

// SyncAllAccounts indicates an expected call of SyncAllAccounts.
func (mr *MockInterfaceMockRecorder) SyncAllAccounts() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SyncAllAccounts", reflect.TypeOf((*MockInterface)(nil).SyncAllAccounts))
}

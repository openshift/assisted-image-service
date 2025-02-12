// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/openshift/assisted-image-service/pkg/imagestore (interfaces: ImageStore)

// Package imagestore is a generated GoMock package.
package imagestore

import (
	context "context"
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"
)

// MockImageStore is a mock of ImageStore interface.
type MockImageStore struct {
	ctrl     *gomock.Controller
	recorder *MockImageStoreMockRecorder
}

// MockImageStoreMockRecorder is the mock recorder for MockImageStore.
type MockImageStoreMockRecorder struct {
	mock *MockImageStore
}

// NewMockImageStore creates a new mock instance.
func NewMockImageStore(ctrl *gomock.Controller) *MockImageStore {
	mock := &MockImageStore{ctrl: ctrl}
	mock.recorder = &MockImageStoreMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockImageStore) EXPECT() *MockImageStoreMockRecorder {
	return m.recorder
}

// HaveVersion mocks base method.
func (m *MockImageStore) HaveVersion(arg0, arg1 string) bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "HaveVersion", arg0, arg1)
	ret0, _ := ret[0].(bool)
	return ret0
}

// HaveVersion indicates an expected call of HaveVersion.
func (mr *MockImageStoreMockRecorder) HaveVersion(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "HaveVersion", reflect.TypeOf((*MockImageStore)(nil).HaveVersion), arg0, arg1)
}

// PathForParams mocks base method.
func (m *MockImageStore) PathForParams(arg0, arg1, arg2 string) string {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "PathForParams", arg0, arg1, arg2)
	ret0, _ := ret[0].(string)
	return ret0
}

// PathForParams indicates an expected call of PathForParams.
func (mr *MockImageStoreMockRecorder) PathForParams(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PathForParams", reflect.TypeOf((*MockImageStore)(nil).PathForParams), arg0, arg1, arg2)
}

// Populate mocks base method.
func (m *MockImageStore) Populate(arg0 int, arg1 context.Context) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Populate", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// Populate indicates an expected call of Populate.
func (mr *MockImageStoreMockRecorder) Populate(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Populate", reflect.TypeOf((*MockImageStore)(nil).Populate), arg0, arg1)
}

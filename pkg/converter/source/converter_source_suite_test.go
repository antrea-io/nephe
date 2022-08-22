// Copyright 2022 Antrea Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package source_test

import (
	"testing"

	mock "github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	antreatypes "antrea.io/antrea/pkg/apis/crd/v1alpha2"
	cloud "antrea.io/nephe/apis/crd/v1alpha1"
	"antrea.io/nephe/pkg/converter/source"
	"antrea.io/nephe/pkg/converter/target"
	testing2 "antrea.io/nephe/pkg/testing"
	"antrea.io/nephe/pkg/testing/controllerruntimeclient"
)

var (
	mockCtrl                    *mock.Controller
	mockClient                  *controllerruntimeclient.MockClient
	scheme                      = runtime.NewScheme()
	networkInterfaceIPAddresses = []string{"1.1.1.1", "2.2.2.2"}
	testNamespace               = "test-namespace"
	emptyExternalEntitySources  = map[string]target.ExternalEntitySource{
		"VirtualMachine": &source.VirtualMachineSource{},
	}

	externalEntitySources map[string]target.ExternalEntitySource
)

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	_ = clientgoscheme.AddToScheme(scheme)
	_ = antreatypes.AddToScheme(scheme)
	_ = cloud.AddToScheme(scheme)
})

func commonInitTest() {
	// common setup valid for all tests.
	mockCtrl = mock.NewController(GinkgoT())
	mockClient = controllerruntimeclient.NewMockClient(mockCtrl)
	externalEntitySources = testing2.SetupExternalEntitySources(networkInterfaceIPAddresses, testNamespace)
}

// Testing converting source crd to target crd
func TestConverterSource(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Converter Source Suite")
}

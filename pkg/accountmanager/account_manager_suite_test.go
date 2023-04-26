// Copyright 2023 Antrea Authors.
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

package accountmanager

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	antreanetworking "antrea.io/antrea/pkg/apis/controlplane/v1beta2"
	antreatypes "antrea.io/antrea/pkg/apis/crd/v1alpha2"
	crdv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
)

var (
	scheme = runtime.NewScheme()
)

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	_ = clientgoscheme.AddToScheme(scheme)
	_ = antreatypes.AddToScheme(scheme)
	_ = crdv1alpha1.AddToScheme(scheme)
	_ = antreanetworking.AddToScheme(scheme)
})

func TestAccountManager(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Account Manager Suite")
}

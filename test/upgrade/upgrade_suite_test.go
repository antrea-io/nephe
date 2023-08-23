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

package upgrade

import (
	"flag"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	antreav1alpha1 "antrea.io/antrea/pkg/apis/crd/v1alpha1"
	antreav1alpha2 "antrea.io/antrea/pkg/apis/crd/v1alpha2"
	crdv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/logging"
	"antrea.io/nephe/test/utils"
)

const (
	focusAws   = "test-aws"
	focusAzure = "test-azure"
)

var (
	kubeCtl       *utils.KubeCtl
	k8sClient     client.Client
	cloudVPC      utils.CloudVPC
	scheme        = runtime.NewScheme()
	testFocus     = []string{focusAws, focusAzure}
	preserveSetup = false

	// flags.
	preserveSetupOnFail bool
	supportBundleDir    string
	kubeconfig          string
	cloudProvider       string
	fromVersion         string
	toVersion           string
	chartDir            string
)

func init() {
	flag.StringVar(&fromVersion, "from-version", "", "Helm upgrade version from.")
	flag.StringVar(&toVersion, "to-version", "latest", "Helm upgrade version to.")
	flag.BoolVar(&preserveSetupOnFail, "preserve-setup-on-fail", false, "Preserve the setup if a test failed.")
	flag.StringVar(&supportBundleDir, "support-bundle-dir", "", "Support bundles are saved in this dir when specified.")
	flag.StringVar(&cloudProvider, "cloud-provider", string(runtimev1alpha1.AzureCloudProvider),
		"Cloud Provider. Default is Azure.")
	flag.StringVar(&chartDir, "chart-dir", "../../build/charts/nephe", "Helm chart directory.")
}

func TestUpgrade(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Upgrade Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), logging.UseDevMode()))

	// required fields.
	Expect(fromVersion).ToNot(BeEmpty())

	var err error
	By("Bootstrapping the test environment")
	err = clientgoscheme.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())

	err = crdv1alpha1.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())

	err = antreav1alpha1.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())

	err = antreav1alpha2.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())

	err = runtimev1alpha1.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())

	// setup kubeCtl.
	kubeconfig = flag.Lookup("kubeconfig").Value.(flag.Getter).Get().(string)
	kubeCtl, err = utils.NewKubeCtl(kubeconfig)
	Expect(err).ToNot(HaveOccurred())
	Expect(kubeCtl).ToNot(BeNil())

	// set k8sClient.
	k8sClient, err = utils.NewK8sClient(scheme, "")
	Expect(err).ToNot(HaveOccurred())
	Expect(k8sClient).ToNot(BeNil())

	By("Check cert-manager is ready, may wait longer for docker pull")
	// Increase the timeout for now to get past CI/CD timeout at this point to see what is causing it.
	err = utils.RestartOrWaitDeployment(k8sClient, "cert-manager", "cert-manager", time.Second*240, false)
	Expect(err).ToNot(HaveOccurred())
	err = utils.RestartOrWaitDeployment(k8sClient, "cert-manager-cainjector", "cert-manager", time.Second*120, false)
	Expect(err).ToNot(HaveOccurred())
	err = utils.RestartOrWaitDeployment(k8sClient, "cert-manager-webhook", "cert-manager", time.Second*120, false)
	Expect(err).ToNot(HaveOccurred())

	By("Check antrea controller is ready, may wait longer for docker pull")
	err = utils.RestartOrWaitDeployment(k8sClient, "antrea-controller", "kube-system", time.Second*120, false)
	Expect(err).ToNot(HaveOccurred())

	By("Check nephe controller is not running")
	err = utils.CheckDeploymentConfigured(k8sClient, "nephe-controller", "nephe-system")
	Expect(err).To(HaveOccurred())

	By("Check VM VPCs are ready")
	cloudVPC, err = utils.NewCloudVPC(runtimev1alpha1.CloudProvider(cloudProvider))
	Expect(err).ToNot(HaveOccurred())
	if !cloudVPC.IsConfigured() {
		err := cloudVPC.Reapply(time.Second*600, false)
		Expect(err).ToNot(HaveOccurred())
		logf.Log.Info("VM VPCs created", "Provider", cloudProvider, "VPCID", cloudVPC.GetVPCID())
	}

})

var _ = AfterSuite(func() {
	if preserveSetup == true || kubeCtl == nil {
		return
	}

	// Delete VPC.
	if cloudVPC != nil {
		logf.Log.Info("Initiating VM VPC deletion", "Provider", cloudProvider, "VPCID", cloudVPC.GetVPCID())
		err := cloudVPC.Delete(time.Second * 600)
		Expect(err).ToNot(HaveOccurred())

	}
})

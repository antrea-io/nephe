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

package apiserver

import (
	"bytes"
	"context"
	"net"
	"os"

	logger "github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/apiserver/pkg/registry/rest"
	genericapiserver "k8s.io/apiserver/pkg/server"
	genericoptions "k8s.io/apiserver/pkg/server/options"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	aggregatorclientset "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset"
	controllerruntime "sigs.k8s.io/controller-runtime"

	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	virtualmachineinventory "antrea.io/nephe/pkg/apiserver/registry/inventory/virtualmachine"
	vpcinventory "antrea.io/nephe/pkg/apiserver/registry/inventory/vpc"
	"antrea.io/nephe/pkg/apiserver/registry/virtualmachinepolicy"
	"antrea.io/nephe/pkg/inventory"
)

var (
	// APIService listening port number.
	apiServerPort = 5443
	// Match Nephe Controller Service Name
	nepheControllerSvcName = "nephe-controller-service"
	// nepheServedLabel includes the labels used to select resources served by nephe-controller.
	nepheServedLabel = map[string]string{
		"served-by": "nephe-controller",
	}
)

// ExtraConfig holds custom apiserver config.
type ExtraConfig struct {
	// virtual machine policy indexer.
	npTrackerIndexer cache.Indexer
	cloudInventory   inventory.Interface
}

// Config defines the config for the apiserver.
type Config struct {
	GenericConfig *genericapiserver.RecommendedConfig
	ExtraConfig   ExtraConfig
}

func NewConfig(codecs serializer.CodecFactory, npTrackerIndexer cache.Indexer, cloudInventory inventory.Interface,
	certDir string) (*Config, error) {
	recommend := genericoptions.NewRecommendedOptions("", nil)
	serverConfig := genericapiserver.NewRecommendedConfig(codecs)
	recommend.SecureServing.BindPort = apiServerPort

	// tls.crt and tls.key is populated by cert-manager injector.
	recommend.SecureServing.ServerCert.PairName = "tls"
	recommend.SecureServing.ServerCert.CertDirectory = certDir
	if err := recommend.SecureServing.MaybeDefaultWithSelfSignedCerts(nepheControllerSvcName,
		[]string{}, []net.IP{net.ParseIP("127.0.0.1")}); err != nil {
		return nil, err
	}

	if err := recommend.SecureServing.ApplyTo(&serverConfig.SecureServing, &serverConfig.LoopbackClientConfig); err != nil {
		return nil, err
	}

	// Check if KUBECONFIG env is set. If so, point the api service kubeconfig to the path set in the env variable.
	// This will override the default in cluster config used by the api service.
	kubeConfigPath := os.Getenv(clientcmd.RecommendedConfigPathEnvVar)
	if len(kubeConfigPath) > 0 {
		recommend.CoreAPI = &genericoptions.CoreAPIOptions{CoreAPIKubeconfigPath: kubeConfigPath}
		if err := recommend.CoreAPI.ApplyTo(serverConfig); err != nil {
			return nil, err
		}
		recommend.Authentication.RemoteKubeConfigFile = kubeConfigPath
		recommend.Authorization.RemoteKubeConfigFile = kubeConfigPath
	}

	if err := recommend.Authentication.ApplyTo(&serverConfig.Authentication, serverConfig.SecureServing,
		serverConfig.OpenAPIConfig); err != nil {
		return nil, err
	}
	if err := recommend.Authorization.ApplyTo(&serverConfig.Authorization); err != nil {
		return nil, err
	}
	config := &Config{
		GenericConfig: serverConfig,
		ExtraConfig: ExtraConfig{
			npTrackerIndexer: npTrackerIndexer,
			cloudInventory:   cloudInventory,
		},
	}
	return config, nil
}

// NepheControllerAPIServer contains state for a Kubernetes cluster master/api server.
type NepheControllerAPIServer struct {
	genericAPIServer *genericapiserver.GenericAPIServer
	logger           logger.Logger
}

func (s *NepheControllerAPIServer) Start(stop context.Context) error {
	s.logger.Info("Starting APIServer")
	err := s.genericAPIServer.PrepareRun().Run(stop.Done())
	if err != nil {
		s.logger.Error(err, "Failed to run APIServer")
	}
	return err
}

func (s *NepheControllerAPIServer) SetupWithManager(
	mgr controllerruntime.Manager,
	npTrackerIndexer cache.Indexer,
	cloudInventory inventory.Interface,
	certDir string,
	logger logger.Logger) error {
	s.logger = logger
	codecs := serializer.NewCodecFactory(mgr.GetScheme())

	apiConfig, err := NewConfig(codecs, npTrackerIndexer, cloudInventory, certDir)
	if err != nil {
		s.logger.Error(err, "unable to create APIServer config")
		return err
	}
	if s.genericAPIServer, err = apiConfig.Complete().New(mgr.GetScheme(), codecs, s.logger); err != nil {
		s.logger.Error(err, "unable to create APIServer")
		return err
	}
	if err = s.syncAPIServices(certDir); err != nil {
		s.logger.Error(err, "failed to sync CA cert with APIService")
		return err
	}
	if err = mgr.Add(s); err != nil {
		return err
	}
	return nil
}

// syncAPIServices updates nephe controller APIService with CA bundle.
func (s *NepheControllerAPIServer) syncAPIServices(certDir string) error {
	clientset, err := aggregatorclientset.NewForConfig(controllerruntime.GetConfigOrDie())
	if err != nil {
		return err
	}

	listOption := metav1.ListOptions{
		LabelSelector: metav1.FormatLabelSelector(&metav1.LabelSelector{
			MatchLabels: nepheServedLabel})}
	nepheAPIServices, err := clientset.ApiregistrationV1().APIServices().List(context.TODO(), listOption)
	if err != nil {
		return err
	}

	if len(nepheAPIServices.Items) == 0 {
		return nil
	}

	s.logger.Info("Syncing CA certificate with APIServices")
	caCert, err := getCaCert(certDir)
	if err != nil {
		return err
	}
	for i := range nepheAPIServices.Items {
		apiService := nepheAPIServices.Items[i]
		if bytes.Equal(apiService.Spec.CABundle, caCert) {
			continue
		}
		apiService.Spec.CABundle = caCert
		if _, err := clientset.ApiregistrationV1().APIServices().Update(context.TODO(), &apiService, metav1.UpdateOptions{}); err != nil {
			s.logger.Error(err, "failed to update CA cert of APIService", "name", apiService.Name)
			return err
		}
		s.logger.Info("Updated CA cert of APIService", "name", apiService.Name)
	}
	return nil
}

// getCaCert gets the content of CA bundle from cert file.
func getCaCert(certDir string) ([]byte, error) {
	filePath := certDir + "/ca.crt"
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	return content, nil
}

type completedConfig struct {
	GenericConfig genericapiserver.CompletedConfig
	ExtraConfig   *ExtraConfig
}

// CompletedConfig embeds a private pointer that cannot be instantiated outside of this package.
type CompletedConfig struct {
	*completedConfig
}

// Complete fills in any fields not set that are required to have valid data. It's mutating the receiver.
func (cfg *Config) Complete() CompletedConfig {
	c := completedConfig{
		cfg.GenericConfig.Complete(),
		&cfg.ExtraConfig,
	}
	c.GenericConfig.Version = &version.Info{
		Major: "1",
		Minor: "0",
	}
	return CompletedConfig{&c}
}

// New returns a new instance of NepheControllerAPIServer from the given config.
func (c completedConfig) New(scheme *runtime.Scheme, codecs serializer.CodecFactory,
	logger logger.Logger) (*genericapiserver.GenericAPIServer, error) {
	genericServer, err := c.GenericConfig.New("nephe-controller-apiserver", genericapiserver.NewEmptyDelegate())
	if err != nil {
		return nil, err
	}

	vpcStorage := vpcinventory.NewREST(c.ExtraConfig.cloudInventory, logger.WithName("VpcInventory"))
	vmpStorage := virtualmachinepolicy.NewREST(c.ExtraConfig.npTrackerIndexer, logger.WithName("VirtualMachinePolicy"))
	vmStorage := virtualmachineinventory.NewREST(c.ExtraConfig.cloudInventory, logger.WithName("VirtualMachineInventory"))

	cpGroup := genericapiserver.NewDefaultAPIGroupInfo(runtimev1alpha1.GroupVersion.Group, scheme, metav1.ParameterCodec, codecs)
	cpv1alpha1Storage := map[string]rest.Storage{}
	cpv1alpha1Storage["vpc"] = vpcStorage
	cpv1alpha1Storage["virtualmachinepolicy"] = vmpStorage
	cpv1alpha1Storage["virtualmachine"] = vmStorage

	cpGroup.VersionedResourcesStorageMap["v1alpha1"] = cpv1alpha1Storage

	if err := genericServer.InstallAPIGroup(&cpGroup); err != nil {
		return nil, err
	}
	return genericServer, nil
}

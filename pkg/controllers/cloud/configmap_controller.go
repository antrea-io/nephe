// Copyright 2023 Antrea Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cloud

import (
	"context"
	"fmt"
	"os"

	"github.com/go-logr/logr"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"antrea.io/nephe/pkg/cloud-provider/securitygroup"
	"antrea.io/nephe/pkg/controllers/config"
)

type ConfigMapReconciler struct {
	client.Client
	Log              logr.Logger
	Scheme           *runtime.Scheme
	ControllerConfig *config.ControllerConfig
}

func (r *ConfigMapReconciler) Reconcile(_ context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("configmap", req.NamespacedName)
	// Process nephe-config ConfigMaps only.
	if req.Namespace != config.ConfigMapNamespace || req.Name != config.ConfigMapName {
		return ctrl.Result{}, nil
	}

	configMap := corev1.ConfigMap{}
	if err := r.Get(context.TODO(), req.NamespacedName, &configMap); err != nil {
		return ctrl.Result{}, err
	}
	newControllerConfig := &config.ControllerConfig{}
	if err := yaml.UnmarshalStrict([]byte(configMap.Data["nephe-controller.conf"]), newControllerConfig); err != nil {
		r.Log.Error(err, "error in unmarshalling ConfigMap")
		return ctrl.Result{}, err
	}

	if err := r.setCloudResourcePrefix(newControllerConfig.CloudResourcePrefix); err != nil {
		// Unable to continue as prefix is not valid.
		r.Log.Error(err, "exiting nephe-controller")
		os.Exit(1)
	}
	return ctrl.Result{}, nil
}

// setCloudResourcePrefix sets the CloudResourcePrefix, returns error when invalid.
func (r *ConfigMapReconciler) setCloudResourcePrefix(prefix string) error {
	if len(prefix) == 0 {
		r.Log.Info("Setting CloudResourcePrefix to default", "Old", r.ControllerConfig.CloudResourcePrefix,
			"New", config.DefaultCloudResourcePrefix)
		r.ControllerConfig.CloudResourcePrefix = config.DefaultCloudResourcePrefix
		return nil
	}

	if !(config.ValidateName(prefix)) {
		return fmt.Errorf("invalid CloudResourcePrefix %s, "+
			"First and last characters should be alphanumeric and "+
			"'-' characters are allowed only in the middle", prefix)
	}

	// Update the CloudResourcePrefix
	if r.ControllerConfig.CloudResourcePrefix != prefix {
		r.Log.V(1).Info("Updating the CloudResourcePrefix",
			"Old", r.ControllerConfig.CloudResourcePrefix, "New", prefix)
		r.ControllerConfig.CloudResourcePrefix = prefix
	}
	return nil
}

// SetupWithManager registers ConfigMapReconciler with manager.
func (r *ConfigMapReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.ControllerConfig = &config.ControllerConfig{
		CloudResourcePrefix: config.DefaultCloudResourcePrefix,
	}
	securitygroup.SetCloudResourcePrefix(&r.ControllerConfig.CloudResourcePrefix)
	return ctrl.NewControllerManagedBy(mgr).For(&corev1.ConfigMap{}).Complete(r)
}

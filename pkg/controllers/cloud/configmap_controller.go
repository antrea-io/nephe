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
	"antrea.io/nephe/pkg/logging"
)

type ConfigMapHandler func(controllerConfig *config.ControllerConfig)

// controllerConfigType use to identify the fields in ControllerConfig
type controllerConfigType int

type ConfigMapReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	log    logr.Logger

	ControllerConfig     *config.ControllerConfig
	pendingSyncCount     int
	configmapHandlerFunc map[controllerConfigType][]ConfigMapHandler
}

const (
	cloudSyncIntervalConfig controllerConfigType = iota
)

var (
	configmapReconciler *ConfigMapReconciler
)

func init() {
	configmapReconciler = &ConfigMapReconciler{}
	configmapReconciler.ControllerConfig = &config.ControllerConfig{
		CloudResourcePrefix: config.DefaultCloudResourcePrefix,
		CloudSyncInterval:   config.DefaultCloudSyncInterval,
	}
	configmapReconciler.configmapHandlerFunc = make(map[controllerConfigType][]ConfigMapHandler)
	configmapReconciler.pendingSyncCount = 1
}

// GetConfigMapControllerInstance gets the ConfigMap controller instance.
func GetConfigMapControllerInstance() *ConfigMapReconciler {
	return configmapReconciler
}

// Configure configures the client, scheme and log.
func (r *ConfigMapReconciler) Configure(client client.Client, scheme *runtime.Scheme) *ConfigMapReconciler {
	r.Client = client
	r.Scheme = scheme
	r.log = logging.GetLogger("controllers").WithName("ConfigMap")
	return r
}

func (r *ConfigMapReconciler) Reconcile(_ context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.log.WithValues("configmap", req.NamespacedName)
	// Process 'nephe-config' ConfigMap only.
	if req.Namespace != config.ConfigMapNamespace || req.Name != config.ConfigMapName {
		return ctrl.Result{}, nil
	}

	configMap := corev1.ConfigMap{}
	if err := r.Get(context.TODO(), req.NamespacedName, &configMap); err != nil {
		return ctrl.Result{}, err
	}
	newControllerConfig := &config.ControllerConfig{}
	if err := yaml.UnmarshalStrict([]byte(configMap.Data["nephe-controller.conf"]), newControllerConfig); err != nil {
		r.log.Error(err, "error in unmarshalling ConfigMap")
		return ctrl.Result{}, err
	}
	// set the cloud resource prefix.
	if err := r.setCloudResourcePrefix(newControllerConfig.CloudResourcePrefix); err != nil {
		// Unable to continue as prefix is not valid.
		r.log.Error(err, "exiting nephe-controller")
		os.Exit(1)
	}
	r.updatePendingSyncCountAndStatus()
	// set the cloud sync interval.
	r.setCloudSyncIntervalConfig(newControllerConfig.CloudSyncInterval)

	return ctrl.Result{}, nil
}

// setCloudResourcePrefix sets the CloudResourcePrefix, returns error when invalid.
func (r *ConfigMapReconciler) setCloudResourcePrefix(prefix string) error {
	if len(prefix) == 0 {
		r.log.Info("Setting CloudResourcePrefix to default", "CloudResourcePrefix", config.DefaultCloudResourcePrefix)
		r.ControllerConfig.CloudResourcePrefix = config.DefaultCloudResourcePrefix
		return nil
	}

	if !(config.ValidateName(prefix)) {
		return fmt.Errorf("invalid CloudResourcePrefix %s, "+
			"Only alphanumeric and '-' characters are allowed."+
			" Special character '-' is only allowed at the middle", prefix)
	}

	// Update the CloudResourcePrefix.
	if r.ControllerConfig.CloudResourcePrefix != prefix {
		r.log.Info("Updating the CloudResourcePrefix", "CloudResourcePrefix", prefix)
		r.ControllerConfig.CloudResourcePrefix = prefix
	}
	return nil
}

// updatePendingSyncCountAndStatus decrements the pendingSyncCount and when
// pendingSyncCount is 0, sets the sync status.
func (r *ConfigMapReconciler) updatePendingSyncCountAndStatus() {
	if r.pendingSyncCount == 1 {
		r.pendingSyncCount--
		GetControllerSyncStatusInstance().SetControllerSyncStatus(ControllerTypeCM)
	}
}

// setCloudSyncInterval sets the CloudSyncInterval.
func (r *ConfigMapReconciler) setCloudSyncIntervalConfig(interval int64) {
	if interval == 0 {
		r.log.Info("Setting CloudSyncInterval to default", "CloudSyncInterval", config.DefaultCloudSyncInterval)
		r.ControllerConfig.CloudSyncInterval = config.DefaultCloudSyncInterval
	} else if interval < config.DefaultCloudSyncInterval {
		r.ControllerConfig.CloudSyncInterval = config.DefaultCloudSyncInterval
		r.log.Info(fmt.Sprintf("Invalid CloudSyncInterval %d, "+
			"CloudSyncInterval should be >= %d seconds. So using default interval", interval, config.DefaultCloudSyncInterval))
	} else if interval != r.ControllerConfig.CloudSyncInterval {
		r.log.Info("Updating the CloudSyncInterval", "CloudSyncInterval", interval)
		r.ControllerConfig.CloudSyncInterval = interval
	} else {
		return
	}
	r.notifyConfigMapHandlers(cloudSyncIntervalConfig)
}

// registerConfigMapHandlers registers the ConfigMapHandler functions.
func (r *ConfigMapReconciler) registerConfigMapHandlers(handler ConfigMapHandler, configType controllerConfigType) {
	r.configmapHandlerFunc[configType] = append(r.configmapHandlerFunc[configType], handler)
}

// notifyConfigMapHandlers notifies the ConfigMapHandlers registered functions.
func (r *ConfigMapReconciler) notifyConfigMapHandlers(configType controllerConfigType) {
	for i := 0; i < len(r.configmapHandlerFunc[configType]); i++ {
		r.configmapHandlerFunc[configType][i](r.ControllerConfig)
	}
}

// SetupWithManager registers ConfigMapReconciler with manager.
func (r *ConfigMapReconciler) SetupWithManager(mgr ctrl.Manager) error {
	securitygroup.SetCloudResourcePrefix(&r.ControllerConfig.CloudResourcePrefix)
	return ctrl.NewControllerManagedBy(mgr).For(&corev1.ConfigMap{}).Complete(r)
}

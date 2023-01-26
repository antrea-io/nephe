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

package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
	"gopkg.in/yaml.v2"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	controllerclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	controllers "antrea.io/nephe/pkg/controllers/cloud"
	"antrea.io/nephe/pkg/controllers/config"
)

// ConfigMapValidator is used to validate ConfigMap.
type ConfigMapValidator struct {
	Client                controllerclient.Client
	Log                   logr.Logger
	decoder               *admission.Decoder
	NpControllerInterface controllers.NetworkPolicyController
}

// Handle handles admission request for ConfigMap.
func (v *ConfigMapValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	v.Log.V(1).Info("Received admission webhook request", "Name", req.Name, "Operation", req.Operation)
	switch req.Operation {
	case admissionv1.Create:
		return v.validateCreate()
	case admissionv1.Update:
		return v.validateUpdate(req)
	case admissionv1.Delete:
		return v.validateDelete()
	default:
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("invalid admission webhook request"))
	}
}

// InjectDecoder injects the decoder.
func (v *ConfigMapValidator) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}

// getCloudResourcePrefix get the CloudResourcePrefix from the ConfigMap.
func (v *ConfigMapValidator) getCloudResourcePrefix(configMap *corev1.ConfigMap) (string, error) {
	configData := configMap.Data["nephe-controller.conf"]
	controllerConfig := &config.ControllerConfig{}
	err := yaml.UnmarshalStrict([]byte(configData), controllerConfig)
	if err != nil {
		return "", err
	}
	// When cloud resource prefix is empty use default
	if len(controllerConfig.CloudResourcePrefix) == 0 {
		controllerConfig.CloudResourcePrefix = config.DefaultCloudResourcePrefix
	}
	return controllerConfig.CloudResourcePrefix, nil
}

// validateCreate validates the ConfigMap create.
func (v *ConfigMapValidator) validateCreate() admission.Response {
	return admission.Allowed("")
}

// validateUpdate validates the ConfigMap and CloudResourcePrefix update.
func (v *ConfigMapValidator) validateUpdate(req admission.Request) admission.Response {
	newConfigMap := &corev1.ConfigMap{}
	err := v.decoder.Decode(req, newConfigMap)
	if err != nil {
		v.Log.Error(err, "failed to decode ConfigMap", "ConfigMapValidator", req.Name)
		return admission.Errored(http.StatusBadRequest, err)
	}

	// Validate ConfigMaps only with name as nephe-config
	if newConfigMap.GetName() != config.ConfigMapName {
		return admission.Allowed("")
	}

	newCloudResourcePrefix, err := v.getCloudResourcePrefix(newConfigMap)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	oldConfigMap := &corev1.ConfigMap{}
	err = json.Unmarshal(req.OldObject.Raw, &oldConfigMap)
	if err != nil {
		v.Log.Error(err, "failed to decode old ConfigMap", "ConfigMapValidator", req.Name)
		return admission.Errored(http.StatusBadRequest, err)
	}

	oldCloudResourcePrefix, err := v.getCloudResourcePrefix(oldConfigMap)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if newCloudResourcePrefix == oldCloudResourcePrefix {
		return admission.Allowed("")
	}
	if v.NpControllerInterface.IsCloudResourceCreated() {
		return admission.Denied(fmt.Sprintf("cloud resource is already created with CloudResourcePrefix %s. "+
			"Please delete the resource and try or use the same CloudResourcePrefix",
			oldCloudResourcePrefix))
	}
	if len(newCloudResourcePrefix) > 0 {
		if !config.ValidateName(newCloudResourcePrefix) {
			return admission.Denied(fmt.Sprintf("invalid CloudResourcePrefix %s, "+
				"First and last characters should be alphanumeric and '-' characters are allowed only in the middle.",
				newCloudResourcePrefix))
		}
	}
	return admission.Allowed("")
}

// validateDelete validates the ConfigMap delete.
func (v *ConfigMapValidator) validateDelete() admission.Response {
	return admission.Allowed("")
}

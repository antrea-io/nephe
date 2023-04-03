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

package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	controllerclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	crdv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
)

// nolint:lll
// +kubebuilder:webhook:verbs=update;delete,path=/validate-v1-secret,mutating=false,failurePolicy=fail,groups="",resources=secrets,versions=v1,name=vsecret.kb.io,sideEffects=None,admissionReviewVersions=v1;v1beta1

// SecretValidator is used to validate Secret.
type SecretValidator struct {
	Client  controllerclient.Client
	Log     logr.Logger
	decoder *admission.Decoder
}

// Handle handles admission requests for Secret.
func (v *SecretValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	v.Log.V(1).Info("Received admission webhook request", "Name", req.Name, "Operation", req.Operation)
	switch req.Operation {
	case admissionv1.Create:
		return v.validateCreate()
	case admissionv1.Update:
		return v.validateUpdate()
	case admissionv1.Delete:
		return v.validateDelete(req)
	default:
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("invalid admission webhook request"))
	}
}

// InjectDecoder injects the decoder.
func (v *SecretValidator) InjectDecoder(d *admission.Decoder) error { //nolint:unparam
	v.decoder = d
	return nil
}

// getCPABySecret returns nil only when the Secret is not used by any CloudProvideAccount CR,
// otherwise the dependent CloudProvideAccount CR will be returned.
func (v *SecretValidator) getCPABySecret(s *corev1.Secret) (error, *crdv1alpha1.CloudProviderAccount) {
	cpaList := &crdv1alpha1.CloudProviderAccountList{}
	err := v.Client.List(context.TODO(), cpaList, &controllerclient.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to get CloudProviderAccount list, err:%v", err), nil
	}

	for _, cpa := range cpaList.Items {
		if cpa.Spec.AWSConfig != nil {
			if cpa.Spec.AWSConfig.SecretRef.Name == s.Name &&
				cpa.Spec.AWSConfig.SecretRef.Namespace == s.Namespace {
				return nil, &cpa
			}
		}

		if cpa.Spec.AzureConfig != nil {
			if cpa.Spec.AzureConfig.SecretRef.Name == s.Name &&
				cpa.Spec.AzureConfig.SecretRef.Namespace == s.Namespace {
				return nil, &cpa
			}
		}
	}
	return nil, nil
}

// validateCreate does not deny Secret creation.
func (v *SecretValidator) validateCreate() admission.Response { // nolint: unparam
	return admission.Allowed("")
}

// validateUpdate denies Secret update, if the Secret key is referred in a CloudProviderAccount.
func (v *SecretValidator) validateUpdate() admission.Response {
	return admission.Allowed("")
}

// validateDelete denies Secret deletion if the Secret is referred in a CloudProviderAccount.
func (v *SecretValidator) validateDelete(req admission.Request) admission.Response {
	oldSecret := &corev1.Secret{}
	if req.OldObject.Raw != nil {
		if err := json.Unmarshal(req.OldObject.Raw, &oldSecret); err != nil {
			v.Log.Error(err, "Failed to decode old Secret", "SecretValidator", req.Name)
			return admission.Errored(http.StatusBadRequest, err)
		}
	}
	err, cpa := v.getCPABySecret(oldSecret)
	if err != nil {
		return admission.Denied(err.Error())
	}
	if cpa != nil {
		v.Log.Error(nil, "The Secret is referred by a CloudProviderAccount. Cannot delete it,",
			"Secret", oldSecret.Name, "CloudProviderAccount", cpa.Name)
		return admission.Denied(fmt.Sprintf("the Secret %s is referred by a CloudProviderAccount %s. "+
			"Please delete the CloudProviderAccount first", oldSecret.Name, cpa.Name))
	}
	return admission.Allowed("")
}

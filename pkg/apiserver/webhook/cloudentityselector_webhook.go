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
	"strings"

	"github.com/go-logr/logr"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	controllerclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"antrea.io/nephe/apis/crd/v1alpha1"
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/controllers/cloud"
	"antrea.io/nephe/pkg/controllers/utils"
)

var (
	errorMsgSameVPCMatchID            = "two vpcMatch selectors configured with same matchID"
	errorMsgSameVMMatchID             = "two vmMatch selectors configured with same matchID"
	errorMsgSameVMMatchName           = "two vmMatch selectors configured with same matchName"
	errorMsgSameVMAndVPCMatch         = "two selectors configured with same vpcMatch and vmMatch criteria"
	errorMsgUnsupportedVPCMatchName01 = "matchName is not supported in vpcMatch, use matchID instead of matchName"
	errorMsgUnsupportedAgented        = "vpc matchName with agented flag set to true is not supported, use vpc " +
		"matchID instead"
	errorMsgUnsupportedVPCMatchName02 = "vpc matchName with either vm matchID or vm matchName is not supported, " +
		"use vpc matchID instead of vpc matchName"
	errorMsgMatchIDNameTogether = "matchID and matchName are not supported together, " +
		"configure either matchID or matchName in an EntityMatch"
	errorMsgAccountNameUpdate    = "account name update not allowed"
	errorMsgSameAccountUsage     = "account in a namespace can be owner of only one CloudEntitySelector"
	errorMsgOwnerAccountNotFound = "failed to find owner account"
	errorMsgInvalidCloudType     = "invalid cloud provider type"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// nolint:lll
// +kubebuilder:webhook:path=/mutate-crd-cloud-antrea-io-v1alpha1-cloudentityselector,mutating=true,failurePolicy=fail,groups=crd.cloud.antrea.io,resources=cloudentityselectors,verbs=create;update,versions=v1alpha1,name=mcloudentityselector.kb.io,sideEffects=None,admissionReviewVersions=v1;v1beta1

// CESMutator is used to mutate CES object.
type CESMutator struct {
	Client  controllerclient.Client
	Sh      *runtime.Scheme
	Log     logr.Logger
	decoder *admission.Decoder
}

// Handle handles mutator admission requests for CES.
func (v *CESMutator) Handle(_ context.Context, req admission.Request) admission.Response {
	selector := &v1alpha1.CloudEntitySelector{}
	err := v.decoder.Decode(req, selector)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	v.Log.V(1).Info("CES Mutator", "name", selector.Name)
	// make sure selector has owner reference.
	// set owner account only if resource CloudProviderAccount with CloudEntitySelector account name in this Namespace exists.
	ownerReference := metav1.GetControllerOf(selector)
	accountNameSpacedName := &types.NamespacedName{
		Namespace: selector.Namespace,
		Name:      selector.Spec.AccountName,
	}
	ownerAccount := &v1alpha1.CloudProviderAccount{}
	err = v.Client.Get(context.TODO(), *accountNameSpacedName, ownerAccount)
	if err != nil {
		v.Log.Error(err, errorMsgOwnerAccountNotFound, "account", *accountNameSpacedName)
		return admission.Errored(http.StatusBadRequest, err)
	}
	cloudProviderType, err := utils.GetAccountProviderType(ownerAccount)
	if err != nil {
		v.Log.Error(err, errorMsgInvalidCloudType)
		return admission.Errored(http.StatusBadRequest, err)
	}
	if cloudProviderType == runtimev1alpha1.AzureCloudProvider {
		for _, m := range selector.Spec.VMSelector {
			// Convert azure ID to lower case, because Azure API do not preserve case info.
			// Tags are required to be lower case when used in nephe.
			if m.VpcMatch != nil {
				m.VpcMatch.MatchID = strings.ToLower(m.VpcMatch.MatchID)
				m.VpcMatch.MatchName = strings.ToLower(m.VpcMatch.MatchName)
			}
			for _, vmMatch := range m.VMMatch {
				vmMatch.MatchID = strings.ToLower(vmMatch.MatchID)
				vmMatch.MatchName = strings.ToLower(vmMatch.MatchName)
			}
		}
	}
	if ownerReference == nil {
		err = controllerutil.SetControllerReference(ownerAccount, selector, v.Sh)
		if err != nil {
			v.Log.Error(err, "failed to set owner account", "CloudEntitySelector", selector, "account", *accountNameSpacedName)
			return admission.Errored(http.StatusBadRequest, err)
		}
	}

	marshaledSelector, err := json.Marshal(selector)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledSelector)
}

// InjectDecoder injects the decoder.
func (v *CESMutator) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// nolint:lll
// +kubebuilder:webhook:verbs=create;update;delete,path=/validate-crd-cloud-antrea-io-v1alpha1-cloudentityselector,mutating=false,failurePolicy=fail,groups=crd.cloud.antrea.io,resources=cloudentityselectors,versions=v1alpha1,name=vcloudentityselector.kb.io,sideEffects=None,admissionReviewVersions=v1;v1beta1

// CESValidator is used to validate CES object.
type CESValidator struct {
	Client  controllerclient.Client
	Log     logr.Logger
	decoder *admission.Decoder
}

// Handle handles validator admission requests for CES.
func (v *CESValidator) Handle(_ context.Context, req admission.Request) admission.Response {
	v.Log.V(1).Info("Received CES admission webhook request", "Name", req.Name, "Operation", req.Operation)
	if !cloud.GetControllerSyncStatusInstance().IsControllerSynced(cloud.ControllerTypeCES) {
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("%v %v, retry after sometime",
			cloud.ControllerTypeCES.String(), cloud.ErrorMsgControllerInitializing))
	}
	switch req.Operation {
	case admissionv1.Create:
		return v.validateCreate(req)
	case admissionv1.Update:
		return v.validateUpdate(req)
	case admissionv1.Delete:
		return v.validateDelete(req)
	default:
		return admission.Errored(http.StatusBadRequest, fmt.Errorf(errorMsgInvalidRequest))
	}
}

// InjectDecoder injects the decoder.
func (v *CESValidator) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}

// ValidateCreate implements webhook validations for CES add operation.
func (v *CESValidator) validateCreate(req admission.Request) admission.Response {
	selector := &v1alpha1.CloudEntitySelector{}
	err := v.decoder.Decode(req, selector)
	if err != nil {
		v.Log.Error(err, "Failed to decode CloudEntitySelector", "CESValidator", req.Name)
		return admission.Errored(http.StatusBadRequest, err)
	}
	// make sure owner exists. Default will try to populate if owner with provided account name exists.
	if selector.GetObjectMeta().GetOwnerReferences() == nil {
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("%s %v", errorMsgOwnerAccountNotFound,
			selector.Spec.AccountName))
	}

	// make sure no existing CloudEntitySelector has same account as this CloudEntitySelector as its owner.
	if err := v.validateNoOwnerConflict(selector); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	// make sure unsupported match combinations are not configured.
	if err := v.validateMatchSections(selector); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	return admission.Allowed("")
}

// validateNoOwnerConflict makes sure no two CloudEntitySelectors have same account owner.
func (r *CESValidator) validateNoOwnerConflict(selector *v1alpha1.CloudEntitySelector) error {
	cloudEntitySelectorList := &v1alpha1.CloudEntitySelectorList{}
	err := r.Client.List(context.TODO(), cloudEntitySelectorList, controllerclient.InNamespace(selector.Namespace))
	if err != nil {
		return err
	}

	namespace := selector.GetNamespace()
	accName := selector.Spec.AccountName
	for _, item := range cloudEntitySelectorList.Items {
		if strings.Compare(strings.TrimSpace(item.Spec.AccountName), strings.TrimSpace(accName)) == 0 {
			return fmt.Errorf("CloudEntitySelector %v/%v with owner as account %v/%v already exists."+
				"(%s)", namespace, item.GetName(), namespace, accName, errorMsgSameAccountUsage)
		}
	}

	return nil
}

// ValidateUpdate implements webhook validations for CES update operation.
func (v *CESValidator) validateUpdate(req admission.Request) admission.Response {
	newSelector := &v1alpha1.CloudEntitySelector{}
	err := v.decoder.Decode(req, newSelector)
	if err != nil {
		v.Log.Error(err, "Failed to decode CloudEntitySelector", "CESValidator", req.Name)
		return admission.Errored(http.StatusBadRequest, err)
	}
	oldSelector := &v1alpha1.CloudEntitySelector{}
	if req.OldObject.Raw != nil {
		if err := json.Unmarshal(req.OldObject.Raw, &oldSelector); err != nil {
			v.Log.Error(err, "Failed to decode old CloudEntitySelector", "CESValidator", req.Name)
			return admission.Errored(http.StatusBadRequest, err)
		}
	}

	// account name update not allowed.
	oldAccName := strings.TrimSpace(oldSelector.Spec.AccountName)
	newAccountName := strings.TrimSpace(newSelector.Spec.AccountName)
	if strings.Compare(oldAccName, newAccountName) != 0 {
		return admission.Errored(http.StatusBadRequest, fmt.Errorf(
			"%s (old:%v, new:%v)", errorMsgAccountNameUpdate, oldAccName, newAccountName))
	}

	// make sure unsupported match combinations are not configured.
	if err := v.validateMatchSections(newSelector); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	return admission.Allowed("")
}

// ValidateDelete implements webhook validations for CES delete operation.
func (v *CESValidator) validateDelete(_ admission.Request) admission.Response { //nolint:unparam
	// TODO(user): fill in your validation logic upon object deletion.
	return admission.Allowed("")
}

// GetOwnerAccount fetches the CPA account owning the current CES.
func (v *CESValidator) GetOwnerAccount(selector *v1alpha1.CloudEntitySelector) (*v1alpha1.CloudProviderAccount, error) {
	accountNameSpacedName := &types.NamespacedName{
		Namespace: selector.Namespace,
		Name:      selector.Spec.AccountName,
	}
	ownerAccount := &v1alpha1.CloudProviderAccount{}
	err := v.Client.Get(context.TODO(), *accountNameSpacedName, ownerAccount)
	if err != nil {
		v.Log.Error(err, errorMsgOwnerAccountNotFound, "CloudEntitySelector", selector,
			"account", *accountNameSpacedName)
		return nil, err
	}
	return ownerAccount, err
}

// validateMatchSections checks for unsupported selector match combinations and errors out.
func (v *CESValidator) validateMatchSections(selector *v1alpha1.CloudEntitySelector) error {
	// MatchID and MatchName are not supported together in an EntityMatch, applicable for both vpcMatch, vmMatch section.
	for _, m := range selector.Spec.VMSelector {
		if m.VpcMatch != nil {
			if len(strings.TrimSpace(m.VpcMatch.MatchID)) != 0 &&
				len(strings.TrimSpace(m.VpcMatch.MatchName)) != 0 {
				return fmt.Errorf("%s", errorMsgMatchIDNameTogether)
			}
		}
		for _, vmMatch := range m.VMMatch {
			if len(strings.TrimSpace(vmMatch.MatchID)) != 0 &&
				len(strings.TrimSpace(vmMatch.MatchName)) != 0 {
				return fmt.Errorf("%s", errorMsgMatchIDNameTogether)
			}
		}
	}

	ownerAccount, err := v.GetOwnerAccount(selector)
	if err != nil {
		return err
	}
	cloudProviderType, err := utils.GetAccountProviderType(ownerAccount)
	if err != nil {
		v.Log.Error(err, errorMsgInvalidCloudType)
		return err
	}

	// In Azure, Vpc Name is not supported in vpcMatch.
	// In AWS, Vpc name(in vpcMatch section) with either vm id or vm name(in vmMatch section) is not supported.
	if cloudProviderType == runtimev1alpha1.AzureCloudProvider {
		for _, m := range selector.Spec.VMSelector {
			if m.VpcMatch != nil && len(strings.TrimSpace(m.VpcMatch.MatchName)) != 0 {
				return fmt.Errorf(errorMsgUnsupportedVPCMatchName01)
			}
		}
	} else {
		for _, m := range selector.Spec.VMSelector {
			if m.VpcMatch != nil && len(strings.TrimSpace(m.VpcMatch.MatchName)) != 0 {
				for _, vmMatch := range m.VMMatch {
					if len(strings.TrimSpace(vmMatch.MatchID)) != 0 ||
						len(strings.TrimSpace(vmMatch.MatchName)) != 0 {
						return fmt.Errorf(errorMsgUnsupportedVPCMatchName02)
					}
				}
				if m.Agented {
					return fmt.Errorf(errorMsgUnsupportedAgented)
				}
			}
		}
	}

	// Block ambiguous match combinations as it can result in unpredictable behavior w.r.t agented configuration.
	if err := v.validateMatchCombinations(selector); err != nil {
		return err
	}

	return nil
}

// validateMatchCombinations function validates VMSelector to find if it conflicts with other VMSelectors.
// Block same VPC ID configuration in two VMSelectors with only vpcMatch section.
// Block same VM ID configuration in any two VMSelectors, same is not applicable for VM Name as VM Name need not be unique.
// Block same combination of VPC ID and VM ID configuration in any two VMSelectors.
// Block same combination of VPC ID and VM Name configuration in any two VMSelectors.
// Block same VM Name configuration in any two VMSelectors with only VMMatch section, when used along with VPCMatch, it is allowed.
func (v *CESValidator) validateMatchCombinations(selector *v1alpha1.CloudEntitySelector) error {
	// vpcIDOnlyMatch map - VPC ID as key for selector with only vpcMatch matchID.
	// vmIDOnlyMatch map - VM ID as key for selector with only vmMatch matchID.
	// vmNameOnlyMatch map - VM Name as key for selector with only vmMatch matchName.
	// vmIDWithVpcMatch map - VM ID and VPC ID as key for selector with vpcMatch matchID and vmMatch matchID.
	// vmNameWithVpcMatch map - VM Name and VPC ID as key for selector with vpcMatch matchID and vmMatch matchName.
	vpcIDOnlyMatch := make(map[string]struct{})
	vmIDOnlyMatch := make(map[string]struct{})
	vmNameOnlyMatch := make(map[string]struct{})
	vmIDWithVpcMatch := make(map[string]struct{})
	vmNameWithVpcMatch := make(map[string]struct{})
	exists := struct{}{}

	for _, selector := range selector.Spec.VMSelector {
		if selector.VpcMatch != nil {
			if selector.VpcMatch.MatchID != "" {
				if len(selector.VMMatch) == 0 {
					if _, found := vpcIDOnlyMatch[selector.VpcMatch.MatchID]; found {
						return fmt.Errorf("%s, %v", errorMsgSameVPCMatchID, selector.VpcMatch.MatchID)
					}
					vpcIDOnlyMatch[selector.VpcMatch.MatchID] = exists
				} else {
					for _, n := range selector.VMMatch {
						if n.MatchID != "" {
							index := n.MatchID + "/" + selector.VpcMatch.MatchID
							if _, found := vmIDWithVpcMatch[index]; found {
								return fmt.Errorf("%s, vpcMatch matchID %v, vmMatch matchID %v",
									errorMsgSameVMAndVPCMatch, selector.VpcMatch.MatchID, n.MatchID)
							}
							vmIDWithVpcMatch[index] = exists

							// VM ID is unique across account. Irrespective of VPC match,
							// same VM ID config in two VMSelector is not allowed.
							if _, found := vmIDOnlyMatch[n.MatchID]; found {
								return fmt.Errorf("%s, %v", errorMsgSameVMMatchID, n.MatchID)
							}
							vmIDOnlyMatch[n.MatchID] = exists
						} else { // Applicable for matchName.
							index := n.MatchName + "/" + selector.VpcMatch.MatchID
							if _, found := vmNameWithVpcMatch[index]; found {
								return fmt.Errorf("%s, vpcMatch matchID %v, vmMatch matchName %v",
									errorMsgSameVMAndVPCMatch, selector.VpcMatch.MatchID, n.MatchName)
							}
							vmNameWithVpcMatch[index] = exists
						}
					}
				}
			}
		} else {
			for _, n := range selector.VMMatch {
				if n.MatchID != "" {
					if _, found := vmIDOnlyMatch[n.MatchID]; found {
						return fmt.Errorf("%s, %v", errorMsgSameVMMatchID, n.MatchID)
					}
					vmIDOnlyMatch[n.MatchID] = exists
				} else {
					if _, found := vmNameOnlyMatch[n.MatchName]; found {
						return fmt.Errorf("%s, %v", errorMsgSameVMMatchName, n.MatchName)
					}
					vmNameOnlyMatch[n.MatchName] = exists
				}
			}
		}
	}
	return nil
}

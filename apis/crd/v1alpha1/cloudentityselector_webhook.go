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

package v1alpha1

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	controllerclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// log is for logging in this package.
var (
	cloudentityselectorlog = logf.Log.WithName("cloudentityselector-resource")
	client                 controllerclient.Client
	sh                     *runtime.Scheme
)

func (r *CloudEntitySelector) SetupWebhookWithManager(mgr ctrl.Manager) error {
	client = mgr.GetClient()
	sh = mgr.GetScheme()
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// nolint:lll
// +kubebuilder:webhook:path=/mutate-crd-cloud-antrea-io-v1alpha1-cloudentityselector,mutating=true,failurePolicy=fail,groups=crd.cloud.antrea.io,resources=cloudentityselectors,verbs=create;update,versions=v1alpha1,name=mcloudentityselector.kb.io,sideEffects=None,admissionReviewVersions=v1;v1beta1

var _ webhook.Defaulter = &CloudEntitySelector{}

// Default implements webhook.Defaulter so a webhook will be registered for the type.
func (r *CloudEntitySelector) Default() {
	cloudentityselectorlog.Info("default", "name", r.Name)

	// make sure selector has owner reference.
	// set owner account only if resource cloudprovideraccount with cloudentiryselector account name in this namespace exists.
	ownerReference := metav1.GetControllerOf(r)
	accountNameSpacedName := &types.NamespacedName{
		Namespace: r.Namespace,
		Name:      r.Spec.AccountName,
	}
	ownerAccount := &CloudProviderAccount{}
	err := client.Get(context.TODO(), *accountNameSpacedName, ownerAccount)
	if err != nil {
		cloudentityselectorlog.Error(err, "failed to find owner account", "account", *accountNameSpacedName)
		return
	}
	cloudProviderType, err := ownerAccount.GetAccountProviderType()
	if err != nil {
		cloudentityselectorlog.Error(err, "Invalid cloud provider type")
		return
	}
	if cloudProviderType == AzureCloudProvider {
		for _, m := range r.Spec.VMSelector {
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
		err = controllerutil.SetControllerReference(ownerAccount, r, sh)
		if err != nil {
			cloudentityselectorlog.Error(err, "failed to set owner account", "cloudentityselector", r, "account", *accountNameSpacedName)
			return
		}
	}
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// nolint:lll
// +kubebuilder:webhook:verbs=create;update,path=/validate-crd-cloud-antrea-io-v1alpha1-cloudentityselector,mutating=false,failurePolicy=fail,groups=crd.cloud.antrea.io,resources=cloudentityselectors,versions=v1alpha1,name=vcloudentityselector.kb.io,sideEffects=None,admissionReviewVersions=v1;v1beta1

var _ webhook.Validator = &CloudEntitySelector{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type.
func (r *CloudEntitySelector) ValidateCreate() error {
	cloudentityselectorlog.Info("validate create", "name", r.Name)

	// make sure owner exists. Default will try to populate if owner with provided account name exists.
	if r.GetObjectMeta().GetOwnerReferences() == nil {
		return fmt.Errorf("owner account %v not found", r.Spec.AccountName)
	}

	// make sure no existing cloudentityselector has same account as this cloudentiryselector as its owner.
	if err := r.validateNoOwnerConflict(); err != nil {
		return err
	}

	// make sure unsupported match combinations are not configured
	if err := r.validateMatchSections(); err != nil {
		return err
	}
	return nil
}

// validateNoOwnerConflict makes sure no two cloudentityselectors have same account owner.
func (r *CloudEntitySelector) validateNoOwnerConflict() error {
	cloudEntitySelectorList := &CloudEntitySelectorList{}
	err := client.List(context.TODO(), cloudEntitySelectorList, controllerclient.InNamespace(r.Namespace))
	if err != nil {
		return err
	}

	namespace := r.GetNamespace()
	accName := r.Spec.AccountName
	for _, item := range cloudEntitySelectorList.Items {
		if strings.Compare(strings.TrimSpace(item.Spec.AccountName), strings.TrimSpace(accName)) == 0 {
			return fmt.Errorf("cloudentityselector %v/%v with owner as account %v/%v already exists."+
				"(account in a namespace can be owner of only one cloudentityselector)", namespace, item.GetName(), namespace, accName)
		}
	}

	return nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type.
func (r *CloudEntitySelector) ValidateUpdate(old runtime.Object) error {
	cloudentityselectorlog.Info("validate update", "name", r.Name)

	// account name update not allowed.
	oldAccName := strings.TrimSpace(old.(*CloudEntitySelector).Spec.AccountName)
	newAccountName := strings.TrimSpace(r.Spec.AccountName)
	if strings.Compare(oldAccName, newAccountName) != 0 {
		return fmt.Errorf("account name update not allowed (old:%v, new:%v)", oldAccName, newAccountName)
	}

	// make sure unsupported match combinations are not configured
	if err := r.validateMatchSections(); err != nil {
		return err
	}

	return nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type.
func (r *CloudEntitySelector) ValidateDelete() error {
	cloudentityselectorlog.Info("validate delete", "name", r.Name)

	// TODO(user): fill in your validation logic upon object deletion.
	return nil
}

func (r *CloudEntitySelector) GetOwnerAccount() (*CloudProviderAccount, error) {
	accountNameSpacedName := &types.NamespacedName{
		Namespace: r.Namespace,
		Name:      r.Spec.AccountName,
	}
	ownerAccount := &CloudProviderAccount{}
	err := client.Get(context.TODO(), *accountNameSpacedName, ownerAccount)
	if err != nil {
		cloudentityselectorlog.Error(err, "failed to find owner account", "account", *accountNameSpacedName)
		return nil, err
	}
	return ownerAccount, err
}

// validateMatchSections checks for unsupported match combinations and errors out
func (r *CloudEntitySelector) validateMatchSections() error {
	// MatchID and MatchName are not supported together in an EntityMatch, applicable for both vpcMatch, vmMatch section
	for _, m := range r.Spec.VMSelector {
		if m.VpcMatch != nil {
			if len(strings.TrimSpace(m.VpcMatch.MatchID)) != 0 &&
				len(strings.TrimSpace(m.VpcMatch.MatchName)) != 0 {
				return fmt.Errorf("matchID and matchName are not supported together, " +
					"configure either matchID or matchName in an EntityMatch")
			}
		}
		for _, vmMatch := range m.VMMatch {
			if len(strings.TrimSpace(vmMatch.MatchID)) != 0 &&
				len(strings.TrimSpace(vmMatch.MatchName)) != 0 {
				return fmt.Errorf("matchID and matchName are not supported together, " +
					"configure either matchID or matchName in an EntityMatch")
			}
		}
	}

	ownerAccount, err := r.GetOwnerAccount()
	if err != nil {
		return err
	}
	cloudProviderType, err := ownerAccount.GetAccountProviderType()
	if err != nil {
		cloudentityselectorlog.Error(err, "Invalid cloud provider type")
		return err
	}

	// In Azure, Vpc Name is not supported in vpcMatch
	// In AWS, Vpc name(in vpcMatch section) with either vm id or vm name(in vmMatch section) is not supported
	if cloudProviderType == AzureCloudProvider {
		for _, m := range r.Spec.VMSelector {
			if m.VpcMatch != nil {
				if len(strings.TrimSpace(m.VpcMatch.MatchName)) != 0 {
					return fmt.Errorf("matchName is not supported in vpcMatch, " +
						"use matchID instead of matchName")
				}
			}
		}
	} else {
		for _, m := range r.Spec.VMSelector {
			if m.VpcMatch != nil {
				if len(strings.TrimSpace(m.VpcMatch.MatchName)) != 0 {
					for _, vmMatch := range m.VMMatch {
						if len(strings.TrimSpace(vmMatch.MatchID)) != 0 ||
							len(strings.TrimSpace(vmMatch.MatchName)) != 0 {
							return fmt.Errorf("vpc matchName with either vm matchID" +
								" or vm matchName is not supported, use vpc matchID instead" +
								" of vpc matchName")
						}
					}
				}
			}
		}
	}
	return nil
}

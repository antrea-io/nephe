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

package crd

import (
	"context"
	"fmt"
	"time"

	crdv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	"github.com/cenkalti/backoff/v4"
	"github.com/go-logr/logr"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DoesCesCrExistsForAccount returns true if there is a CloudEntitySelector CR for a given account.
func DoesCesCrExistsForAccount(k8sClient client.Client, namespacedName *types.NamespacedName) bool {
	cesList := &crdv1alpha1.CloudEntitySelectorList{}
	listOptions := &client.ListOptions{
		Namespace: namespacedName.Namespace,
	}
	if err := k8sClient.List(context.TODO(), cesList, listOptions); err != nil {
		return false
	}

	for _, ces := range cesList.Items {
		if ces.Spec.AccountName == namespacedName.Name {
			return true
		}
	}
	return false
}

// CheckCRDExistence checks if the custom resource definitions are installed on the API server.
func CheckCRDExistence(log logr.Logger) error {
	apiExtClient, err := apiextensionsclientset.NewForConfig(ctrl.GetConfigOrDie())
	if err != nil {
		return fmt.Errorf("failed to create API extensions client: %v", err)
	}
	b := backoff.NewExponentialBackOff()
	// Configure the backoff parameters
	b.InitialInterval = 1 * time.Second
	b.MaxInterval = 30 * time.Second
	b.MaxElapsedTime = 1 * time.Hour // Max retry

	err = backoff.Retry(func() error {
		crds := []string{
			"cloudprovideraccounts.crd.cloud.antrea.io",
			"cloudentityselectors.crd.cloud.antrea.io",
		}
		for _, crd := range crds {
			_, err := apiExtClient.ApiextensionsV1().CustomResourceDefinitions().Get(context.TODO(), crd, v1.GetOptions{})
			if err != nil {
				log.Info("failed to get CRD", "name", crd)
				return fmt.Errorf("failed to get CRD: %v", err)
			}
		}
		return nil
	}, b)
	return err
}

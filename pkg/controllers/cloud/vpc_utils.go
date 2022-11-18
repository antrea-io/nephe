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

package cloud

import (
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	cloudcommon "antrea.io/nephe/pkg/cloud-provider/cloudapi/common"
	"fmt"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
)

type VpcCache struct {
	VpcIndexer cache.Indexer
}

var vpcCache *VpcCache

const (
	VpcIndexerByAccountNameSpacedName = "accountname.namespace"
	VpcIndexerByVpcID                 = "vpc.id"
	VpcIndexerByUUID                  = "vpc.uuid"
	VpcIndexerByNamespace             = "vpc.namespace"
)

func init() {
	vpcCache = &VpcCache{}
}

func DefineIndexers(log logr.Logger) {
	log.Info("Test: Defining indexers")
	vpcCache.VpcIndexer = cache.NewIndexer(
		func(obj interface{}) (string, error) {
			vpc := obj.(*runtimev1alpha1.Vpc)
			return fmt.Sprintf("%v-%v", vpc.Annotations[VpcIndexerByUUID], vpc.Info.Id), nil
		},
		cache.Indexers{
			VpcIndexerByAccountNameSpacedName: func(obj interface{}) ([]string, error) {
				vpc := obj.(*runtimev1alpha1.Vpc)
				return []string{vpc.Annotations[VpcIndexerByAccountNameSpacedName]}, nil

			},
			VpcIndexerByVpcID: func(obj interface{}) ([]string, error) {
				vpc := obj.(*runtimev1alpha1.Vpc)
				return []string{vpc.Info.Id}, nil
			},
			VpcIndexerByNamespace: func(obj interface{}) ([]string, error) {
				vpc := obj.(*runtimev1alpha1.Vpc)
				return []string{vpc.Annotations[VpcIndexerByNamespace]}, nil
			},
		})
}

func BuildVpcInventory(cloudInterface cloudcommon.CloudInterface, log logr.Logger, namespace string,
	namespacedName *types.NamespacedName, uuid string) error {
	//TODO: Add locks

	log.Info("Test: In BuildVpcInventory")
	// Retrieve vpclist for account from internal snapshot
	vpcList, e := cloudInterface.GetVpcInventory(namespacedName)
	if e != nil {
		log.Info("failed to read vpc list from Cache")
	}

	log.Info("Test: Insert to indexer")
	/*
		key := fmt.Sprintf("%v-vpc-01e9d9bca776e862e", uuid)
		testVpc, found, _ := vpcCache.VpcIndexer.GetByKey(key)
		if !found {
			log.Info("test:", "key not found in cache", key)
		} else {
			log.Info("test:", "key", key, "vpc id", testVpc.(*runtimev1alpha1.Vpc).Info.Id, "name", testVpc.(*runtimev1alpha1.Vpc).Info.Name)
		}
	*/
	vpcsInCache, _ := vpcCache.VpcIndexer.ByIndex(VpcIndexerByAccountNameSpacedName, namespacedName.String())
	log.Info("test:", "num of vpcs in cache indexer", len(vpcsInCache))

	// Find if vpcs in cache indexer(internal) are found in vpclist from cloud
	for _, i := range vpcsInCache {
		vpc := i.(*runtimev1alpha1.Vpc)
		log.Info("Test: Data from indexer", "vpc id", vpc.Info.Id, "accountid", vpc.ObjectMeta.Annotations[VpcIndexerByAccountNameSpacedName], "Name", vpc.Info.Name)
		if _, found := vpcList[vpc.Info.Id]; !found {
			log.Info("Test:", "vpc not found, delete from cache indexer and internal map", vpc.Info.Id)
			// Delete from cache indexer
			vpcCache.VpcIndexer.Delete(vpc)
		}
	}

	log.Info("Test: Adding to indexer")
	for _, v := range vpcList {
		annotationsMap := map[string]string{
			VpcIndexerByNamespace:             namespace,
			VpcIndexerByAccountNameSpacedName: namespacedName.String(),
			VpcIndexerByUUID:                  uuid,
		}
		v.ObjectMeta.Annotations = annotationsMap

		log.Info("Test: Data from cloud", "vpc id", v.Info.Id, "accountid",
			v.ObjectMeta.Annotations[VpcIndexerByAccountNameSpacedName], "Name", v.Info.Name)
		err := vpcCache.VpcIndexer.Add(v)
		if err != nil {
			log.Info("Test: adding data to indexer failed")
			return err
		}
	}
	return nil
}

func DeleteVpcInventory(log logr.Logger, namespacedName *types.NamespacedName) {
	log.Info("Test: in DeleteVpcInventory")
	vpcsInCache, _ := vpcCache.VpcIndexer.ByIndex(VpcIndexerByAccountNameSpacedName, namespacedName.String())
	for _, i := range vpcsInCache {
		vpc := i.(*runtimev1alpha1.Vpc)
		vpcCache.VpcIndexer.Delete(vpc)
	}
}

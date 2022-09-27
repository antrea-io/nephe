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

package aws

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"antrea.io/nephe/apis/crd/v1alpha1"
	"antrea.io/nephe/pkg/cloud-provider/securitygroup"
)

var _ = Describe("AWS Cloud Security", func() {
	var (
		testVpcID01 = "vpc-cb82c3b2"
		testVMID01  = "i-02d82ffda0fba57b6"
		testVMID02  = "i-0b194935df0d83eb8"

		testAccountNamespacedName = &types.NamespacedName{Namespace: "namespace01", Name: "account01"}
		testEntitySelectorName    = "testEntitySelector01"
		credentials               = "credentials"

		cloudInterface *awsCloud
		account        *v1alpha1.CloudProviderAccount
		selector       *v1alpha1.CloudEntitySelector
		secret         *corev1.Secret

		mockCtrl           *gomock.Controller
		mockawsCloudHelper *MockawsServicesHelper
		mockawsEC2         *MockawsEC2Wrapper
		mockawsService     *MockawsServiceClientCreateInterface
	)

	BeforeEach(func() {
		var pollIntv uint = 2
		account = &v1alpha1.CloudProviderAccount{
			ObjectMeta: v1.ObjectMeta{
				Name:      testAccountNamespacedName.Name,
				Namespace: testAccountNamespacedName.Namespace,
			},
			Spec: v1alpha1.CloudProviderAccountSpec{
				PollIntervalInSeconds: &pollIntv,
				AWSConfig: &v1alpha1.CloudProviderAccountAWSConfig{
					Region: "us-west-2",
					SecretRef: &v1alpha1.SecretReference{
						Name:      testAccountNamespacedName.Name,
						Namespace: testAccountNamespacedName.Namespace,
						Key:       credentials,
					},
				},
			},
		}
		credential := `{"accessKeyId": "keyId","accessKeySecret": "keySecret","roleArn" : "roleArn","externalID" : "" }`

		secret = &corev1.Secret{
			ObjectMeta: v1.ObjectMeta{
				Name:      testAccountNamespacedName.Name,
				Namespace: testAccountNamespacedName.Namespace,
			},
			Data: map[string][]byte{
				"credentials": []byte(credential),
			},
		}
		selector = &v1alpha1.CloudEntitySelector{
			ObjectMeta: v1.ObjectMeta{
				Name:      testEntitySelectorName,
				Namespace: testAccountNamespacedName.Namespace,
			},
			Spec: v1alpha1.CloudEntitySelectorSpec{
				AccountName: testAccountNamespacedName.Name,
				VMSelector: []v1alpha1.VirtualMachineSelector{
					{
						VpcMatch: &v1alpha1.EntityMatch{
							MatchID: testVpcID01,
						},
					},
				},
			},
		}

		mockCtrl = gomock.NewController(GinkgoT())
		mockawsCloudHelper = NewMockawsServicesHelper(mockCtrl)

		mockawsService = NewMockawsServiceClientCreateInterface(mockCtrl)
		mockawsEC2 = NewMockawsEC2Wrapper(mockCtrl)

		mockawsCloudHelper.EXPECT().newServiceSdkConfigProvider(gomock.Any()).Return(mockawsService, nil).Times(1)
		mockawsService.EXPECT().compute().Return(mockawsEC2, nil).AnyTimes()

		instanceIds := []string{testVMID01, testVMID02}
		mockawsEC2.EXPECT().pagedDescribeInstancesWrapper(gomock.Any()).Return(getEc2InstanceObject(instanceIds), nil).AnyTimes()
		mockawsEC2.EXPECT().pagedDescribeNetworkInterfaces(gomock.Any()).Return([]*ec2.NetworkInterface{}, nil).AnyTimes()
		mockawsEC2.EXPECT().describeVpcsWrapper(gomock.Any()).Return(&ec2.DescribeVpcsOutput{}, nil).AnyTimes()
		mockawsEC2.EXPECT().describeVpcPeeringConnectionsWrapper(gomock.Any()).Return(&ec2.DescribeVpcPeeringConnectionsOutput{}, nil).AnyTimes()

		fakeClient := fake.NewClientBuilder().Build()
		_ = fakeClient.Create(context.Background(), secret)
		cloudInterface = newAWSCloud(mockawsCloudHelper)
		err := cloudInterface.AddProviderAccount(fakeClient, account)
		Expect(err).Should(BeNil())

		err = cloudInterface.AddAccountResourceSelector(testAccountNamespacedName, selector)
		Expect(err).Should(BeNil())

		// wait for instances to be populated
		time.Sleep(time.Duration(pollIntv+1) * time.Second)
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	Context("CreateSecurityGroup", func() {
		It("Should create security group successfully and return ID", func() {
			webAddressGroupIdentifier := &securitygroup.CloudResourceID{
				Name: "Web",
				Vpc:  testVpcID01,
			}

			input1 := constructDescribeSecurityGroupInput(webAddressGroupIdentifier.Vpc,
				map[string]struct{}{webAddressGroupIdentifier.GetCloudName(true): {}})
			mockawsEC2.EXPECT().describeSecurityGroups(gomock.Eq(input1)).Return(constructDescribeSecurityGroupOutput(
				nil, true, false), nil).Times(1)

			mockawsEC2.EXPECT().describeSecurityGroups(gomock.Eq(input1)).Return(constructDescribeSecurityGroupOutput(
				webAddressGroupIdentifier, true, false), nil).Times(1)

			mockawsEC2.EXPECT().describeSecurityGroups(gomock.Any()).Return(
				constructDescribeSecurityGroupOutput(webAddressGroupIdentifier, true, false), nil).Times(1)
			createOutput := &ec2.CreateSecurityGroupOutput{GroupId: aws.String(fmt.Sprintf("%v", rand.Intn(10)))}
			mockawsEC2.EXPECT().createSecurityGroup(gomock.Any()).Return(createOutput, nil).Times(1)
			mockawsEC2.EXPECT().revokeSecurityGroupEgress(gomock.Any()).Return(nil, nil).Times(1)

			cloudSgID, err := cloudInterface.CreateSecurityGroup(webAddressGroupIdentifier, true)
			Expect(err).Should(BeNil())
			Expect(cloudSgID).Should(Not(BeNil()))
		})
		It("Should return pre-created security group successfully and return ID", func() {
			webAddressGroupIdentifier := &securitygroup.CloudResourceID{
				Name: "Web",
				Vpc:  testVpcID01,
			}
			mockawsEC2.EXPECT().describeSecurityGroups(gomock.Any()).Return(
				constructDescribeSecurityGroupOutput(webAddressGroupIdentifier, true, false), nil).Times(1)
			mockawsEC2.EXPECT().createSecurityGroup(gomock.Any()).Times(0)

			cloudSgID, err := cloudInterface.CreateSecurityGroup(webAddressGroupIdentifier, true)
			Expect(err).Should(BeNil())
			Expect(cloudSgID).Should(Not(BeNil()))
		})
	})
	Context("DeleteSecurityGroup", func() {
		It("Should delete security groups successfully (SG does not exist in cloud)", func() {
			webAddressGroupIdentifier := &securitygroup.CloudResourceID{
				Name: "Web",
				Vpc:  testVpcID01,
			}
			input := constructDescribeSecurityGroupInput(webAddressGroupIdentifier.Vpc,
				map[string]struct{}{webAddressGroupIdentifier.GetCloudName(true): {}})
			mockawsEC2.EXPECT().describeSecurityGroups(gomock.Eq(input)).Return(constructDescribeSecurityGroupOutput(
				nil, true, false), nil).Times(1)
			mockawsEC2.EXPECT().deleteSecurityGroup(gomock.Any()).Times(0)
			err := cloudInterface.DeleteSecurityGroup(webAddressGroupIdentifier, true)
			Expect(err).Should(BeNil())
		})
		It("Should delete security groups successfully (SG exist in cloud)", func() {
			webAddressGroupIdentifier := &securitygroup.CloudResourceID{
				Name: "Web",
				Vpc:  testVpcID01,
			}
			input1 := constructDescribeSecurityGroupInput(webAddressGroupIdentifier.Vpc,
				map[string]struct{}{webAddressGroupIdentifier.GetCloudName(true): {}})
			mockawsEC2.EXPECT().describeSecurityGroups(gomock.Eq(input1)).Return(constructDescribeSecurityGroupOutput(
				webAddressGroupIdentifier, true, false), nil).Times(1)

			input2 := constructDescribeSecurityGroupInput(webAddressGroupIdentifier.Vpc,
				map[string]struct{}{awsVpcDefaultSecurityGroupName: {}})
			output := &ec2.DescribeSecurityGroupsOutput{
				NextToken: nil,
				SecurityGroups: []*ec2.SecurityGroup{{
					GroupId:   aws.String(fmt.Sprintf("%v", rand.Intn(10))),
					GroupName: aws.String(awsVpcDefaultSecurityGroupName),
				}},
			}
			mockawsEC2.EXPECT().describeSecurityGroups(gomock.Eq(input2)).Return(output, nil).Times(1)

			mockawsEC2.EXPECT().pagedDescribeNetworkInterfaces(gomock.Any()).Return([]*ec2.NetworkInterface{}, nil).AnyTimes()
			mockawsEC2.EXPECT().deleteSecurityGroup(gomock.Any()).Return(&ec2.DeleteSecurityGroupOutput{}, nil).Times(1)
			err := cloudInterface.DeleteSecurityGroup(webAddressGroupIdentifier, true)
			Expect(err).Should(BeNil())
		})
	})
	Context("UpdateSecurityGroupRules", func() {
		It("Should create ingress rules successfully", func() {
			webSgIdentifier := &securitygroup.CloudResourceID{
				Name: "Web",
				Vpc:  testVpcID01,
			}
			ingress := []*securitygroup.IngressRule{{
				FromPort:           aws.Int(22),
				FromSrcIP:          []*net.IPNet{},
				FromSecurityGroups: []*securitygroup.CloudResourceID{webSgIdentifier},
				Protocol:           aws.Int(6),
			}}
			output := constructDescribeSecurityGroupOutput(webSgIdentifier, true, false)
			outputAt := constructDescribeSecurityGroupOutput(webSgIdentifier, false, false)
			output.SecurityGroups = append(output.SecurityGroups, outputAt.SecurityGroups...)
			input := constructDescribeSecurityGroupInput(webSgIdentifier.Vpc,
				map[string]struct{}{webSgIdentifier.GetCloudName(true): {}, webSgIdentifier.GetCloudName(false): {}})

			mockawsEC2.EXPECT().describeSecurityGroups(gomock.Any()).Return(output, nil).Times(1).
				Do(func(req *ec2.DescribeSecurityGroupsInput) {
					sortSliceStringPointer(req.Filters[1].Values)
					sortSliceStringPointer(input.Filters[1].Values)
					Expect(req).To(Equal(input))
				})
			mockawsEC2.EXPECT().revokeSecurityGroupIngress(gomock.Any()).Times(0)
			mockawsEC2.EXPECT().authorizeSecurityGroupIngress(gomock.Any()).Times(1).
				Do(func(req *ec2.AuthorizeSecurityGroupIngressInput) {
					Expect(len(req.IpPermissions)).To(Equal(1))
				})
			mockawsEC2.EXPECT().revokeSecurityGroupEgress(gomock.Any()).Times(0)
			mockawsEC2.EXPECT().authorizeSecurityGroupEgress(gomock.Any()).Times(0)

			err := cloudInterface.UpdateSecurityGroupRules(webSgIdentifier, ingress, []*securitygroup.EgressRule{})
			Expect(err).Should(BeNil())
		})

		It("Should create egress rules successfully", func() {
			webSgIdentifier := &securitygroup.CloudResourceID{
				Name: "Web",
				Vpc:  testVpcID01,
			}
			egress := []*securitygroup.EgressRule{{
				ToPort:           aws.Int(22),
				ToDstIP:          []*net.IPNet{},
				ToSecurityGroups: []*securitygroup.CloudResourceID{webSgIdentifier},
				Protocol:         aws.Int(6),
			}}
			output := constructDescribeSecurityGroupOutput(webSgIdentifier, true, false)
			outputAt := constructDescribeSecurityGroupOutput(webSgIdentifier, false, false)
			output.SecurityGroups = append(output.SecurityGroups, outputAt.SecurityGroups...)
			input := constructDescribeSecurityGroupInput(webSgIdentifier.Vpc,
				map[string]struct{}{webSgIdentifier.GetCloudName(true): {}, webSgIdentifier.GetCloudName(false): {}})

			mockawsEC2.EXPECT().describeSecurityGroups(gomock.Any()).Return(output, nil).Times(1).
				Do(func(req *ec2.DescribeSecurityGroupsInput) {
					sortSliceStringPointer(req.Filters[1].Values)
					sortSliceStringPointer(input.Filters[1].Values)
					Expect(req).To(Equal(input))
				})
			mockawsEC2.EXPECT().revokeSecurityGroupIngress(gomock.Any()).Times(0)
			mockawsEC2.EXPECT().authorizeSecurityGroupIngress(gomock.Any()).Times(0)
			mockawsEC2.EXPECT().revokeSecurityGroupEgress(gomock.Any()).Times(0)
			mockawsEC2.EXPECT().authorizeSecurityGroupEgress(gomock.Any()).Times(1).
				Do(func(req *ec2.AuthorizeSecurityGroupEgressInput) {
					Expect(len(req.IpPermissions)).To(Equal(1))
				})

			err := cloudInterface.UpdateSecurityGroupRules(webSgIdentifier, []*securitygroup.IngressRule{}, egress)
			Expect(err).Should(BeNil())
		})

		It("Should delta update ingress rules successfully", func() {
			webSgIdentifier := &securitygroup.CloudResourceID{
				Name: "Web",
				Vpc:  testVpcID01,
			}
			ingress := []*securitygroup.IngressRule{{
				FromPort:           aws.Int(22),
				FromSrcIP:          []*net.IPNet{},
				FromSecurityGroups: []*securitygroup.CloudResourceID{webSgIdentifier},
				Protocol:           aws.Int(6),
			}, {
				FromPort:           aws.Int(80),
				FromSrcIP:          []*net.IPNet{},
				FromSecurityGroups: []*securitygroup.CloudResourceID{webSgIdentifier},
				Protocol:           aws.Int(6),
			}}
			output := constructDescribeSecurityGroupOutput(webSgIdentifier, true, false)
			outputAt := constructDescribeSecurityGroupOutput(webSgIdentifier, false, false)
			outputAt.SecurityGroups[0].IpPermissions = []*ec2.IpPermission{{
				FromPort:         aws.Int64(80),
				ToPort:           aws.Int64(80),
				IpProtocol:       aws.String("tcp"),
				UserIdGroupPairs: []*ec2.UserIdGroupPair{{GroupId: output.SecurityGroups[0].GroupId}},
			}, {
				FromPort:         aws.Int64(8080),
				ToPort:           aws.Int64(8080),
				IpProtocol:       aws.String("tcp"),
				UserIdGroupPairs: []*ec2.UserIdGroupPair{{GroupId: output.SecurityGroups[0].GroupId}},
			}}
			output.SecurityGroups = append(output.SecurityGroups, outputAt.SecurityGroups...)
			input := constructDescribeSecurityGroupInput(webSgIdentifier.Vpc,
				map[string]struct{}{webSgIdentifier.GetCloudName(true): {}, webSgIdentifier.GetCloudName(false): {}})

			mockawsEC2.EXPECT().describeSecurityGroups(gomock.Any()).Return(output, nil).Times(1).
				Do(func(req *ec2.DescribeSecurityGroupsInput) {
					sortSliceStringPointer(req.Filters[1].Values)
					sortSliceStringPointer(input.Filters[1].Values)
					Expect(req).To(Equal(input))
				})
			mockawsEC2.EXPECT().revokeSecurityGroupIngress(gomock.Any()).Times(1).
				Do(func(req *ec2.RevokeSecurityGroupIngressInput) {
					Expect(len(req.IpPermissions)).To(Equal(1))
				})
			mockawsEC2.EXPECT().authorizeSecurityGroupIngress(gomock.Any()).Times(1).
				Do(func(req *ec2.AuthorizeSecurityGroupIngressInput) {
					Expect(len(req.IpPermissions)).To(Equal(1))
				})
			mockawsEC2.EXPECT().revokeSecurityGroupEgress(gomock.Any()).Times(0)
			mockawsEC2.EXPECT().authorizeSecurityGroupEgress(gomock.Any()).Times(0)

			err := cloudInterface.UpdateSecurityGroupRules(webSgIdentifier, ingress, []*securitygroup.EgressRule{})
			Expect(err).Should(BeNil())
		})

		It("Should delta update egress rules successfully", func() {
			webSgIdentifier := &securitygroup.CloudResourceID{
				Name: "Web",
				Vpc:  testVpcID01,
			}
			egress := []*securitygroup.EgressRule{{
				ToPort:           aws.Int(22),
				ToDstIP:          []*net.IPNet{},
				ToSecurityGroups: []*securitygroup.CloudResourceID{webSgIdentifier},
				Protocol:         aws.Int(6),
			}, {
				ToPort:           aws.Int(80),
				ToDstIP:          []*net.IPNet{},
				ToSecurityGroups: []*securitygroup.CloudResourceID{webSgIdentifier},
				Protocol:         aws.Int(6),
			}}
			output := constructDescribeSecurityGroupOutput(webSgIdentifier, true, false)
			outputAt := constructDescribeSecurityGroupOutput(webSgIdentifier, false, false)
			outputAt.SecurityGroups[0].IpPermissionsEgress = []*ec2.IpPermission{{
				FromPort:         aws.Int64(80),
				ToPort:           aws.Int64(80),
				IpProtocol:       aws.String("tcp"),
				UserIdGroupPairs: []*ec2.UserIdGroupPair{{GroupId: output.SecurityGroups[0].GroupId}},
			}, {
				FromPort:         aws.Int64(8080),
				ToPort:           aws.Int64(8080),
				IpProtocol:       aws.String("tcp"),
				UserIdGroupPairs: []*ec2.UserIdGroupPair{{GroupId: output.SecurityGroups[0].GroupId}},
			}}
			output.SecurityGroups = append(output.SecurityGroups, outputAt.SecurityGroups...)
			input := constructDescribeSecurityGroupInput(webSgIdentifier.Vpc,
				map[string]struct{}{webSgIdentifier.GetCloudName(true): {}, webSgIdentifier.GetCloudName(false): {}})

			mockawsEC2.EXPECT().describeSecurityGroups(gomock.Any()).Return(output, nil).Times(1).
				Do(func(req *ec2.DescribeSecurityGroupsInput) {
					sortSliceStringPointer(req.Filters[1].Values)
					sortSliceStringPointer(input.Filters[1].Values)
					Expect(req).To(Equal(input))
				})
			mockawsEC2.EXPECT().revokeSecurityGroupIngress(gomock.Any()).Times(0)
			mockawsEC2.EXPECT().authorizeSecurityGroupIngress(gomock.Any()).Times(0)
			mockawsEC2.EXPECT().revokeSecurityGroupEgress(gomock.Any()).Times(1).
				Do(func(req *ec2.RevokeSecurityGroupEgressInput) {
					Expect(len(req.IpPermissions)).To(Equal(1))
				})
			mockawsEC2.EXPECT().authorizeSecurityGroupEgress(gomock.Any()).Times(1).
				Do(func(req *ec2.AuthorizeSecurityGroupEgressInput) {
					Expect(len(req.IpPermissions)).To(Equal(1))
				})

			err := cloudInterface.UpdateSecurityGroupRules(webSgIdentifier, []*securitygroup.IngressRule{}, egress)
			Expect(err).Should(BeNil())
		})
	})
})

func constructDescribeSecurityGroupInput(vpcID string, sgNamesSet map[string]struct{}) *ec2.DescribeSecurityGroupsInput {
	vpcIDs := []string{vpcID}
	filters := buildAwsEc2FilterForSecurityGroupNameMatches(vpcIDs, sgNamesSet)
	input := &ec2.DescribeSecurityGroupsInput{
		Filters: filters,
	}
	return input
}

// nolint: unparam
func constructDescribeSecurityGroupOutput(identifier *securitygroup.CloudResourceID, membershipOnly bool,
	isDefaultSg bool) *ec2.DescribeSecurityGroupsOutput {
	var securityGroups []*ec2.SecurityGroup
	if isDefaultSg {
		securityGroup := &ec2.SecurityGroup{
			GroupId:   aws.String(fmt.Sprintf("%v", rand.Intn(10))),
			GroupName: aws.String(awsVpcDefaultSecurityGroupName),
		}
		securityGroups = append(securityGroups, securityGroup)
	} else {
		if identifier != nil {
			securityGroup := &ec2.SecurityGroup{
				GroupId:   aws.String(fmt.Sprintf("%v", rand.Intn(10))),
				GroupName: aws.String(identifier.GetCloudName(membershipOnly)),
			}
			securityGroups = append(securityGroups, securityGroup)
		}
	}

	output := &ec2.DescribeSecurityGroupsOutput{
		NextToken:      nil,
		SecurityGroups: securityGroups,
	}
	return output
}

func sortSliceStringPointer(s []*string) {
	sort.Slice(s, func(i, j int) bool { return *s[i] < *s[j] })
}

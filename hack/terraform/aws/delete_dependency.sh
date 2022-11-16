#! /bin/bash
# Copyright 2022 Antrea Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Delete security groups and the dependencies on AWS created by users.
vpc_id="$( terraform output vpc_id | sed 's/\"//g' )"
region="$( terraform output region | sed 's/\"//g' )"

terrform_security_group_ids="$( aws ec2 describe-security-groups --region $region \
--filters Name=vpc-id,Values=$vpc_id Name=tag:Terraform,Values=true | jq ".SecurityGroups[].GroupId" )"
default_security_group_id="$( terraform output vpc_security_group_ids | grep sg- | sed 's/\"//g;s/,//g' )"
security_group_ids="$( aws ec2 describe-security-groups --region $region --filters Name=vpc-id,Values=$vpc_id | jq ".SecurityGroups[].GroupId" )"

function delete_security_group(){
    network_interface_ids="$( aws ec2 describe-instances --region $region --filters Name=vpc-id,Values=$vpc_id Name=instance.group-id,Values=$1  | jq ".[][].Instances[].NetworkInterfaces[].NetworkInterfaceId" )"

    for network_interface_id in $network_interface_ids; do
        attachment_id="$( echo $network_interface_id | sed 's/\"//g' )" # make the attachment's security group as 'default'
        aws ec2 modify-network-interface-attribute --network-interface-id $attachment_id --groups $default_security_group_id 
        if [ $? -ne 0 ]; then
            echo Failed to detach attachment $attachment_id
            exit 1
        else 
            echo Successfully detached security group from $attachment_id
        fi
    done

    aws ec2 delete-security-group --group-id $1
    if [ $? -ne 0 ]; then
        echo Failed to delete security group $1
        exit 1
    else 
        echo Successfully deleted security group $1
    fi
}

for security_group_id in $security_group_ids; do
    s="$( echo ${terrform_security_group_ids} | grep ${security_group_id} )"
    if [[ -z "$s" ]]; then
        delete_security_group "$( echo ${security_group_id} | sed 's/\"//g' )"
    fi
done



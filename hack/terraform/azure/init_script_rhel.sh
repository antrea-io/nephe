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

# Constants.
K8S_KUBECONFIG="antrea-agent.kubeconfig"
ANTREA_KUBECONFIG="antrea-agent.antrea.kubeconfig"

if [[ ${WITH_AGENT} == true ]]; then
  cat <<EOF > $K8S_KUBECONFIG
${K8S_CONF}
EOF
  cat <<EOF > $ANTREA_KUBECONFIG
${ANTREA_CONF}
EOF
  # redirecting wrapper script into a file like above kubeconfigs doesn't work, it executes the script anyways.
  # TODO: redirect script to a file then execute the file instead of running it inline here.
  set -- --ns "${NAMESPACE}" --antrea-version "${ANTREA_VERSION}" --kubeconfig $K8S_KUBECONFIG --antrea-kubeconfig $ANTREA_KUBECONFIG
  export SYSTEMD_PAGER=""
  ${INSTALL_VM_AGENT_WRAPPER}
fi

sudo -s
dnf install httpd -y
echo "Listen 8080" >> /etc/httpd/conf/httpd.conf
service httpd restart
echo "<h1>Deployed via Terraform</h1>" |  tee /var/www/html/index.html
setenforce 0
firewall-cmd --add-service=http
firewall-cmd --add-port=8080/tcp
setenforce 1

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

sudo apt-get update

if [[ ${WITH_AGENT} == true ]]; then
  sudo apt-get install -y openvswitch-switch
  cat <<EOF > antrea-agent.kubeconfig
${K8S_CONF}
EOF
  cat <<EOF > antrea-agent.antrea.kubeconfig
${ANTREA_CONF}
EOF
  echo $HOME
  set -- --ns "vm-ns" --antrea-version v1.8.0 --kubeconfig antrea-agent.kubeconfig --antrea-kubeconfig antrea-agent.antrea.kubeconfig --bin /home/azureuser/antrea-agent
  export SYSTEMD_PAGER=""
  ${INSTALL_WRAPPER}
fi

sudo apt-get install -y apache2
sudo echo "Listen 8080" >> /etc/apache2/ports.conf
sudo systemctl restart apache2
sudo systemctl enable apache2
echo "<h1>Deployed via Terraform</h1>" | sudo tee /var/www/html/index.html

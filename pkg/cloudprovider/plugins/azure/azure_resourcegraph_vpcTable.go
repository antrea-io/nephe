package azure

import (
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork"
)

const (
	vpcTableQueryTemplate = "Resources" +
		"| where type =~ 'microsoft.network/virtualnetworks'" +
		"| extend subscriptionIdLowerCase = tolower(subscriptionId)" +
		"{{ if .SubscriptionIDs }} " +
		"| where subscriptionIdLowerCase in ({{ .SubscriptionIDs }}) " +
		"{{ end }}" +
		"| extend tenantIdLowerCase = tolower(tenantId)" +
		"{{ if .TenantIDs }} " +
		"| where tenantIdLowerCase in ({{ .TenantIDs }}) " +
		"{{ end }}" +
		"| extend locationLowerCase = tolower(location)" +
		"{{ if .Locations }} " +
		"| where locationLowerCase in ({{ .Locations }})" +
		"{{ end }}" +
		"| extend virtualNetworkID = properties.virtualMachine.id" +
		"| extend networkSecurityGroupID = properties.networkSecurityGroup.id" +
		"| mvexpand ipconfig = properties.ipConfigurations" +
		"| extend applicationSecurityGroup = ipconfig.properties.applicationSecurityGroups" +
		"| extend primary = ipconfig.properties.primary" +
		"| where primary =~ 'true'" +
		"| extend vnetIdArray = array_slice(split(ipconfig.properties.subnet.id, \"/\"), 0, 8)" +
		"| extend vnetId = tolower(strcat_array(vnetIdArray, \"/\"))" +
		"{{ if .VnetIDs }} " +
		"| where vnetId in ({{ .VnetIDs }}) " +
		"{{ end }}" +
		"| mvexpand applicationSecurityGroup" +
		"| extend appId = applicationSecurityGroup.id" +
		"| summarize applicationSecurityGroupIDs=make_list(appId), tags=make_set(tags) by id, name, " +
		"tostring(virtualMachineID), tostring(networkSecurityGroupID), tostring(vnetId)"
)

type virtualNetworkTable struct {
	AddressSpace           *armnetwork.AddressSpace
	ID                     *string
	Name                   *string
	Resourceguid           *string
	Properties             *armnetwork.VirtualNetworkPropertiesFormat
	Tags                   map[string]*string
	VirtualNetworkPeerings []*armnetwork.VirtualNetworkPeering
}

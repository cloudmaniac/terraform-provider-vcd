package vcd

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/vmware/go-vcloud-director/v2/govcd"
	"github.com/vmware/go-vcloud-director/v2/types/v56"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func resourceVcdNetworkIsolatedV2() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceVcdNetworkIsolatedV2Create,
		ReadContext:   resourceVcdNetworkIsolatedV2Read,
		UpdateContext: resourceVcdNetworkIsolatedV2Update,
		DeleteContext: resourceVcdNetworkIsolatedV2Delete,
		Importer: &schema.ResourceImporter{
			StateContext: resourceVcdNetworkIsolatedV2Import,
		},

		Schema: map[string]*schema.Schema{
			"org": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
				Description: "The name of organization to use, optional if defined at provider " +
					"level. Useful when connected as sysadmin working across different organizations",
			},
			"vdc": {
				Type:          schema.TypeString,
				Optional:      true,
				Computed:      true,
				Description:   "The name of VDC to use, optional if defined at provider level",
				ConflictsWith: []string{"owner_id"},
				Deprecated:    "This field is deprecated in favor of 'owner_id' which supports both - VDC and VDC Group IDs",
			},
			"owner_id": {
				Type:          schema.TypeString,
				Optional:      true,
				Computed:      true,
				Description:   "ID of VDC or VDC Group",
				ConflictsWith: []string{"vdc"},
			},
			"name": {
				Type:        schema.TypeString,
				Required:    true,
				Description: "Network name",
			},
			"description": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Network description",
			},
			"is_shared": {
				Type:        schema.TypeBool,
				Optional:    true,
				Computed:    true,
				Description: "NSX-V only - share this network with other VDCs in this organization. Default - false",
			},
			"gateway": {
				Type:        schema.TypeString,
				Required:    true,
				Description: "Gateway IP address",
			},
			"prefix_length": {
				Type:        schema.TypeInt,
				Required:    true,
				Description: "Network prefix",
			},
			"dns1": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "DNS server 1",
			},
			"dns2": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "DNS server 1",
			},
			"dns_suffix": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "DNS suffix",
			},
			"static_ip_pool": {
				Type:        schema.TypeSet,
				Optional:    true,
				Description: "IP ranges used for static pool allocation in the network",
				Elem:        networkV2IpRange,
			},
			"metadata": {
				Type:        schema.TypeMap,
				Optional:    true,
				Description: "Key value map of metadata to assign to this network. Key and value can be any string",
			},
		},
	}
}

func resourceVcdNetworkIsolatedV2Create(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	vcdClient := meta.(*VCDClient)

	// Only when a network is in VDC Group - it must lock parent VDC Group. It doesn't cause lock
	// issues when created in VDC.
	vcdClient.lockIfOwnerIsVdcGroup(d)
	defer vcdClient.unLockIfOwnerIsVdcGroup(d)

	org, err := vcdClient.GetOrgFromResource(d)
	if err != nil {
		return diag.Errorf("[isolated network create v2] error retrieving Org: %s", err)
	}

	networkType, err := getOpenApiOrgVdcIsolatedNetworkType(d, vcdClient)
	if err != nil {
		return diag.FromErr(err)
	}

	orgNetwork, err := org.CreateOpenApiOrgVdcNetwork(networkType)
	if err != nil {
		return diag.Errorf("[isolated network v2 create] error creating Isolated network: %s", err)
	}

	d.SetId(orgNetwork.OpenApiOrgVdcNetwork.ID)

	err = createOrUpdateOpenApiNetworkMetadata(d, orgNetwork)
	if err != nil {
		return diag.Errorf("[isolated network v2 create] error adding metadata to Isolated network: %s", err)
	}

	return resourceVcdNetworkIsolatedV2Read(ctx, d, meta)
}

func resourceVcdNetworkIsolatedV2Update(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	vcdClient := meta.(*VCDClient)

	// `vdc` field is deprecated. `vdc` value should not be changed unless it is removal of the
	// field at all to allow easy migration to `owner_id` path
	if _, newValue := d.GetChange("vdc"); d.HasChange("vdc") && newValue.(string) != "" {
		return diag.Errorf("changing 'vdc' field value is not supported. It can only be removed. " +
			"Please use `owner_id` field for moving network to/from VDC Group")
	}

	// Only when a network is in VDC Group - it must lock parent VDC Group. It doesn't cause lock
	// issues when created in VDC.
	vcdClient.lockIfOwnerIsVdcGroup(d)
	defer vcdClient.unLockIfOwnerIsVdcGroup(d)

	org, err := vcdClient.GetOrgFromResource(d)
	if err != nil {
		return diag.Errorf("[isolated network create v2] error retrieving Org: %s", err)
	}

	orgNetwork, err := org.GetOpenApiOrgVdcNetworkById(d.Id())
	// If object is not found -
	if govcd.ContainsNotFound(err) {
		d.SetId("")
		return nil
	}
	if err != nil {
		return diag.Errorf("[isolated network v2 update] error getting Isolated network: %s", err)
	}

	networkType, err := getOpenApiOrgVdcIsolatedNetworkType(d, vcdClient)
	if err != nil {
		return diag.FromErr(err)
	}

	// Explicitly add ID to the new type because function `getOpenApiOrgVdcIsolatedNetworkType` only sets other fields
	networkType.ID = d.Id()

	_, err = orgNetwork.Update(networkType)
	if err != nil {
		return diag.Errorf("[isolated network v2 update] error updating Isolated network: %s", err)
	}

	err = createOrUpdateOpenApiNetworkMetadata(d, orgNetwork)
	if err != nil {
		return diag.Errorf("[isolated network v2 update] error updating Isolated network metadata: %s", err)
	}

	return resourceVcdNetworkIsolatedV2Read(ctx, d, meta)
}

func resourceVcdNetworkIsolatedV2Read(_ context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	vcdClient := meta.(*VCDClient)

	org, err := vcdClient.GetOrgFromResource(d)
	if err != nil {
		return diag.Errorf("[isolated network v2 read] error retrieving VDC: %s", err)
	}

	orgNetwork, err := org.GetOpenApiOrgVdcNetworkById(d.Id())
	// If object is not found - unset ID
	if govcd.ContainsNotFound(err) {
		d.SetId("")
		return nil
	}
	if err != nil {
		return diag.Errorf("[isolated network v2 read] error getting Isolated network: %s", err)
	}

	err = setOpenApiOrgVdcIsolatedNetworkData(d, orgNetwork.OpenApiOrgVdcNetwork)
	if err != nil {
		return diag.Errorf("[isolated network v2 read] error setting Isolated network data: %s", err)
	}

	d.SetId(orgNetwork.OpenApiOrgVdcNetwork.ID)

	// Metadata is not supported when the network is in a VDC Group
	if !govcd.OwnerIsVdcGroup(orgNetwork.OpenApiOrgVdcNetwork.OwnerRef.ID) {
		metadata, err := orgNetwork.GetMetadata()
		if err != nil {
			log.Printf("[DEBUG] Unable to find isolated network v2 metadata: %s", err)
			return diag.Errorf("[isolated network v2 read] unable to find Isolated network metadata %s", err)
		}
		err = d.Set("metadata", getMetadataStruct(metadata.MetadataEntry))
		if err != nil {
			return diag.Errorf("[isolated network v2 read] unable to set Isolated network metadata %s", err)
		}
	}

	return nil
}

func resourceVcdNetworkIsolatedV2Delete(_ context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	vcdClient := meta.(*VCDClient)

	// Only when a network is in VDC Group - it must lock parent VDC Group. It doesn't cause lock
	// issues when created in VDC.
	vcdClient.lockIfOwnerIsVdcGroup(d)
	defer vcdClient.unLockIfOwnerIsVdcGroup(d)

	org, err := vcdClient.GetOrgFromResource(d)
	if err != nil {
		return diag.Errorf("[isolated network create v2] error retrieving Org: %s", err)
	}

	orgNetwork, err := org.GetOpenApiOrgVdcNetworkById(d.Id())
	if err != nil {
		return diag.Errorf("[isolated network v2 delete] error getting Isolated network: %s", err)
	}

	err = orgNetwork.Delete()
	if err != nil {
		return diag.Errorf("[isolated network v2 delete] error deleting Isolated network: %s", err)
	}

	return nil
}

func resourceVcdNetworkIsolatedV2Import(_ context.Context, d *schema.ResourceData, meta interface{}) ([]*schema.ResourceData, error) {
	resourceURI := strings.Split(d.Id(), ImportSeparator)
	if len(resourceURI) != 3 {
		return nil, fmt.Errorf("[isolated network v2 import] resource name must be specified as org-name.vdc-name.network-name")
	}
	orgName, vdcName, networkName := resourceURI[0], resourceURI[1], resourceURI[2]
	vcdClient := meta.(*VCDClient)

	// define an interface type to match VDC and VDC Groups
	var vdcOrVdcGroup vdcOrVdcGroupHandler
	_, vdcOrVdcGroup, err := vcdClient.GetOrgAndVdc(orgName, vdcName)
	if govcd.ContainsNotFound(err) {
		adminOrg, err := vcdClient.GetAdminOrg(orgName)
		if err != nil {
			return nil, fmt.Errorf("error retrieving Admin Org for '%s': %s", orgName, err)
		}

		vdcOrVdcGroup, err = adminOrg.GetVdcGroupByName(vdcName)
		if err != nil {
			return nil, fmt.Errorf("error finding VDC or VDC Group by name '%s': %s", vdcName, err)
		}
	}

	orgNetwork, err := vdcOrVdcGroup.GetOpenApiOrgVdcNetworkByName(networkName)
	if err != nil {
		return nil, fmt.Errorf("error retrieving Isolated network '%s': %s", networkName, err)
	}

	if !orgNetwork.IsIsolated() {
		return nil, fmt.Errorf("[isolated network v2 import] Org network with name '%s' found, but is not of type Isolated (type is '%s')",
			networkName, orgNetwork.GetType())
	}

	dSet(d, "org", orgName)
	dSet(d, "vdc", vdcName)
	d.SetId(orgNetwork.OpenApiOrgVdcNetwork.ID)

	return []*schema.ResourceData{d}, nil
}

func setOpenApiOrgVdcIsolatedNetworkData(d *schema.ResourceData, orgVdcNetwork *types.OpenApiOrgVdcNetwork) error {
	dSet(d, "name", orgVdcNetwork.Name)
	dSet(d, "description", orgVdcNetwork.Description)

	dSet(d, "owner_id", orgVdcNetwork.OwnerRef.ID)
	dSet(d, "vdc", orgVdcNetwork.OwnerRef.Name)

	// Only one subnet can be defined although the structure accepts slice
	dSet(d, "gateway", orgVdcNetwork.Subnets.Values[0].Gateway)
	dSet(d, "prefix_length", orgVdcNetwork.Subnets.Values[0].PrefixLength)
	dSet(d, "dns1", orgVdcNetwork.Subnets.Values[0].DNSServer1)
	dSet(d, "dns2", orgVdcNetwork.Subnets.Values[0].DNSServer2)
	dSet(d, "dns_suffix", orgVdcNetwork.Subnets.Values[0].DNSSuffix)
	dSet(d, "is_shared", orgVdcNetwork.Shared)

	// If any IP sets are available
	if len(orgVdcNetwork.Subnets.Values[0].IPRanges.Values) > 0 {
		ipRangeSlice := make([]interface{}, len(orgVdcNetwork.Subnets.Values[0].IPRanges.Values))
		for index, ipRange := range orgVdcNetwork.Subnets.Values[0].IPRanges.Values {
			ipRangeMap := make(map[string]interface{})
			ipRangeMap["start_address"] = ipRange.StartAddress
			ipRangeMap["end_address"] = ipRange.EndAddress

			ipRangeSlice[index] = ipRangeMap
		}
		ipRangeSet := schema.NewSet(schema.HashResource(networkV2IpRange), ipRangeSlice)

		err := d.Set("static_ip_pool", ipRangeSet)
		if err != nil {
			return fmt.Errorf("error setting 'static_ip_pool': %s", err)
		}
	}
	return nil
}

func getOpenApiOrgVdcIsolatedNetworkType(d *schema.ResourceData, vcdClient *VCDClient) (*types.OpenApiOrgVdcNetwork, error) {
	inheritedVdcField := vcdClient.Vdc
	vdcField := d.Get("vdc").(string)
	ownerIdField := d.Get("owner_id").(string)

	ownerId, err := getOwnerId(d, vcdClient, ownerIdField, vdcField, inheritedVdcField)
	if err != nil {
		return nil, fmt.Errorf("error finding owner reference: %s", err)
	}

	orgVdcNetworkConfig := &types.OpenApiOrgVdcNetwork{
		Name:        d.Get("name").(string),
		Description: d.Get("description").(string),
		OwnerRef:    &types.OpenApiReference{ID: ownerId},

		NetworkType: types.OrgVdcNetworkTypeIsolated,
		Shared:      takeBoolPointer(d.Get("is_shared").(bool)),

		Subnets: types.OrgVdcNetworkSubnets{
			Values: []types.OrgVdcNetworkSubnetValues{
				{
					Gateway:      d.Get("gateway").(string),
					PrefixLength: d.Get("prefix_length").(int),
					DNSServer1:   d.Get("dns1").(string),
					DNSServer2:   d.Get("dns2").(string),
					DNSSuffix:    d.Get("dns_suffix").(string),
					IPRanges: types.OrgVdcNetworkSubnetIPRanges{
						Values: processIpRanges(d.Get("static_ip_pool").(*schema.Set)),
					},
				},
			},
		},
	}

	return orgVdcNetworkConfig, nil
}

func createOrUpdateOpenApiNetworkMetadata(d *schema.ResourceData, network *govcd.OpenApiOrgVdcNetwork) error {
	log.Printf("[TRACE] adding/updating metadata to Network V2")

	// Metadata is not supported when the network is in a VDC Group
	if govcd.OwnerIsVdcGroup(network.OpenApiOrgVdcNetwork.OwnerRef.ID) {
		return nil
	}

	if d.HasChange("metadata") {
		oldRaw, newRaw := d.GetChange("metadata")
		oldMetadata := oldRaw.(map[string]interface{})
		newMetadata := newRaw.(map[string]interface{})
		var toBeRemovedMetadata []string
		// Check if any key in old metadata was removed in new metadata.
		// Creates a list of keys to be removed.
		for k := range oldMetadata {
			if _, ok := newMetadata[k]; !ok {
				toBeRemovedMetadata = append(toBeRemovedMetadata, k)
			}
		}
		for _, k := range toBeRemovedMetadata {
			err := network.DeleteMetadataEntry(k)
			if err != nil {
				return fmt.Errorf("error deleting metadata: %s", err)
			}
		}
		// Add new metadata
		for k, v := range newMetadata {
			err := network.AddMetadataEntry(types.MetadataStringValue, k, v.(string))
			if err != nil {
				return fmt.Errorf("error adding metadata: %s", err)
			}
		}
	}
	return nil
}

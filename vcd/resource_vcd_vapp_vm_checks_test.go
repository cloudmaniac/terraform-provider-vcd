//go:build vapp || vm || catalog || ALL || functional
// +build vapp vm catalog ALL functional

package vcd

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/vmware/go-vcloud-director/v2/govcd"
)

func testAccCheckVcdVAppVmExists(vappName, vmName, node string, vapp *govcd.VApp, vm *govcd.VM) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[node]
		if !ok {
			return fmt.Errorf("not found: %s", node)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("no vApp ID is set")
		}

		conn := testAccProvider.Meta().(*VCDClient)
		_, vdc, err := conn.GetOrgAndVdc(testConfig.VCD.Org, testConfig.VCD.Vdc)
		if err != nil {
			return fmt.Errorf(errorRetrievingVdcFromOrg, testConfig.VCD.Vdc, testConfig.VCD.Org, err)
		}

		newVapp, err := vdc.GetVAppByName(vappName, false)
		if err != nil {
			return err
		}

		*vapp = *newVapp

		newVm, err := newVapp.GetVMByName(vmName, false)

		if err != nil {
			return err
		}

		*vm = *newVm

		return nil
	}
}

func testAccCheckVcdVAppVmDestroy(vappName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		conn := testAccProvider.Meta().(*VCDClient)

		for _, rs := range s.RootModule().Resources {
			if rs.Type != "vcd_vapp" {
				continue
			}
			_, vdc, err := conn.GetOrgAndVdc(testConfig.VCD.Org, testConfig.VCD.Vdc)
			if err != nil {
				return fmt.Errorf(errorRetrievingVdcFromOrg, testConfig.VCD.Vdc, testConfig.VCD.Org, err)
			}

			_, err = vdc.GetVAppByName(vappName, false)

			if err == nil {
				return fmt.Errorf("VPCs still exist")
			}

			return nil
		}

		return nil
	}
}

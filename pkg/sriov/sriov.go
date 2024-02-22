// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2023 Network Plumping Working Group
// Copyright (C) 2023 Nordix Foundation.
// Copyright (c) 2024 Ericsson AB.

// Package sriov handles VF plumbing
package sriov

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/k8snetworkplumbingwg/sriovnet"

	evpngwtypes "github.com/opiproject/opi-gateway-evpn-cni/pkg/types"
	"github.com/opiproject/opi-gateway-evpn-cni/pkg/utils"
)

type pciUtils interface {
	GetSriovNumVfs(ifName string) (int, error)
	GetVFLinkNamesFromVFID(pfName string, vfID int) ([]string, error)
	GetPciAddress(ifName string, vf int) (string, error)
	EnableArpAndNdiscNotify(ifName string) error
}

type pciUtilsImpl struct{}

func (p *pciUtilsImpl) GetSriovNumVfs(ifName string) (int, error) {
	return utils.GetSriovNumVfs(ifName)
}

func (p *pciUtilsImpl) GetVFLinkNamesFromVFID(pfName string, vfID int) ([]string, error) {
	return utils.GetVFLinkNamesFromVFID(pfName, vfID)
}

func (p *pciUtilsImpl) GetPciAddress(ifName string, vf int) (string, error) {
	return utils.GetPciAddress(ifName, vf)
}

func (p *pciUtilsImpl) EnableArpAndNdiscNotify(ifName string) error {
	return utils.EnableArpAndNdiscNotify(ifName)
}

// Manager provides interface invoke sriov nic related operations
type Manager interface {
	SetupVF(conf *evpngwtypes.NetConf, podifName string, netns ns.NetNS) (string, error)
	ReleaseVF(conf *evpngwtypes.NetConf, netns ns.NetNS, netNSPath string) error
	ResetVFConfig(conf *evpngwtypes.NetConf) error
	ResetVF(conf *evpngwtypes.NetConf) error
	ApplyVFConfig(conf *evpngwtypes.NetConf) error
	FillOriginalVfInfo(conf *evpngwtypes.NetConf) error
}

type sriovManager struct {
	nLink utils.NetlinkManager
	utils pciUtils
}

// NewSriovManager returns an instance of SriovManager
func NewSriovManager() Manager {
	return &sriovManager{
		nLink: &utils.MyNetlink{},
		utils: &pciUtilsImpl{},
	}
}

// SetupVF sets up a VF in Pod netns
func (s *sriovManager) SetupVF(conf *evpngwtypes.NetConf, podifName string, netns ns.NetNS) (string, error) {
	linkName := conf.OrigVfState.HostIFName

	linkObj, err := s.nLink.LinkByName(linkName)
	if err != nil {
		return "", fmt.Errorf("error getting VF netdevice with name %s", linkName)
	}

	// tempName used as intermediary name to avoid name conflicts
	tempName := fmt.Sprintf("%s%d", "temp_", linkObj.Attrs().Index)

	// 1. Set link down
	if err := s.nLink.LinkSetDown(linkObj); err != nil {
		return "", fmt.Errorf("failed to down vf device %q: %v", linkName, err)
	}

	// 2. Set temp name
	if err := s.nLink.LinkSetName(linkObj, tempName); err != nil {
		return "", fmt.Errorf("error setting temp IF name %s for %s", tempName, linkName)
	}

	macAddress := linkObj.Attrs().HardwareAddr.String()
	if conf.MAC != "" {
		fmt.Printf("SetupVF(): MAC address configuration functionality is not supported currently")
		fmt.Printf("SetupVF(): MAC address %s will be ignored", conf.MAC)
	}
	// 3. Set MAC address
	// Identation is wrong here. Has been mixed up during the commenting out of functionality.
	// Everything under if conf.Mac should be one level in
	/*if conf.MAC != "" {
	hwaddr, err := net.ParseMAC(conf.MAC)
	if err != nil {
		return "", fmt.Errorf("failed to parse MAC address %s: %v", conf.MAC, err)
	}

	// Save the original effective MAC address before overriding it
	conf.OrigVfState.EffectiveMAC = linkObj.Attrs().HardwareAddr.String()*/

	/* Some NIC drivers (i.e. i40e/iavf) set VF MAC address asynchronously
	   via PF. This means that while the PF could already show the VF with
	   the desired MAC address, the netdev VF may still have the original
	   one. If in this window we issue a netdev VF MAC address set, the driver
	   will return an error and the pod will fail to create.
	   Other NICs (Mellanox) require explicit netdev VF MAC address so we
	   cannot skip this part.
	   Retry up to 5 times; wait 200 milliseconds between retries
	*/
	/*err = utils.Retry(5, 200*time.Millisecond, func() error {
			return s.nLink.LinkSetHardwareAddr(linkObj, hwaddr)
		})

		if err != nil {
			return "", fmt.Errorf("failed to set netlink MAC address to %s: %v", hwaddr, err)
		}
		macAddress = conf.MAC
	}*/

	// 4. Change netns
	if err := s.nLink.LinkSetNsFd(linkObj, int(netns.Fd())); err != nil {
		return "", fmt.Errorf("failed to move IF %s to netns: %q", tempName, err)
	}

	if err := netns.Do(func(_ ns.NetNS) error {
		// 5. Set Pod IF name
		if err := s.nLink.LinkSetName(linkObj, podifName); err != nil {
			return fmt.Errorf("error setting container interface name %s for %s", linkName, tempName)
		}

		// 6. Enable IPv4 ARP notify and IPv6 Network Discovery notify
		// Error is ignored here because enabling this feature is only a performance enhancement.
		_ = s.utils.EnableArpAndNdiscNotify(podifName)

		// 7. Bring IF up in Pod netns
		if err := s.nLink.LinkSetUp(linkObj); err != nil {
			return fmt.Errorf("error bringing interface up in container ns: %q", err)
		}

		return nil
	}); err != nil {
		return "", fmt.Errorf("error setting up interface in container namespace: %q", err)
	}

	return macAddress, nil
}

// ReleaseVF reset a VF from Pod netns and return it to init netns
func (s *sriovManager) ReleaseVF(conf *evpngwtypes.NetConf, netns ns.NetNS, netNSPath string) error {
	initns, err := ns.GetCurrentNS()
	if err != nil {
		return fmt.Errorf("ReleaseVF(): failed to get init netns: %v", err)
	}

	// get VF netdevice from PCI that is attached to container. This is executed on the host namespace accessing
	// the containers filesystem through the /proc/<PID> path on the host.
	vfNetdevices, err := utils.GetContainerNetDevFromPci(netNSPath, conf.DeviceID)
	if err != nil {
		return fmt.Errorf("ReleaseVF(): failed to get VF netdevice from PCI %s : %v", conf.DeviceID, err)
	}

	if len(vfNetdevices) == 0 {
		// The VF has not been found in the Container namespace so no point to continue
		return nil
	}

	podifName := vfNetdevices[0]
	return netns.Do(func(_ ns.NetNS) error {
		// get VF device
		linkObj, err := s.nLink.LinkByName(podifName)
		if err != nil {
			return fmt.Errorf("ReleaseVF(): failed to get netlink device with name %s: %q", podifName, err)
		}

		// shutdown VF device
		if err = s.nLink.LinkSetDown(linkObj); err != nil {
			return fmt.Errorf("ReleaseVF(): failed to set link %s down: %q", podifName, err)
		}

		// rename VF device
		err = s.nLink.LinkSetName(linkObj, conf.OrigVfState.HostIFName)
		if err != nil {
			return fmt.Errorf("ReleaseVF(): failed to rename link %s to host name %s: %q", podifName, conf.OrigVfState.HostIFName, err)
		}

		// Bring VF device UP
		err = s.nLink.LinkSetUp(linkObj)
		if err != nil {
			return fmt.Errorf("ReleaseVF(): failed to set link %s up: %q", podifName, err)
		}

		// reset effective MAC address
		if conf.MAC != "" {
			fmt.Printf("ReleaseVF(): MAC address configuration functionality is not supported currently")
			fmt.Printf("ReleaseVF(): MAC address %s will be ignored", conf.MAC)
		}
		/*if conf.MAC != "" {
			hwaddr, err := net.ParseMAC(conf.OrigVfState.EffectiveMAC)
			if err != nil {
				return fmt.Errorf("failed to parse original effective MAC address %s: %v", conf.OrigVfState.EffectiveMAC, err)
			}

			if err = s.nLink.LinkSetHardwareAddr(linkObj, hwaddr); err != nil {
				return fmt.Errorf("failed to restore original effective netlink MAC address %s: %v", hwaddr, err)
			}
		}*/

		// move VF device to init netns
		if err = s.nLink.LinkSetNsFd(linkObj, int(initns.Fd())); err != nil {
			return fmt.Errorf("ReleaseVF(): failed to move interface %s to init netns: %v", conf.OrigVfState.HostIFName, err)
		}

		return nil
	})
}

// This is an unused function for now. Comment it out but keep it
// in case is needed in the future
/*func getVfInfo(link netlink.Link, id int) *netlink.VfInfo {
	attrs := link.Attrs()
	for _, vf := range attrs.Vfs {
		if vf.ID == id {
			return &vf
		}
	}
	return nil
}*/

// ApplyVFConfig configure a VF with parameters given in NetConf
func (s *sriovManager) ApplyVFConfig(conf *evpngwtypes.NetConf) error {
	if conf.MAC != "" {
		fmt.Printf("ApplyVFConfig(): MAC address configuration functionality is not supported currently")
		fmt.Printf("ApplyVFConfig(): MAC address %s will be ignored", conf.MAC)
	}

	/*pfLink, err := s.nLink.LinkByName(conf.Master)
	if err != nil {
		return fmt.Errorf("failed to lookup master %q: %v", conf.Master, err)
	}

	// 1. Set mac address
	if conf.MAC != "" {
		hwaddr, err := net.ParseMAC(conf.MAC)
		if err != nil {
			return fmt.Errorf("failed to parse MAC address %s: %v", conf.MAC, err)
		}

		if err = s.nLink.LinkSetVfHardwareAddr(pfLink, conf.VFID, hwaddr); err != nil {
			return fmt.Errorf("failed to set MAC address to %s: %v", hwaddr, err)
		}
	}

	// 2. Set min/max tx link rate. 0 means no rate limiting. Support depends on NICs and driver.
	var minTxRate, maxTxRate int
	rateConfigured := false
	if conf.MinTxRate != nil {
		minTxRate = *conf.MinTxRate
		rateConfigured = true
	}

	if conf.MaxTxRate != nil {
		maxTxRate = *conf.MaxTxRate
		rateConfigured = true
	}

	if rateConfigured {
		if err = s.nLink.LinkSetVfRate(pfLink, conf.VFID, minTxRate, maxTxRate); err != nil {
			return fmt.Errorf("failed to set vf %d min_tx_rate to %d Mbps: max_tx_rate to %d Mbps: %v",
				conf.VFID, minTxRate, maxTxRate, err)
		}
	}

	// 3. Set spoofchk flag
	if conf.SpoofChk != "" {
		spoofChk := false
		if conf.SpoofChk == "on" {
			spoofChk = true
		}
		if err = s.nLink.LinkSetVfSpoofchk(pfLink, conf.VFID, spoofChk); err != nil {
			return fmt.Errorf("failed to set vf %d spoofchk flag to %s: %v", conf.VFID, conf.SpoofChk, err)
		}
	}

	// 4. Set trust flag
	if conf.Trust != "" {
		trust := false
		if conf.Trust == "on" {
			trust = true
		}
		if err = s.nLink.LinkSetVfTrust(pfLink, conf.VFID, trust); err != nil {
			return fmt.Errorf("failed to set vf %d trust flag to %s: %v", conf.VFID, conf.Trust, err)
		}
	}

	// 5. Set link state
	if conf.LinkState != "" {
		var state uint32
		switch conf.LinkState {
		case "auto":
			state = netlink.VF_LINK_STATE_AUTO
		case "enable":
			state = netlink.VF_LINK_STATE_ENABLE
		case "disable":
			state = netlink.VF_LINK_STATE_DISABLE
		default:
			// the value should have been validated earlier, return error if we somehow got here
			return fmt.Errorf("unknown link state %s when setting it for vf %d: %v", conf.LinkState, conf.VFID, err)
		}
		if err = s.nLink.LinkSetVfState(pfLink, conf.VFID, state); err != nil {
			return fmt.Errorf("failed to set vf %d link state to %d: %v", conf.VFID, state, err)
		}
	}*/

	return nil
}

// FillOriginalVfInfo fills the original vf info
/*func (s *sriovManager) FillOriginalVfInfo(conf *evpngwtypes.NetConf) error {
	pfLink, err := s.nLink.LinkByName(conf.Master)
	if err != nil {
		return fmt.Errorf("failed to lookup master %q: %v", conf.Master, err)
	}
	// Save current the VF state before modifying it
	vfState := getVfInfo(pfLink, conf.VFID)
	if vfState == nil {
		return fmt.Errorf("failed to find vf %d", conf.VFID)
	}
	conf.OrigVfState.FillFromVfInfo(vfState)
	return err
}*/

// FillOriginalVfInfo fills the original vf info
func (s *sriovManager) FillOriginalVfInfo(_ *evpngwtypes.NetConf) error {
	return nil
}

// ResetVFConfig reset a VF to its original administrative state
func (s *sriovManager) ResetVFConfig(conf *evpngwtypes.NetConf) error {
	if conf.MAC != "" {
		fmt.Printf("ResetVFConfig(): MAC address configuration functionality is not supported currently")
		fmt.Printf("ResetVFConfig(): MAC address %s will be ignored", conf.MAC)
	}
	/*pfLink, err := s.nLink.LinkByName(conf.Master)
	if err != nil {
		return fmt.Errorf("failed to lookup master %q: %v", conf.Master, err)
	}*/

	// Restore the original administrative MAC address
	// Identation is wrong here. Has been mixed up during the commenting out of functionality.
	// Everything under if conf.Mac should be one level in
	/*if conf.MAC != "" {
	hwaddr, err := net.ParseMAC(conf.OrigVfState.AdminMAC)
	if err != nil {
		return fmt.Errorf("failed to parse original administrative MAC address %s: %v", conf.OrigVfState.AdminMAC, err)
	}*/

	/* Some NIC drivers (i.e. i40e/iavf) set VF MAC address asynchronously
	   via PF. This means that while the PF could already show the VF with
	   the desired MAC address, the netdev VF may still have the original
	   one. If in this window we issue a netdev VF MAC address set, the driver
	   will return an error and the pod will fail to create.
	   Other NICs (Mellanox) require explicit netdev VF MAC address so we
	   cannot skip this part.
	   Retry up to 5 times; wait 200 milliseconds between retries
	*/
	/*err = utils.Retry(5, 200*time.Millisecond, func() error {
			return s.nLink.LinkSetVfHardwareAddr(pfLink, conf.VFID, hwaddr)
		})
		if err != nil {
			return fmt.Errorf("failed to restore original administrative MAC address %s: %v", hwaddr, err)
		}
	}*/

	// Restore VF trust
	/*if conf.Trust != "" {
		// TODO: netlink go implementation does not support getting VF trust, need to add support there first
		// for now, just set VF trust to off if it was specified by the user in netconf
		if err = s.nLink.LinkSetVfTrust(pfLink, conf.VFID, false); err != nil {
			return fmt.Errorf("failed to disable trust for vf %d: %v", conf.VFID, err)
		}
	}

	// Restore rate limiting
	if conf.MinTxRate != nil || conf.MaxTxRate != nil {
		if err = s.nLink.LinkSetVfRate(pfLink, conf.VFID, conf.OrigVfState.MinTxRate, conf.OrigVfState.MaxTxRate); err != nil {
			return fmt.Errorf("failed to disable rate limiting for vf %d %v", conf.VFID, err)
		}
	}

	// Restore link state to `auto`
	if conf.LinkState != "" {
		// Reset only when link_state was explicitly specified, to  accommodate for drivers / NICs
		// that don't support the netlink command (e.g. igb driver)
		if err = s.nLink.LinkSetVfState(pfLink, conf.VFID, conf.OrigVfState.LinkState); err != nil {
			return fmt.Errorf("failed to set link state to auto for vf %d: %v", conf.VFID, err)
		}
	}

	// Restore spoofchk
	if conf.SpoofChk != "" {
		if err = s.nLink.LinkSetVfSpoofchk(pfLink, conf.VFID, conf.OrigVfState.SpoofChk); err != nil {
			return fmt.Errorf("failed to restore spoofchk for vf %d: %v", conf.VFID, err)
		}
	}*/

	return nil
}

// ResetVF resets a netdev VF to its original state
func (s *sriovManager) ResetVF(conf *evpngwtypes.NetConf) error {
	// Maybe in this function we need to handle the OriginalVfState.EffectiveMac
	// Check ReleaseVF func

	// get VF netdevice from PCI
	vfNetdevices, err := sriovnet.GetNetDevicesFromPci(conf.DeviceID)
	if err != nil {
		return fmt.Errorf("ResetVF(): failed to get VF netdevice from PCI %s : %v", conf.DeviceID, err)
	}

	if len(vfNetdevices) == 0 {
		// The VF has not been found in the host namespace so no point to continue.
		// This is according to the idempotent logic where if something is not found
		// then is considered that is not an error.
		return nil
	}

	curNetVFName := vfNetdevices[0]

	// get VF device
	linkObj, err := s.nLink.LinkByName(curNetVFName)
	if err != nil {
		return fmt.Errorf("ResetVF(): failed to get netlink device with name %s: %q", curNetVFName, err)
	}

	// shutdown VF device
	if err = s.nLink.LinkSetDown(linkObj); err != nil {
		return fmt.Errorf("ResetVF(): failed to set link %s down: %q", curNetVFName, err)
	}

	// Delete the altname of the MEV VF.
	// If the altname of the original name of the VF is not deleted the renaming will fail
	// Note: We need installed on the host bash shell and ip executable for commands to be executed correctly
	searchStr := "'altname " + conf.OrigVfState.HostIFName + "'"
	showCmd := "ip link show " + curNetVFName + " | grep -o " + searchStr + " | cut -d ' ' -f 2"
	altnameByte, err := exec.Command("bash", "-c", showCmd).Output() // #nosec G204
	if err != nil {
		return fmt.Errorf("ResetVF(): failed to get the altname: %q", err)
	}

	altname := strings.TrimSpace(string(altnameByte))
	if altname == conf.OrigVfState.HostIFName {
		delCmd := "ip link property del dev " + curNetVFName + " altname " + conf.OrigVfState.HostIFName
		_, err := exec.Command("bash", "-c", delCmd).Output() // #nosec G204
		if err != nil {
			return fmt.Errorf("ResetVF(): failed to delete the altname: %q", err)
		}
	}

	// rename VF device
	err = s.nLink.LinkSetName(linkObj, conf.OrigVfState.HostIFName)
	if err != nil {
		return fmt.Errorf("ResetVF(): failed to rename link %s to host name %s: %q", curNetVFName, conf.OrigVfState.HostIFName, err)
	}

	// Bring VF device UP
	err = s.nLink.LinkSetUp(linkObj)
	if err != nil {
		return fmt.Errorf("ResetVF(): failed to set link %s up: %q", curNetVFName, err)
	}

	return nil
}

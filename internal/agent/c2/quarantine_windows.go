//go:build windows

package c2

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

const (
	fwDirOut        int32 = 2
	fwDirIn         int32 = 1
	fwActBlock      int32 = 0
	fwActAllow      int32 = 1
	fwProtoTCP      int32 = 6
	fwProtoAny      int32 = 256
	ruleBlockOut    = "ARX MDM Quarantine Block Outbound"
	ruleBlockIn     = "ARX MDM Quarantine Block Inbound"
	ruleAllowServer = "ARX MDM Quarantine Allow MDM Egress"
)

func platformApplyQuarantine(enabled bool, hosts []string, ports []uint16) (string, error) {
	if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err != nil {
		return "", fmt.Errorf("com init: %w", err)
	}
	defer ole.CoUninitialize()

	unkPolicy, err := oleutil.CreateObject("HNetCfg.FwPolicy2")
	if err != nil {
		return "", fmt.Errorf("firewall policy com object: %w", err)
	}
	defer unkPolicy.Release()

	policy := unkPolicy.MustQueryInterface(ole.IID_IDispatch)
	defer policy.Release()

	for _, name := range []string{ruleAllowServer, ruleBlockOut, ruleBlockIn} {
		_ = firewallRemoveRule(policy, name)
	}

	if !enabled {
		return "windows firewall quarantine cleared", nil
	}

	var allowIPs []string
	for _, h := range hosts {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		if pip := net.ParseIP(h); pip != nil {
			if pip4 := pip.To4(); pip4 != nil {
				allowIPs = append(allowIPs, pip4.String())
			}
			continue
		}
		ips, err := net.LookupIP(h)
		if err != nil {
			return "", fmt.Errorf("resolve %q: %w", h, err)
		}
		for _, ip := range ips {
			if ip4 := ip.To4(); ip4 != nil {
				allowIPs = append(allowIPs, ip4.String())
			}
		}
	}
	if len(allowIPs) == 0 {
		return "", fmt.Errorf("no IPv4 MDM endpoints resolved for allow_hosts")
	}
	if len(ports) == 0 {
		ports = []uint16{443}
	}

	if err := firewallAddRule(policy, ruleBlockOut, fwDirOut, fwActBlock, fwProtoAny, "", "", ""); err != nil {
		return "", err
	}
	if err := firewallAddRule(policy, ruleBlockIn, fwDirIn, fwActBlock, fwProtoAny, "", "", ""); err != nil {
		return "", err
	}
	portParts := make([]string, 0, len(ports))
	for _, p := range ports {
		portParts = append(portParts, strconv.Itoa(int(p)))
	}
	remotePorts := strings.Join(portParts, ",")
	remoteAddrs := strings.Join(allowIPs, ",")
	if err := firewallAddRule(policy, ruleAllowServer, fwDirOut, fwActAllow, fwProtoTCP, remoteAddrs, remotePorts, ""); err != nil {
		return "", err
	}

	return "windows advanced firewall quarantine rules installed", nil
}

func firewallRemoveRule(policy *ole.IDispatch, name string) error {
	rulesRaw, err := oleutil.GetProperty(policy, "Rules")
	if err != nil {
		return err
	}
	rules := rulesRaw.ToIDispatch()
	defer rules.Release()
	_, err = oleutil.CallMethod(rules, "Remove", name)
	return err
}

func firewallAddRule(policy *ole.IDispatch, name string, direction, action, protocol int32, remoteAddrs, remotePorts, localPorts string) error {
	unkRule, err := oleutil.CreateObject("HNetCfg.FwRule")
	if err != nil {
		return fmt.Errorf("create rule %s: %w", name, err)
	}
	defer unkRule.Release()
	rule := unkRule.MustQueryInterface(ole.IID_IDispatch)
	defer rule.Release()

	if _, err := oleutil.PutProperty(rule, "Name", name); err != nil {
		return err
	}
	if _, err := oleutil.PutProperty(rule, "Description", "Installed by ARX MDM network isolation"); err != nil {
		return err
	}
	if _, err := oleutil.PutProperty(rule, "Enabled", true); err != nil {
		return err
	}
	if _, err := oleutil.PutProperty(rule, "Direction", direction); err != nil {
		return err
	}
	if _, err := oleutil.PutProperty(rule, "Action", action); err != nil {
		return err
	}
	if _, err := oleutil.PutProperty(rule, "Protocol", protocol); err != nil {
		return err
	}
	if _, err := oleutil.PutProperty(rule, "InterfaceTypes", "All"); err != nil {
		return err
	}
	if remoteAddrs != "" {
		if _, err := oleutil.PutProperty(rule, "RemoteAddresses", remoteAddrs); err != nil {
			return err
		}
	}
	if remotePorts != "" {
		if _, err := oleutil.PutProperty(rule, "RemotePorts", remotePorts); err != nil {
			return err
		}
	}
	if localPorts != "" {
		if _, err := oleutil.PutProperty(rule, "LocalPorts", localPorts); err != nil {
			return err
		}
	}

	rulesRaw, err := oleutil.GetProperty(policy, "Rules")
	if err != nil {
		return err
	}
	rules := rulesRaw.ToIDispatch()
	defer rules.Release()
	if _, err := oleutil.CallMethod(rules, "Add", rule); err != nil {
		return fmt.Errorf("rules.Add %s: %w", name, err)
	}
	return nil
}

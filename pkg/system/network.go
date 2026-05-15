package system

import (
	"fmt"
	"net"
	"strings"
)

// IPAddress is one address bound to an interface.
type IPAddress struct {
	Addr string `json:"addr"` // IP or CIDR text from net package
}

// NetworkInterface summarizes a system network interface from net.Interfaces.
type NetworkInterface struct {
	Index        int          `json:"index"`
	Name         string       `json:"name"`
	MTU          int          `json:"mtu"`
	HardwareAddr string       `json:"hardware_addr,omitempty"`
	Flags        string       `json:"flags"`
	Up           bool         `json:"up"`
	Loopback     bool         `json:"loopback"`
	Multicast    bool         `json:"multicast"`
	Addrs        []IPAddress  `json:"addrs"`
}

// ListNetworkInterfaces enumerates interfaces and addresses using the standard net package.
func ListNetworkInterfaces() ([]NetworkInterface, error) {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	out := make([]NetworkInterface, 0, len(ifs))
	for _, iface := range ifs {
		addrs, err := iface.Addrs()
		if err != nil {
			return nil, fmt.Errorf("addrs for %s: %w", iface.Name, err)
		}
		ipAddrs := make([]IPAddress, 0, len(addrs))
		for _, a := range addrs {
			ipAddrs = append(ipAddrs, IPAddress{Addr: addrString(a)})
		}
		flags := iface.Flags.String()
		out = append(out, NetworkInterface{
			Index:        iface.Index,
			Name:         iface.Name,
			MTU:          iface.MTU,
			HardwareAddr: formatHardwareAddr(iface.HardwareAddr),
			Flags:        flags,
			Up:           iface.Flags&net.FlagUp != 0,
			Loopback:     iface.Flags&net.FlagLoopback != 0,
			Multicast:    iface.Flags&net.FlagMulticast != 0,
			Addrs:        ipAddrs,
		})
	}
	return out, nil
}

func formatHardwareAddr(hw net.HardwareAddr) string {
	if len(hw) == 0 {
		return ""
	}
	return strings.ToUpper(hw.String())
}

func addrString(a net.Addr) string {
	switch v := a.(type) {
	case *net.IPNet:
		if v == nil {
			return ""
		}
		if v.IP.To4() != nil && v.Mask != nil {
			ones, _ := v.Mask.Size()
			if ones >= 0 {
				return fmt.Sprintf("%s/%d", v.IP.String(), ones)
			}
		}
		return v.String()
	case *net.IPAddr:
		if v == nil {
			return ""
		}
		return v.String()
	default:
		return a.String()
	}
}

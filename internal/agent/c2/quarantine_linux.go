//go:build linux && !android

package c2

import (
	"encoding/binary"
	"fmt"
	"net"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
)

const linuxQuarantineTable = "arx_mdm_quarantine"

func linuxApplyQuarantine(enabled bool, hosts []string, ports []uint16) (string, error) {
	c := &nftables.Conn{}
	defer c.Close()

	if err := linuxDeleteQuarantineTable(c); err != nil {
		return "", err
	}
	if err := c.Flush(); err != nil {
		return "", fmt.Errorf("nft flush after cleanup: %w", err)
	}
	if !enabled {
		return "linux network quarantine cleared", nil
	}

	var v4 []net.IP
	for _, h := range hosts {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		if pip := net.ParseIP(h); pip != nil {
			if pip4 := pip.To4(); pip4 != nil {
				v4 = append(v4, pip4)
			}
			continue
		}
		ips, err := net.LookupIP(h)
		if err != nil {
			return "", fmt.Errorf("resolve %q: %w", h, err)
		}
		for _, ip := range ips {
			if ip4 := ip.To4(); ip4 != nil {
				v4 = append(v4, ip4)
			}
		}
	}
	if len(v4) == 0 {
		return "", fmt.Errorf("no IPv4 targets for allow_hosts; supply IPs or resolvable hostnames in ARX_MDM_PUBLIC_HOST")
	}
	if len(ports) == 0 {
		ports = []uint16{443}
	}

	table := c.AddTable(&nftables.Table{
		Family: nftables.TableFamilyINet,
		Name:   linuxQuarantineTable,
	})
	polDrop := nftables.ChainPolicyDrop
	chain := c.AddChain(&nftables.Chain{
		Name:     "arx_q_out",
		Table:    table,
		Type:     nftables.ChainTypeFilter,
		Hooknum:  nftables.ChainHookOutput,
		Priority: nftables.ChainPriorityRef(-150),
		Policy:   &polDrop,
	})

	c.AddRule(&nftables.Rule{
		Table: table,
		Chain: chain,
		Exprs: []expr.Any{
			&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1},
			&expr.Cmp{
				Op:       expr.CmpOpEq,
				Register: 1,
				Data:     []byte{unix.IPPROTO_UDP},
			},
			&expr.Payload{
				DestRegister: 1,
				Base:         expr.PayloadBaseTransportHeader,
				Offset:       2,
				Len:          2,
			},
			&expr.Cmp{
				Op:       expr.CmpOpEq,
				Register: 1,
				Data:     binary.BigEndian.AppendUint16(nil, 53),
			},
			&expr.Verdict{Kind: expr.VerdictAccept},
		},
	})

	c.AddRule(&nftables.Rule{
		Table: table,
		Chain: chain,
		Exprs: []expr.Any{
			&expr.Payload{
				DestRegister: 1,
				Base:         expr.PayloadBaseNetworkHeader,
				Offset:       16,
				Len:          4,
			},
			&expr.Bitwise{
				SourceRegister: 1,
				DestRegister:   1,
				Len:            4,
				Xor:            []byte{0x0, 0x0, 0x0, 0x0},
				Mask:           net.CIDRMask(8, 32),
			},
			&expr.Cmp{
				Op:       expr.CmpOpEq,
				Register: 1,
				Data:     net.IPv4(127, 0, 0, 0).To4(),
			},
			&expr.Verdict{Kind: expr.VerdictAccept},
		},
	})

	for _, ip := range v4 {
		for _, port := range ports {
			p := port
			ipCopy := append(net.IP(nil), ip...)
			c.AddRule(&nftables.Rule{
				Table: table,
				Chain: chain,
				Exprs: []expr.Any{
					&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1},
					&expr.Cmp{
						Op:       expr.CmpOpEq,
						Register: 1,
						Data:     []byte{unix.IPPROTO_TCP},
					},
					&expr.Payload{
						DestRegister: 1,
						Base:         expr.PayloadBaseNetworkHeader,
						Offset:       16,
						Len:          4,
					},
					&expr.Cmp{
						Op:       expr.CmpOpEq,
						Register: 1,
						Data:     ipCopy,
					},
					&expr.Payload{
						DestRegister: 1,
						Base:         expr.PayloadBaseTransportHeader,
						Offset:       2,
						Len:          2,
					},
					&expr.Cmp{
						Op:       expr.CmpOpEq,
						Register: 1,
						Data:     binary.BigEndian.AppendUint16(nil, p),
					},
					&expr.Verdict{Kind: expr.VerdictAccept},
				},
			})
		}
	}

	if err := c.Flush(); err != nil {
		return "", fmt.Errorf("nft apply quarantine: %w", err)
	}
	return "linux network quarantine active (inet filter output)", nil
}

func linuxDeleteQuarantineTable(c *nftables.Conn) error {
	tables, err := c.ListTablesOfFamily(nftables.TableFamilyINet)
	if err != nil {
		return fmt.Errorf("nft list tables: %w", err)
	}
	for _, t := range tables {
		if t.Name == linuxQuarantineTable {
			c.DelTable(t)
		}
	}
	return nil
}

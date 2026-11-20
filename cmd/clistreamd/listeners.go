package main

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strings"
)

type endpoint struct{ bind, advertised string }

func endpoints(names []string, listen, advertised string) ([]endpoint, error) {
	host, port, e := net.SplitHostPort(listen)
	if e != nil {
		return nil, e
	}
	if len(names) == 0 {
		if advertised == "" {
			if host == "" || net.ParseIP(host).IsUnspecified() {
				return nil, errors.New("set --advertise to a reachable host:port")
			}
			advertised = listen
		}
		return []endpoint{{listen, advertised}}, nil
	}
	if advertised != "" {
		return nil, errors.New("choose either --interface or --advertise")
	}
	if host != "" && host != "127.0.0.1" {
		return nil, errors.New("use --listen :PORT with --interface")
	}
	var result []endpoint
	for _, name := range names {
		ips := []string{}
		if name == "loopback" {
			ips = append(ips, "127.0.0.1")
		} else {
			iface, e := net.InterfaceByName(name)
			if e != nil {
				return nil, e
			}
			addrs, e := iface.Addrs()
			if e != nil {
				return nil, e
			}
			for _, a := range addrs {
				ip, e := netip.ParseAddr(strings.SplitN(a.String(), "/", 2)[0])
				if e == nil && (ip.IsGlobalUnicast() || ip.IsLoopback()) {
					ips = append(ips, ip.String())
				}
			}
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("interface %s has no usable address", name)
		}
		for _, ip := range ips {
			addr := net.JoinHostPort(ip, port)
			item := endpoint{addr, addr}
			if !slices.Contains(result, item) {
				result = append(result, item)
			}
		}
	}
	return result, nil
}

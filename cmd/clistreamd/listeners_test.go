package main

import (
	"net"
	"testing"
)

func TestInterfaceSelection(t *testing.T) {
	local, e := endpoints(nil, "127.0.0.1:7443", "")
	if e != nil || len(local) != 1 || local[0].bind != "127.0.0.1:7443" {
		t.Fatal(local, e)
	}
	ifaces, e := net.Interfaces()
	if e != nil {
		t.Fatal(e)
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback == 0 {
			continue
		}
		selected, e := endpoints([]string{"loopback", iface.Name}, ":7443", "")
		if e != nil {
			t.Fatal(e)
		}
		seen := map[string]bool{}
		for _, item := range selected {
			host, _, e := net.SplitHostPort(item.bind)
			if e != nil || !net.ParseIP(host).IsLoopback() || seen[item.bind] || item.advertised != item.bind {
				t.Fatal(item)
			}
			seen[item.bind] = true
		}
		if !seen["127.0.0.1:7443"] {
			t.Fatal(selected)
		}
		return
	}
	t.Fatal("missing loopback interface")
}

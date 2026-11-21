package main

import (
	"bytes"
	"io"
	"net"
	"time"
)

// reverseServices gives the UI real TCP and UDP exports whose replies prove a round trip.
func reverseServices() (args []string, closeServices func(), err error) {
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, err
	}
	udp, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		tcp.Close()
		return nil, nil, err
	}
	go func() {
		for {
			c, e := tcp.Accept()
			if e != nil {
				return
			}
			go func() {
				defer c.Close()
				c.SetDeadline(time.Now().Add(8 * time.Second))
				data, e := io.ReadAll(io.LimitReader(c, 65536))
				if e == nil {
					c.Write(reverse(data))
				}
			}()
		}
	}()
	go func() {
		buf := make([]byte, 65536)
		for {
			n, addr, e := udp.ReadFrom(buf)
			if e != nil {
				return
			}
			udp.WriteTo(reverse(bytes.Clone(buf[:n])), addr)
		}
	}()
	return []string{"--share-port", "tcp=tcp:" + tcp.Addr().String(), "--share-port", "udp=udp:" + udp.LocalAddr().String()}, func() { tcp.Close(); udp.Close() }, nil
}
func reverse(data []byte) []byte {
	for i, j := 0, len(data)-1; i < j; i, j = i+1, j-1 {
		data[i], data[j] = data[j], data[i]
	}
	return data
}

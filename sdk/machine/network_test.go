package machine

import (
	"net"
	"testing"
)

func TestIsLoopbackInterface(t *testing.T) {
	flags := net.FlagUp | net.FlagRunning | net.FlagLoopback
	if !isLoopbackInterface(flags) {
		t.Fatal("expected interface with FlagLoopback to be identified as loopback")
	}

	if isLoopbackInterface(net.FlagUp | net.FlagRunning) {
		t.Fatal("expected non-loopback interface flags not to be identified as loopback")
	}
}

func TestIsAdvertisableIP(t *testing.T) {
	tests := []struct {
		name string
		ip   net.IP
		want bool
	}{
		{
			name: "public IPv4",
			ip:   net.ParseIP("192.0.2.1"),
			want: true,
		},
		{
			name: "private IPv6",
			ip:   net.ParseIP("fd00::1"),
			want: true,
		},
		{
			name: "loopback",
			ip:   net.ParseIP("127.0.0.1"),
			want: false,
		},
		{
			name: "IPv4 link-local",
			ip:   net.ParseIP("169.254.1.1"),
			want: false,
		},
		{
			name: "IPv6 link-local",
			ip:   net.ParseIP("fe80::1"),
			want: false,
		},
		{
			name: "nil",
			ip:   nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAdvertisableIP(tt.ip); got != tt.want {
				t.Fatalf("isAdvertisableIP(%v) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

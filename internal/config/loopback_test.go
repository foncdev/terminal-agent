package config

import "testing"

func TestIsLoopback(t *testing.T) {
	for _, h := range []string{"localhost", "127.0.0.1", "127.1.2.3", "::1", "[::1]", "LOCALHOST"} {
		if !IsLoopback(h) {
			t.Errorf("%s: want loopback", h)
		}
	}
	// 앞머리만 127인 이름은 DNS 리바인딩이 쓰는 꼴이다.
	for _, h := range []string{"127.evil.com", "127.0.0.1.nip.io", "localhost.evil.com", "0.0.0.0", "192.168.0.10", ""} {
		if IsLoopback(h) {
			t.Errorf("%s: want not loopback", h)
		}
	}
}

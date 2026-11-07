package deploy

import (
	"net"
	"testing"
)

func TestAutomaticPortAvoidsOccupiedDefault(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	busy := listener.Addr().(*net.TCPAddr).Port
	if _, err := SelectPort(busy, false); err == nil {
		t.Fatal("explicit occupied port accepted")
	}
	port, err := SelectPort(busy, true)
	if err != nil || port == busy || port < 1024 {
		t.Fatalf("automatic port=%d, error=%v", port, err)
	}
	listener.Close()
	port, err = SelectPort(busy, true)
	if err != nil || port != busy {
		t.Fatalf("free preferred port was not kept: %d, %v", port, err)
	}
}

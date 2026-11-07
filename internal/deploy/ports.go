package deploy

import (
	"fmt"
	"net"
	"strconv"
)

// SelectPort checks the preferred loopback port while planning. Startup checks
// it again because another application can bind it after the plan is saved.
func SelectPort(preferred int, automatic bool) (int, error) {
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(preferred)))
	if err != nil && automatic {
		listener, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		return 0, fmt.Errorf("local port %d is busy or unavailable; choose another port or use 0 for automatic selection", preferred)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

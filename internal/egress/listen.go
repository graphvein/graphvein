package egress

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strconv"
)

// ListenLoopback opens a TCP listener on a loopback IP literal such as
// 127.0.0.1:0 or [::1]:8080. Names, including localhost, and empty hosts are
// rejected because they could resolve to or bind on a non-loopback
// interface; serving beyond this machine needs its own configuration.
func ListenLoopback(ctx context.Context, address string) (net.Listener, error) {
	host, portText, err := net.SplitHostPort(address)
	if err == nil {
		_, err = strconv.ParseUint(portText, 10, 16)
	}
	if err == nil {
		addr, perr := netip.ParseAddr(host)
		switch {
		case perr != nil:
			err = fmt.Errorf("host %q is not an IP literal; use 127.0.0.1 or [::1]", host)
		case !addr.Unmap().IsLoopback():
			err = fmt.Errorf("%s is not a loopback address; use 127.0.0.1 or [::1]", host)
		}
	}
	if err != nil {
		report(Event{Op: "listen", Network: "tcp", Address: address})
		return nil, fmt.Errorf("%w: listen %s: %v", ErrBlocked, address, err)
	}
	report(Event{Op: "listen", Network: "tcp", Address: address, Allowed: true})
	var lc net.ListenConfig
	l, err := lc.Listen(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("listening on %s: %w", address, err)
	}
	return l, nil
}

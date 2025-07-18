// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lsprpc

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"time"

	"cuelang.org/go/internal/golangorgx/tools/event"
)

// autoNetwork is the pseudo network type used to signal that cuelsp should use
// automatic discovery to resolve a remote address.
const autoNetwork = "auto"

// An autoDialer is a jsonrpc2 dialer that understands the 'auto' network.
type autoDialer struct {
	network, addr string // the 'real' network and address
	isAuto        bool   // whether the server is on the 'auto' network

	executable string
	argFunc    func(network, addr string) []string
}

func newAutoDialer(rawAddr string, argFunc func(network, addr string) []string) (*autoDialer, error) {
	d := autoDialer{
		argFunc: argFunc,
	}
	d.network, d.addr = ParseAddr(rawAddr)
	if d.network == autoNetwork {
		d.isAuto = true
		cuePath, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("getting executable: %w", err)
		}
		d.executable = cuePath
		d.network, d.addr = autoNetworkAddress(cuePath, d.addr)
	}
	return &d, nil
}

// Dial implements the jsonrpc2.Dialer interface.
func (d *autoDialer) Dial(ctx context.Context) (io.ReadWriteCloser, error) {
	conn, err := d.dialNet(ctx)
	return conn, err
}

// TODO(rFindley): remove this once we no longer need to integrate with v1 of
// the jsonrpc2 package.
func (d *autoDialer) dialNet(ctx context.Context) (net.Conn, error) {
	// Attempt to verify that we own the remote. This is imperfect, but if we can
	// determine that the remote is owned by a different user, we should fail.
	ok, err := verifyRemoteOwnership(d.network, d.addr)
	if err != nil {
		// If the ownership check itself failed, we fail open but log an error to
		// the user.
		event.Error(ctx, "unable to check daemon socket owner, failing open", err)
	} else if !ok {
		// We successfully checked that the socket is not owned by us, we fail
		// closed.
		return nil, fmt.Errorf("socket %q is owned by a different user", d.addr)
	}
	const dialTimeout = 1 * time.Second
	// Try dialing our remote once, in case it is already running.
	netConn, err := net.DialTimeout(d.network, d.addr, dialTimeout)
	if err == nil {
		return netConn, nil
	}
	if d.isAuto && d.argFunc != nil {
		if d.network == "unix" {
			// Sometimes the socketfile isn't properly cleaned up when the server
			// shuts down. Since we have already tried and failed to dial this
			// address, it should *usually* be safe to remove the socket before
			// binding to the address.
			// TODO(rfindley): there is probably a race here if multiple server
			// instances are simultaneously starting up.
			if _, err := os.Stat(d.addr); err == nil {
				if err := os.Remove(d.addr); err != nil {
					return nil, fmt.Errorf("removing remote socket file: %w", err)
				}
			}
		}
		args := d.argFunc(d.network, d.addr)
		cmd := exec.Command(d.executable, args...)
		if err := runRemote(cmd); err != nil {
			return nil, err
		}
	}

	const retries = 5
	// It can take some time for the newly started server to bind to our address,
	// so we retry for a bit.
	for retry := 0; retry < retries; retry++ {
		startDial := time.Now()
		netConn, err = net.DialTimeout(d.network, d.addr, dialTimeout)
		if err == nil {
			return netConn, nil
		}
		event.Log(ctx, fmt.Sprintf("failed attempt #%d to connect to remote: %v\n", retry+2, err))
		// In case our failure was a fast-failure, ensure we wait at least
		// f.dialTimeout before trying again.
		if retry != retries-1 {
			time.Sleep(dialTimeout - time.Since(startDial))
		}
	}
	return nil, fmt.Errorf("dialing remote: %w", err)
}

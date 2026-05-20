// Copyright 2024 New Vector Ltd.
// Copyright 2017 Vector Creations Ltd
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package main

import (
	"context"
	"net"
	"net/url"

	"github.com/cretz/bine/tor"
	"github.com/eyedeekay/onramp"
	"github.com/sirupsen/logrus"
)

func start() (*tor.Tor, error) {
	if skip {
		return nil, nil
	}
	return tor.Start(context.Background(), nil)
}

func dialer() (*tor.Dialer, error) {
	if skip {
		return nil, nil
	}
	return t.Dialer(context.TODO(), nil)
}

var (
	t, terr        = start()
	tdialer, tderr = dialer()
)

// DialContext either dials a unix socket address, or connects to a remote address over Tor.
// Always uses Tor for network connections.
func DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if terr != nil {
		return nil, terr
	}
	if (tderr != nil) || (tdialer == nil) {
		return nil, tderr
	}
	if network == "unix" {
		return net.Dial(network, addr)
	}
	// convert the addr to a full URL
	url, err := url.Parse(addr)
	if err != nil {
		return nil, err
	}
	return tdialer.DialContext(ctx, network, url.Host)
}

// createTorListener creates a Tor onion service listener
func createTorListener() (net.Listener, error) {
	onion, err := onramp.NewOnion("dendrite-onion")
	if err != nil {
		logrus.WithError(err).Fatal("failed to create onion")
		return nil, err
	}

	listener, err := onion.ListenTLS()
	if err != nil {
		onion.Close() // nolint: errcheck
		return nil, err
	}

	return listener, nil
}

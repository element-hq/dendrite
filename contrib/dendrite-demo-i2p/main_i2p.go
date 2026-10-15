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
	"strings"

	"github.com/cretz/bine/tor"
	"github.com/eyedeekay/goSam"
	"github.com/eyedeekay/onramp"
	"github.com/sirupsen/logrus"
)

func client() (*goSam.Client, error) {
	if skip {
		return nil, nil
	}
	return goSam.NewClient(*samAddr)
}

var sam, samError = client()

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

// DialContext dials a network connection to an I2P server, a unix socket, or falls back to Tor.
// For .i2p addresses, uses I2P SAM. For unix sockets, uses direct dial.
// For other addresses, falls back to Tor.
func DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if samError != nil {
		return nil, samError
	}
	if network == "unix" {
		return net.Dial(network, addr)
	}

	// convert the addr to a full URL
	url, err := url.Parse(addr)
	if err != nil {
		return nil, err
	}

	// Use I2P for .i2p addresses
	if strings.HasSuffix(url.Host, ".i2p") {
		return sam.DialContext(ctx, network, addr)
	}

	// Fall back to Tor for other addresses
	if terr != nil {
		return nil, terr
	}
	if (tderr != nil) || (tdialer == nil) {
		return nil, tderr
	}
	return tdialer.DialContext(ctx, network, addr)
}

// createI2PListener creates an I2P garlic service listener
func createI2PListener(samAddr string) (net.Listener, error) {
	garlic, err := onramp.NewGarlic("dendrite", samAddr, onramp.OPT_HUGE)
	if err != nil {
		logrus.WithError(err).Error("failed to create garlic service")
		return nil, err
	}

	listener, err := garlic.ListenTLS()
	if err != nil {
		garlic.Close() // nolint: errcheck
		return nil, err
	}

	return listener, nil
}

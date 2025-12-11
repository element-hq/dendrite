// Copyright 2024 New Vector Ltd.
// Copyright 2017 Vector Creations Ltd
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package main

import (
	"context"
	"crypto/tls"
	"flag"
	"net/http"
	"os"
	"time"

	embedded "github.com/element-hq/dendrite/contrib/dendrite-demo-embedded"
	"github.com/element-hq/dendrite/setup"
	basepkg "github.com/element-hq/dendrite/setup/base"
	"github.com/element-hq/dendrite/setup/config"
	"github.com/getsentry/sentry-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"

	"github.com/element-hq/dendrite/internal"
)

var (
	samAddr = flag.String("samaddr", "127.0.0.1:7656", "Address to connect to the I2P SAMv3 API")
	_, skip = os.LookupEnv("CI")
)

func main() {
	cfg := setup.ParseFlags(true)
	if skip {
		return
	}
	configErrors := &config.ConfigErrors{}
	cfg.Verify(configErrors)
	if len(*configErrors) > 0 {
		for _, err := range *configErrors {
			logrus.Errorf("Configuration error: %s", err)
		}
		logrus.Fatalf("Failed to start due to configuration errors")
	}

	basepkg.PlatformSanityChecks()

	// Setup Sentry if enabled
	if cfg.Global.Sentry.Enabled {
		logrus.Info("Setting up Sentry for debugging...")
		err := sentry.Init(sentry.ClientOptions{
			Dsn:              cfg.Global.Sentry.DSN,
			Environment:      cfg.Global.Sentry.Environment,
			Debug:            true,
			ServerName:       string(cfg.Global.ServerName),
			Release:          "dendrite@" + internal.VersionString(),
			AttachStacktrace: true,
		})
		if err != nil {
			logrus.WithError(err).Panic("failed to start Sentry")
		}
		defer func() {
			if !sentry.Flush(time.Second * 5) {
				logrus.Warnf("failed to flush all Sentry events!")
			}
		}()
	}

	// Create HTTP client that uses I2P for .i2p addresses and Tor for others
	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: DialContext,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	// Create embedded server configuration using existing Dendrite config
	serverConfig := embedded.ServerConfig{
		RawDendriteConfig: cfg,
		HTTPClient:        httpClient,
	}

	// Create the embedded server
	server, err := embedded.NewServer(serverConfig)
	if err != nil {
		logrus.WithError(err).Fatal("Failed to create embedded server")
	}

	// Create I2P garlic listener
	listener, err := createI2PListener(*samAddr)
	if err != nil {
		logrus.WithError(err).Fatal("Failed to create I2P listener")
	}
	defer listener.Close() // nolint: errcheck

	logrus.Infof("I2P garlic service address: %s", listener.Addr().String())

	// Register Prometheus metrics
	upCounter := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "dendrite",
		Name:      "up",
		ConstLabels: map[string]string{
			"version": internal.VersionString(),
		},
	})
	upCounter.Add(1)
	prometheus.MustRegister(upCounter)

	// Start the embedded server on the I2P listener
	if err := server.Start(context.Background(), listener); err != nil {
		logrus.WithError(err).Fatal("Failed to start embedded server")
	}

	// Wait for shutdown signal
	basepkg.WaitForShutdown(server.GetProcessContext())

	// Stop the server gracefully
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := server.Stop(shutdownCtx); err != nil {
		logrus.WithError(err).Error("Error during server shutdown")
	}
}

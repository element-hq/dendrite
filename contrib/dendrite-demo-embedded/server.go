package embedded

import (
	"context"
	"net"
	"net/http"
	"sync"

	"github.com/element-hq/dendrite/internal"
	"github.com/element-hq/dendrite/internal/caching"
	"github.com/element-hq/dendrite/internal/httputil"
	"github.com/element-hq/dendrite/internal/sqlutil"
	"github.com/element-hq/dendrite/setup/base"
	"github.com/element-hq/dendrite/setup/jetstream"
	"github.com/element-hq/dendrite/setup/process"
	"github.com/gorilla/mux"
	"github.com/matrix-org/gomatrixserverlib/fclient"
	"github.com/sirupsen/logrus"

	"github.com/element-hq/dendrite/appservice"
	"github.com/element-hq/dendrite/federationapi"
	"github.com/element-hq/dendrite/roomserver"
	"github.com/element-hq/dendrite/setup"
	"github.com/element-hq/dendrite/setup/config"
	"github.com/element-hq/dendrite/setup/mscs"
	"github.com/element-hq/dendrite/userapi"
)

// Server represents an embedded Matrix homeserver
type Server struct {
	processCtx   *process.ProcessContext
	cfg          *config.Dendrite
	httpServer   *http.Server
	natsInstance *jetstream.NATSInstance
	monolith     *setup.Monolith
	serverMutex  sync.Mutex
	running      bool
}

// NewServer creates a new embedded Matrix homeserver
func NewServer(config ServerConfig) (*Server, error) {
	// Convert to dendrite config
	dendriteConfig, err := config.toDendriteConfig()
	if err != nil {
		return nil, err
	}

	// Create process context
	processCtx := process.NewProcessContext()

	// Set up basic logging configuration
	internal.SetupStdLogging()
	internal.SetupHookLogging(dendriteConfig.Logging)
	internal.SetupPprof()

	// Display version info
	logrus.Infof("Dendrite version %s", internal.VersionString())
	if !dendriteConfig.ClientAPI.RegistrationDisabled &&
		dendriteConfig.ClientAPI.OpenRegistrationWithoutVerificationEnabled {
		logrus.Warn("Open registration is enabled")
	}

	// Create embedded server
	server := &Server{
		processCtx:   processCtx,
		cfg:          dendriteConfig,
		running:      false,
		natsInstance: &jetstream.NATSInstance{},
	}

	return server, nil
}

// Start initialises and starts the embedded server on the provided listener
func (s *Server) Start(ctx context.Context, listener net.Listener) error {
	s.serverMutex.Lock()
	defer s.serverMutex.Unlock()

	if s.running {
		return nil
	}

	// Create DNS cache if enabled
	var dnsCache *fclient.DNSCache
	if s.cfg.Global.DNSCache.Enabled {
		dnsCache = fclient.NewDNSCache(
			s.cfg.Global.DNSCache.CacheSize,
			s.cfg.Global.DNSCache.CacheLifetime,
			s.cfg.FederationAPI.AllowNetworkCIDRs,
			s.cfg.FederationAPI.DenyNetworkCIDRs,
		)
		logrus.Infof(
			"DNS cache enabled (size %d, lifetime %s)",
			s.cfg.Global.DNSCache.CacheSize,
			s.cfg.Global.DNSCache.CacheLifetime,
		)
	}

	// Set up tracing
	closer, err := s.cfg.SetupTracing()
	if err != nil {
		logrus.WithError(err).Panicf("failed to start opentracing")
	}
	defer closer.Close() // nolint: errcheck

	// Create HTTP clients
	federationClient := base.CreateFederationClient(s.cfg, dnsCache)
	httpClient := base.CreateClient(s.cfg, dnsCache)

	// Set up connection manager and component APIs
	cm := sqlutil.NewConnectionManager(s.processCtx, s.cfg.Global.DatabaseOptions)
	routers := httputil.NewRouters()
	caches := caching.NewRistrettoCache(s.cfg.Global.Cache.EstimatedMaxSize, s.cfg.Global.Cache.MaxAge, caching.EnableMetrics)

	// Create room server API
	rsAPI := roomserver.NewInternalAPI(s.processCtx, s.cfg, cm, s.natsInstance, caches, caching.EnableMetrics)

	// Create federation API
	fsAPI := federationapi.NewInternalAPI(
		s.processCtx, s.cfg, cm, s.natsInstance, federationClient, rsAPI, caches, nil, false,
	)

	// Get KeyRing
	keyRing := fsAPI.KeyRing()

	// Link APIs together
	rsAPI.SetFederationAPI(fsAPI, keyRing)

	// Create user and appservice APIs
	userAPI := userapi.NewInternalAPI(s.processCtx, s.cfg, cm, s.natsInstance, rsAPI, federationClient, caching.EnableMetrics, fsAPI.IsBlacklistedOrBackingOff)
	asAPI := appservice.NewInternalAPI(s.processCtx, s.cfg, s.natsInstance, userAPI, rsAPI)

	// Set necessary dependencies
	rsAPI.SetAppserviceAPI(asAPI)
	rsAPI.SetUserAPI(userAPI)

	// Initialise monolith
	s.monolith = &setup.Monolith{
		Config:    s.cfg,
		Client:    httpClient,
		FedClient: federationClient,
		KeyRing:   keyRing,

		AppserviceAPI: asAPI,
		FederationAPI: fsAPI,
		RoomserverAPI: rsAPI,
		UserAPI:       userAPI,
	}
	s.monolith.AddAllPublicRoutes(s.processCtx, s.cfg, routers, cm, s.natsInstance, caches, caching.EnableMetrics)

	// Enable MSCs if configured
	if len(s.cfg.MSCs.MSCs) > 0 {
		if err := mscs.Enable(s.cfg, cm, routers, s.monolith, caches); err != nil {
			return err
		}
	}

	// Configure admin endpoints
	base.ConfigureAdminEndpoints(s.processCtx, routers)

	// Set up external router and server handlers
	externalRouter := mux.NewRouter().SkipClean(true).UseEncodedPath()

	// Expose the matrix APIs directly rather than putting them under a /api path
	externalRouter.PathPrefix(httputil.DendriteAdminPathPrefix).Handler(routers.DendriteAdmin)
	externalRouter.PathPrefix(httputil.PublicClientPathPrefix).Handler(routers.Client)

	if !s.cfg.Global.DisableFederation {
		externalRouter.PathPrefix(httputil.PublicKeyPathPrefix).Handler(routers.Keys)
		externalRouter.PathPrefix(httputil.PublicFederationPathPrefix).Handler(routers.Federation)
	}

	externalRouter.PathPrefix(httputil.SynapseAdminPathPrefix).Handler(routers.SynapseAdmin)
	externalRouter.PathPrefix(httputil.PublicMediaPathPrefix).Handler(routers.Media)
	externalRouter.PathPrefix(httputil.PublicWellKnownPrefix).Handler(routers.WellKnown)
	externalRouter.PathPrefix(httputil.PublicStaticPath).Handler(routers.Static)

	// Set up not found and method not allowed handlers
	externalRouter.NotFoundHandler = httputil.NotFoundCORSHandler
	externalRouter.MethodNotAllowedHandler = httputil.NotAllowedHandler

	// Create HTTP server
	s.httpServer = &http.Server{
		Addr:         listener.Addr().String(),
		WriteTimeout: base.HTTPServerTimeout,
		Handler:      externalRouter,
		BaseContext: func(_ net.Listener) context.Context {
			return s.processCtx.Context()
		},
	}

	// Start HTTP server
	go func() {
		logrus.Infof("Starting embedded Matrix server on %s", listener.Addr().String())
		s.processCtx.ComponentStarted()

		if err := s.httpServer.Serve(listener); err != nil {
			if err != http.ErrServerClosed {
				logrus.WithError(err).Error("Failed to serve HTTP")
			}
		}

		logrus.Info("HTTP server stopped")
		s.processCtx.ComponentFinished()
	}()

	s.running = true
	return nil
}

// Stop gracefully stops the embedded server
func (s *Server) Stop(ctx context.Context) error {
	s.serverMutex.Lock()
	defer s.serverMutex.Unlock()

	if !s.running {
		return nil
	}

	// Signal shutdown to process context
	s.processCtx.ShutdownDendrite()

	// Wait for shutdown to complete
	<-s.processCtx.WaitForShutdown()
	return s.httpServer.Shutdown(ctx)
}

// GetProcessContext returns the internal process context
func (s *Server) GetProcessContext() *process.ProcessContext {
	return s.processCtx
}

// GetConfig returns the Dendrite configuration
func (s *Server) GetConfig() *config.Dendrite {
	return s.cfg
}

// GetMonolith returns the internal monolith instance
func (s *Server) GetMonolith() *setup.Monolith {
	return s.monolith
}

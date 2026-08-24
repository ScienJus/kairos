package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/artifactstore"
	"github.com/ScienJus/kairos/internal/httpapi"
	"github.com/ScienJus/kairos/internal/identity"
	"github.com/ScienJus/kairos/internal/mcpapi"
	"github.com/ScienJus/kairos/internal/repository"
	webui "github.com/ScienJus/kairos/web"
)

var version = "dev"

func main() {
	handled, err := handleCommand(os.Args[1:], os.Stdout)
	if err != nil {
		log.Fatal(err)
	}
	if handled {
		return
	}
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func handleCommand(args []string, output io.Writer) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	if len(args) == 1 && args[0] == "--version" {
		_, err := fmt.Fprintf(output, "kairos-server %s\n", version)
		return true, err
	}
	return true, errors.New("usage: kairos-server [--version]")
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	repo, err := openConfiguredRepository(ctx)
	if err != nil {
		return err
	}
	defer repo.Close()

	service, err := application.NewService(repo, systemClock{}, randomIDs{})
	if err != nil {
		return err
	}
	localArtifacts, err := artifactstore.NewLocal(environment("KAIROS_ARTIFACT_DIR", "artifacts"))
	if err != nil {
		return err
	}
	if err := service.ConfigureArtifactStore(localArtifacts); err != nil {
		return err
	}
	claimLease, err := time.ParseDuration(environment("KAIROS_AGENT_CLAIM_LEASE", "5m"))
	if err != nil {
		return fmt.Errorf("parse KAIROS_AGENT_CLAIM_LEASE: %w", err)
	}
	if err := service.SetClaimLeaseDuration(claimLease); err != nil {
		return err
	}
	stopReaper := service.StartLeaseReaper(ctx, 15*time.Second)
	defer stopReaper()
	artifactGCRetention, err := time.ParseDuration(environment("KAIROS_ARTIFACT_GC_RETENTION", application.DefaultArtifactGCRetention.String()))
	if err != nil {
		return fmt.Errorf("parse KAIROS_ARTIFACT_GC_RETENTION: %w", err)
	}
	artifactGCInterval, err := time.ParseDuration(environment("KAIROS_ARTIFACT_GC_INTERVAL", application.DefaultArtifactGCInterval.String()))
	if err != nil {
		return fmt.Errorf("parse KAIROS_ARTIFACT_GC_INTERVAL: %w", err)
	}
	stopArtifactGC, err := service.StartArtifactGarbageCollector(ctx, artifactGCRetention, artifactGCInterval)
	if err != nil {
		return err
	}
	defer stopArtifactGC()
	maxArtifactUploadBytes, err := strconv.ParseInt(environment("KAIROS_ARTIFACT_MAX_UPLOAD_BYTES", strconv.FormatInt(httpapi.DefaultMaxArtifactUploadBytes, 10)), 10, 64)
	if err != nil || maxArtifactUploadBytes <= 0 {
		return fmt.Errorf("KAIROS_ARTIFACT_MAX_UPLOAD_BYTES must be a positive integer")
	}
	identityService, err := identity.NewService(repo, systemClock{}, identity.SecureTokenGenerator{})
	if err != nil {
		return err
	}
	authMode := environment("KAIROS_AUTH_MODE", "trusted")
	adminToken := os.Getenv("KAIROS_ADMIN_TOKEN")
	var resolver identity.Resolver
	switch authMode {
	case "trusted":
		resolver = identity.TrustedResolver{}
	case "authenticated":
		if adminToken == "" {
			return errors.New("KAIROS_ADMIN_TOKEN is required in authenticated mode")
		}
		resolver = identity.AuthenticatedResolver{Authenticator: identityService}
	default:
		return fmt.Errorf("unsupported KAIROS_AUTH_MODE %q", authMode)
	}
	httpOptions := httpapi.Options{
		MaxArtifactUploadBytes: maxArtifactUploadBytes,
		AuthenticationMode:     httpapi.AuthenticationMode(authMode),
	}

	var apiHandler *httpapi.Handler
	if adminToken != "" {
		apiHandler, err = httpapi.NewWithIdentityManagement(service, resolver, identityService, adminToken, httpOptions)
	} else {
		apiHandler, err = httpapi.New(service, resolver, httpOptions)
	}
	if err != nil {
		return err
	}
	mcpHandler, err := mcpapi.New(service, resolver, mcpapi.Options{MaxArtifactUploadBytes: maxArtifactUploadBytes})
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpHandler)
	mux.Handle("/api/", apiHandler)
	mux.Handle("/healthz", apiHandler)
	mux.Handle("/", webui.Handler())

	server := &http.Server{
		Addr:              environment("KAIROS_LISTEN_ADDR", "127.0.0.1:8080"),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("Kairos listening on http://%s in %s mode", server.Addr, authMode)
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownContext)
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func environment(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

type databaseBackend string

const (
	databaseBackendSQLite   databaseBackend = "sqlite"
	databaseBackendPostgres databaseBackend = "postgres"
)

type databaseConfiguration struct {
	backend  databaseBackend
	location string
}

func databaseConfigurationFromEnvironment() databaseConfiguration {
	if dsn := os.Getenv("KAIROS_POSTGRES_DSN"); dsn != "" {
		return databaseConfiguration{backend: databaseBackendPostgres, location: dsn}
	}
	return databaseConfiguration{
		backend:  databaseBackendSQLite,
		location: environment("KAIROS_SQLITE_PATH", "kairos.db"),
	}
}

func openConfiguredRepository(ctx context.Context) (*repository.SQLRepository, error) {
	configuration := databaseConfigurationFromEnvironment()
	if configuration.backend == databaseBackendPostgres {
		return repository.OpenPostgres(ctx, configuration.location)
	}
	return repository.OpenSQLite(ctx, configuration.location)
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

type randomIDs struct{}

func (randomIDs) NewID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(value[:])
}

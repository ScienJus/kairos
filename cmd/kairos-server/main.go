package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/httpapi"
	"github.com/ScienJus/kairos/internal/identity"
	"github.com/ScienJus/kairos/internal/mcpapi"
	"github.com/ScienJus/kairos/internal/repository"
	webui "github.com/ScienJus/kairos/web"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	databasePath := environment("KAIROS_SQLITE_PATH", "kairos.db")
	repo, err := repository.OpenSQLite(ctx, databasePath)
	if err != nil {
		return err
	}
	defer repo.Close()

	service, err := application.NewService(repo, systemClock{}, randomIDs{})
	if err != nil {
		return err
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

	var apiHandler http.Handler
	if adminToken != "" {
		apiHandler, err = httpapi.NewWithIdentityManagement(service, resolver, identityService, adminToken)
	} else {
		apiHandler, err = httpapi.New(service, resolver)
	}
	if err != nil {
		return err
	}
	mcpHandler, err := mcpapi.New(service, resolver)
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

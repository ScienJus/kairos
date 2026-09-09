package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ScienJus/kairos/internal/domain"
	"github.com/ScienJus/kairos/internal/identity"
)

type adminClock struct{}

func (adminClock) Now() time.Time { return repositoryTestTime }

func TestAdminIdentitySQLContract(t *testing.T) {
	forEachSQLRepository(t, func(t *testing.T, repo *SQLRepository, openPeer func(*testing.T) *SQLRepository) {
		ctx := context.Background()
		// This contract owns the complete identity fixture on the disposable database.
		if _, err := repo.db.ExecContext(ctx, "DELETE FROM identities"); err != nil {
			t.Fatal(err)
		}
		newService := func(r *SQLRepository) *identity.Service {
			s, err := identity.NewService(r, adminClock{}, identity.SecureTokenGenerator{})
			if err != nil {
				t.Fatal(err)
			}
			return s
		}
		token := func() string {
			v, err := (identity.SecureTokenGenerator{}).NewToken()
			if err != nil {
				t.Fatal(err)
			}
			return v
		}
		s := newService(repo)
		existing, err := s.CreateIdentity(ctx, domain.ActorRef{Kind: domain.ActorHuman, ID: "admin-collision"}, "")
		if err != nil {
			t.Fatal(err)
		}
		agent, err := s.CreateIdentity(ctx, domain.ActorRef{Kind: domain.ActorAgent, ID: "admin-collision"}, "worker")
		if err != nil {
			t.Fatal(err)
		}
		candidate := identity.StoredIdentity{Identity: identity.Identity{Actor: existing.Identity.Actor}, CredentialSource: "admin", CreatedAt: repositoryTestTime, UpdatedAt: repositoryTestTime}
		current := token()
		hash := sha256.Sum256([]byte(current))
		if _, err := repo.EnsureAdminIdentity(ctx, candidate, hex.EncodeToString(hash[:])); !errors.Is(err, identity.ErrConflict) {
			t.Fatalf("candidate collision = %v", err)
		}
		if err := s.ConfigureAdmin(ctx, existing.Token); !errors.Is(err, identity.ErrInvalid) || !strings.Contains(err.Error(), "conflicts with an existing Identity") {
			t.Fatalf("credential collision = %v", err)
		}
		records, _ := repo.ListIdentities(ctx)
		if len(records) != 2 {
			t.Fatal("failed initialization committed an identity")
		}
		peers := []*SQLRepository{repo, openPeer(t), openPeer(t), openPeer(t)}
		var wg sync.WaitGroup
		ids := make(chan identity.Identity, len(peers))
		failures := make(chan error, len(peers))
		for _, peer := range peers {
			wg.Add(1)
			go func(r *SQLRepository) {
				defer wg.Done()
				service, _ := identity.NewService(r, adminClock{}, identity.SecureTokenGenerator{})
				if err := service.ConfigureAdmin(ctx, current); err != nil {
					failures <- err
					return
				}
				got, err := service.Authenticate(ctx, current)
				if err != nil {
					failures <- err
					return
				}
				ids <- got
			}(peer)
		}
		wg.Wait()
		close(ids)
		close(failures)
		for err := range failures {
			t.Fatal(err)
		}
		var principal identity.Identity
		for got := range ids {
			if principal.Actor.ID != "" && got != principal {
				t.Fatal("concurrent initialization changed actor")
			}
			principal = got
		}
		if principal.Actor.Kind != domain.ActorHuman || principal.Role != "" || principal.Executor != nil || principal.Actor == existing.Identity.Actor {
			t.Fatal("unexpected Admin principal")
		}
		if err := s.ConfigureAdmin(ctx, current); err != nil {
			t.Fatal(err)
		}
		for _, old := range []identity.IssuedToken{existing, agent} {
			got, err := s.Authenticate(ctx, old.Token)
			if err != nil || got != old.Identity {
				t.Fatal("existing identity changed")
			}
		}
		record, err := s.GetIdentity(ctx, principal.Actor)
		if err != nil || record.CredentialSource != "admin" || record.TokenActive {
			t.Fatal("Admin record must not advertise an Identity token")
		}
		if _, err := s.RotateToken(ctx, principal.Actor); !errors.Is(err, identity.ErrForbidden) {
			t.Fatalf("rotate Admin = %v", err)
		}
		if err := s.RevokeToken(ctx, principal.Actor); !errors.Is(err, identity.ErrForbidden) {
			t.Fatalf("revoke Admin = %v", err)
		}
		stored, _ := repo.GetIdentity(ctx, principal.Actor)
		stored.Version++
		stored.TokenHash = strings.Repeat("a", 64)
		if err := repo.SaveIdentity(ctx, stored); !errors.Is(err, identity.ErrConflict) {
			t.Fatalf("direct Admin update = %v", err)
		}
		rotated := token()
		restarted := newService(openPeer(t))
		if err := restarted.ConfigureAdmin(ctx, rotated); err != nil {
			t.Fatal(err)
		}
		got, err := restarted.Authenticate(ctx, rotated)
		if err != nil || got != principal {
			t.Fatal("rotation/reopen changed Admin actor")
		}
		if _, err := restarted.Authenticate(ctx, current); !errors.Is(err, identity.ErrUnauthenticated) {
			t.Fatalf("old Admin token = %v", err)
		}
		for _, invalid := range []string{"", "invalid"} {
			if _, err := restarted.Authenticate(ctx, invalid); !errors.Is(err, identity.ErrUnauthenticated) {
				t.Fatalf("invalid credential = %v", err)
			}
		}
		records, err = repo.ListIdentities(ctx)
		if err != nil || len(records) != 3 {
			t.Fatal("unexpected identity count")
		}
		for _, record := range records {
			if record.TokenHash == hex.EncodeToString(hash[:]) {
				t.Fatal("Admin credential persisted")
			}
		}
		// Database constraints reject corruption of the singleton's Human semantics.
		if _, err := repo.db.ExecContext(ctx, "UPDATE identities SET role = 'admin' WHERE credential_source = 'admin'"); err == nil {
			t.Fatal("corrupt Admin role accepted")
		}
		if _, err := repo.db.ExecContext(ctx, "UPDATE identities SET actor_kind = 'agent' WHERE credential_source = 'admin'"); err == nil {
			t.Fatal("corrupt Admin kind accepted")
		}
	})
}

func TestAdminMigrationPreservesExistingIdentities(t *testing.T) {
	forEachSQLRepository(t, func(t *testing.T, repo *SQLRepository, _ func(*testing.T) *SQLRepository) {
		ctx := context.Background()
		// Reconstruct the deployed 004 schema, with a real identity and credential.
		for _, statement := range []string{"DELETE FROM identities", "DROP INDEX identities_admin_source_idx", "ALTER TABLE identities DROP COLUMN credential_source", "DELETE FROM schema_migrations WHERE version = '005_admin_identity'"} {
			if _, err := repo.db.ExecContext(ctx, statement); err != nil {
				t.Fatal(err)
			}
		}
		token, err := (identity.SecureTokenGenerator{}).NewToken()
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256([]byte(token))
		actor := domain.ActorRef{Kind: domain.ActorHuman, ID: "before-upgrade"}
		old := identity.StoredIdentity{Identity: identity.Identity{Actor: actor}, TokenHash: hex.EncodeToString(hash[:]), CreatedAt: repositoryTestTime, UpdatedAt: repositoryTestTime}
		if err := repo.CreateIdentity(ctx, old); err != nil {
			t.Fatal(err)
		}
		if err := repo.migrate(ctx); err != nil {
			t.Fatal(err)
		}
		if err := repo.migrate(ctx); err != nil {
			t.Fatal(err)
		}
		got, err := repo.GetIdentity(ctx, actor)
		if err != nil || got.Identity != old.Identity || got.TokenHash != old.TokenHash || got.CredentialSource != "identity" || got.Version != old.Version || !got.CreatedAt.Equal(old.CreatedAt) {
			t.Fatal("migration changed existing identity")
		}
		s, _ := identity.NewService(repo, adminClock{}, identity.SecureTokenGenerator{})
		current, err := (identity.SecureTokenGenerator{}).NewToken()
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ConfigureAdmin(ctx, current); err != nil {
			t.Fatal(err)
		}
		if authenticated, err := s.Authenticate(ctx, token); err != nil || authenticated.Actor != actor {
			t.Fatal("upgrade invalidated ordinary token")
		}
	})
}

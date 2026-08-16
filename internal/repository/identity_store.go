package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/domain"
	"github.com/ScienJus/kairos/internal/identity"
)

var _ identity.Repository = (*SQLRepository)(nil)

// CreateIdentity stores one managed identity and credential hash.
func (r *SQLRepository) CreateIdentity(ctx context.Context, value identity.StoredIdentity) error {
	return translateIdentityError(r.withWriteTransaction(ctx, func(store *sqlStore) error {
		_, err := store.exec(`
			INSERT INTO identities
				(actor_kind, actor_id, role, token_hash, version, created_at_ns, updated_at_ns)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			value.Identity.Actor.Kind, value.Identity.Actor.ID, value.Identity.Role,
			nullTokenHash(value.TokenHash), value.Version, value.CreatedAt.UnixNano(), value.UpdatedAt.UnixNano(),
		)
		return err
	}))
}

// SaveIdentity updates one identity using optimistic version matching.
func (r *SQLRepository) SaveIdentity(ctx context.Context, value identity.StoredIdentity) error {
	if value.Version <= 0 {
		return fmt.Errorf("%w: identity %q has no previous version", identity.ErrConflict, value.Identity.Actor.ID)
	}
	return translateIdentityError(r.withWriteTransaction(ctx, func(store *sqlStore) error {
		result, err := store.exec(`
			UPDATE identities
			SET role = ?, token_hash = ?, version = ?, updated_at_ns = ?
			WHERE actor_kind = ? AND actor_id = ? AND version = ?`,
			value.Identity.Role, nullTokenHash(value.TokenHash), value.Version, value.UpdatedAt.UnixNano(),
			value.Identity.Actor.Kind, value.Identity.Actor.ID, value.Version-1,
		)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count == 1 {
			return nil
		}
		var exists bool
		if err := store.queryRow(
			"SELECT EXISTS (SELECT 1 FROM identities WHERE actor_kind = ? AND actor_id = ?)",
			value.Identity.Actor.Kind, value.Identity.Actor.ID,
		).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return identity.ErrNotFound
		}
		return identity.ErrConflict
	}))
}

// GetIdentity loads one managed identity by stable actor reference.
func (r *SQLRepository) GetIdentity(ctx context.Context, actor domain.ActorRef) (identity.StoredIdentity, error) {
	row := r.db.QueryRowContext(ctx, rebind(r.dialect, `
		SELECT role, token_hash, version, created_at_ns, updated_at_ns
		FROM identities
		WHERE actor_kind = ? AND actor_id = ?`), actor.Kind, actor.ID)
	value, err := scanIdentity(row, actor)
	return value, translateIdentityError(err)
}

// GetIdentityByTokenHash resolves one active token hash.
func (r *SQLRepository) GetIdentityByTokenHash(ctx context.Context, tokenHash string) (identity.StoredIdentity, error) {
	var actorKind domain.ActorKind
	var actorID domain.ActorID
	row := r.db.QueryRowContext(ctx, rebind(r.dialect, `
		SELECT actor_kind, actor_id, role, token_hash, version, created_at_ns, updated_at_ns
		FROM identities
		WHERE token_hash = ?`), tokenHash)
	var role string
	var storedHash sql.NullString
	var version, createdAtNS, updatedAtNS int64
	if err := row.Scan(&actorKind, &actorID, &role, &storedHash, &version, &createdAtNS, &updatedAtNS); err != nil {
		return identity.StoredIdentity{}, translateIdentityError(normalizeError(err))
	}
	return identity.StoredIdentity{
		Identity:  identity.Identity{Actor: domain.ActorRef{Kind: actorKind, ID: actorID}, Role: role},
		TokenHash: storedHash.String, Version: version,
		CreatedAt: time.Unix(0, createdAtNS).UTC(), UpdatedAt: time.Unix(0, updatedAtNS).UTC(),
	}, nil
}

// ListIdentities returns identities ordered by kind and stable ID.
func (r *SQLRepository) ListIdentities(ctx context.Context) ([]identity.StoredIdentity, error) {
	rows, err := r.db.QueryContext(ctx, rebind(r.dialect, `
		SELECT actor_kind, actor_id, role, token_hash, version, created_at_ns, updated_at_ns
		FROM identities
		ORDER BY actor_kind, actor_id`))
	if err != nil {
		return nil, translateIdentityError(normalizeError(err))
	}
	defer rows.Close()
	result := make([]identity.StoredIdentity, 0)
	for rows.Next() {
		var actorKind domain.ActorKind
		var actorID domain.ActorID
		var role string
		var tokenHash sql.NullString
		var version, createdAtNS, updatedAtNS int64
		if err := rows.Scan(&actorKind, &actorID, &role, &tokenHash, &version, &createdAtNS, &updatedAtNS); err != nil {
			return nil, translateIdentityError(normalizeError(err))
		}
		result = append(result, identity.StoredIdentity{
			Identity:  identity.Identity{Actor: domain.ActorRef{Kind: actorKind, ID: actorID}, Role: role},
			TokenHash: tokenHash.String, Version: version,
			CreatedAt: time.Unix(0, createdAtNS).UTC(), UpdatedAt: time.Unix(0, updatedAtNS).UTC(),
		})
	}
	return result, translateIdentityError(normalizeError(rows.Err()))
}

type identityScanner interface {
	Scan(...any) error
}

func scanIdentity(row identityScanner, actor domain.ActorRef) (identity.StoredIdentity, error) {
	var role string
	var tokenHash sql.NullString
	var version, createdAtNS, updatedAtNS int64
	if err := row.Scan(&role, &tokenHash, &version, &createdAtNS, &updatedAtNS); err != nil {
		return identity.StoredIdentity{}, normalizeError(err)
	}
	return identity.StoredIdentity{
		Identity: identity.Identity{Actor: actor, Role: role}, TokenHash: tokenHash.String, Version: version,
		CreatedAt: time.Unix(0, createdAtNS).UTC(), UpdatedAt: time.Unix(0, updatedAtNS).UTC(),
	}, nil
}

func translateIdentityError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, identity.ErrNotFound), errors.Is(err, identity.ErrConflict):
		return err
	case errors.Is(err, application.ErrNotFound):
		return fmt.Errorf("%w: %v", identity.ErrNotFound, err)
	case errors.Is(err, application.ErrConflict):
		return fmt.Errorf("%w: %v", identity.ErrConflict, err)
	default:
		return err
	}
}

func nullTokenHash(value string) any {
	if value == "" {
		return nil
	}
	return value
}

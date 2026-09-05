package repository

import (
	"github.com/ScienJus/kairos/internal/domain"
	"github.com/ScienJus/kairos/internal/identity"
)

// FindExecutorPrincipals includes ended Claims so an ambiguous credential always fails closed.
func (s *sqlStore) FindExecutorPrincipals(hash string) ([]identity.Principal, error) {
	rows, err := s.query(`
		SELECT 'task_executor', c.id, c.task_id, t.work_item_id, '', c.executor_kind, c.executor_id
		FROM claims c JOIN tasks t ON t.id = c.task_id WHERE c.executor_token_hash = ?
		UNION ALL
		SELECT 'coordination_executor', id, '', work_item_id, kind, executor_kind, executor_id
		FROM coordination_claims WHERE executor_token_hash = ?
		LIMIT 2`, hash, hash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []identity.Principal
	for rows.Next() {
		var p identity.Principal
		e := &identity.ExecutorScope{TokenHash: hash}
		var actorKind string
		if err := rows.Scan(&e.Profile, &e.ClaimID, &e.TaskID, &e.WorkItemID, &e.CandidateKind, &actorKind, &p.Actor.ID); err != nil {
			return nil, normalizeError(err)
		}
		p.Actor.Kind = domain.ActorKind(actorKind)
		p.Executor = e
		result = append(result, p)
	}
	return result, normalizeError(rows.Err())
}

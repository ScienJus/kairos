package domain

import (
	"encoding/hex"
	"strings"
)

func validateExecutorTokenHash(hash string, actor ActorRef) error {
	if hash == "" {
		return nil
	}
	decoded, err := hex.DecodeString(hash)
	if err != nil || len(decoded) != 32 || hash != strings.ToLower(hash) || actor.Kind != ActorAgent {
		return invalid("executor_token_hash", "must be a SHA-256 hash on an Agent Claim")
	}
	return nil
}

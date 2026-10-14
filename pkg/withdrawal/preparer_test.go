package withdrawal

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

func TestSemanticGoldenVectors(t *testing.T) {
	raw, err := os.ReadFile("testdata/semantic-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []struct {
		Name          string
		Input         contracts.WithdrawRequest
		ActorBytes    string           `json:"actor_bytes"`
		CommandBytes  string           `json:"command_bytes"`
		ActorDigest   contracts.Digest `json:"actor_digest"`
		CommandDigest contracts.Digest `json:"command_digest"`
	}
	if json.Unmarshal(raw, &vectors) != nil || len(vectors) != 2 {
		t.Fatal("invalid vectors")
	}
	for _, v := range vectors {
		t.Run(v.Name, func(t *testing.T) {
			s, err := SemanticCommitments(v.Input)
			if err != nil || string(s.Actor) != v.ActorBytes || string(s.Command) != v.CommandBytes || s.ActorDigest != v.ActorDigest || s.CommandDigest != v.CommandDigest {
				t.Fatal("golden mismatch", err)
			}
			retry := cloneRequest(v.Input)
			retry.Command.Request.RequestID = "new-request"
			retry.Command.Scope.ScopeID = "new-scope"
			retry.Command.Scope.PolicyVersion = 99
			retry.Command.Request.Deadline = time.Now()
			retry.Command.Scope.ExpiresAt = time.Now()
			retry.Command.IdempotencyKey = "another-key"
			if retry.Command.Principal.Delegation != nil {
				retry.Command.Principal.Delegation.ProofReference = "renewed-proof"
			}
			next, err := SemanticCommitments(retry)
			if err != nil || next.ActorDigest != s.ActorDigest || next.CommandDigest != s.CommandDigest {
				t.Fatal("transient data changed semantics")
			}
			retry.Reason += " "
			next, err = SemanticCommitments(retry)
			if err != nil || next.CommandDigest == s.CommandDigest {
				t.Fatal("reason not bound exactly")
			}
			retry = cloneRequest(v.Input)
			retry.Command.Principal.Issuer = "other-issuer"
			next, err = SemanticCommitments(retry)
			if err != nil || next.ActorDigest == s.ActorDigest || next.CommandDigest == s.CommandDigest {
				t.Fatal("issuer not bound")
			}
		})
	}
}
func TestPreparerRequiresAuthorityBeforeGeneratingIDs(t *testing.T) {
	r, _, _ := fixture()
	r.Command.Principal.ClientID = "client"
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	ids := 0
	deny := errors.New("denied")
	p := Preparer{Verify: func(context.Context, contracts.WithdrawRequest) error { return deny }, NewID: func(context.Context) (contracts.ID, error) { ids++; return "id", nil }}
	if result, err := p.Prepare(ctx, r); !errors.Is(err, deny) || result != (Preparation{}) || ids != 0 {
		t.Fatal("authority denial allocated IDs", err)
	}
	p.Verify = nil
	if _, err := p.Prepare(ctx, r); err == nil || ids != 0 {
		t.Fatal("missing verifier allowed")
	}
	p.Verify = func(context.Context, contracts.WithdrawRequest) error { cancel(); return nil }
	if _, err := p.Prepare(ctx, r); err == nil || ids != 0 {
		t.Fatal("cancellation after verification ignored")
	}
}
func TestPreparerRejectsDuplicateIDsAndGeneratesIndependentIDs(t *testing.T) {
	r, _, _ := fixture()
	r.Command.Principal.ClientID = "client"
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	p := Preparer{Verify: func(_ context.Context, input contracts.WithdrawRequest) error {
		input.Command.Principal.Delegation.UserID = "mutated"
		return nil
	}, NewID: func(context.Context) (contracts.ID, error) { return "duplicate", nil }}
	if result, err := p.Prepare(ctx, r); err == nil || result != (Preparation{}) {
		t.Fatal("duplicate IDs accepted")
	}
	p.NewID = nil
	prepared, err := p.Prepare(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[contracts.ID]bool{}
	for _, id := range []contracts.ID{prepared.OperationID, prepared.HistoryID, prepared.PlanID, prepared.EventID, prepared.AuditID} {
		if id == "" || seen[id] {
			t.Fatal("invalid generated ID")
		}
		seen[id] = true
	}
	semantic, err := SemanticCommitments(r)
	if err != nil || prepared.ActorSemanticDigest != semantic.ActorDigest || prepared.CommandDigest != semantic.CommandDigest || r.Command.Principal.Delegation.UserID != "verified-user" {
		t.Fatal("authority mutated semantic input")
	}
}

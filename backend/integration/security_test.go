package integration

import (
	"testing"
	"time"
)

// TestTrainerTakeoverRejectedWithoutToken covers S1 at the integration
// level: a second WebSocket connection that sends trainer_join for an
// already-trainered session without the per-session token must be rejected.
// The legitimate trainer is NOT displaced.
func TestTrainerTakeoverRejectedWithoutToken(t *testing.T) {
	ts := NewTestServer(t)
	defer ts.Close(t)

	// 1. Legitimate trainer creates the session.
	trainer := NewWSClient(t, ts.WebSocketURL())
	defer trainer.Close()
	trainer.SendMessage(TrainerJoin("new").Build())
	created := trainer.WaitForType("session_created", 2*time.Second)
	sessionCode := created["sessionCode"].(string)
	if _, ok := created["trainerToken"].(string); !ok {
		t.Fatal("session_created must include trainerToken")
	}
	trainer.WaitForType("connected_count", 2*time.Second)

	// 2. Imposter attempts takeover without the token.
	imposter := NewWSClient(t, ts.WebSocketURL())
	defer imposter.Close()
	imposter.SendMessage(TrainerJoin(sessionCode).Build())

	errMsg := imposter.WaitForType("error", 2*time.Second)
	if errMsg["type"] != "error" {
		t.Fatalf("expected error for unauthenticated takeover, got %v", errMsg["type"])
	}

	// 3. The legitimate trainer can still drive the session: start a vote
	// and confirm stagiaires (joined below) receive the broadcast. This
	// proves the imposter did not steal the session.
	stagiaire := NewWSClient(t, ts.WebSocketURL())
	defer stagiaire.Close()
	stagiaire.SendMessage(StagiaireJoin(sessionCode, "", "Alice").Build())
	stagiaire.WaitForType("session_joined", 2*time.Second)
	trainer.WaitForType("connected_count", 2*time.Second)

	trainer.SendMessage(StartVote([]string{"rouge"}, false).Build())
	stagiaire.WaitForType("vote_started", 2*time.Second)
}

// TestTrainerTakeoverSucceedsWithToken verifies the flip side of S1: a
// reconnect presenting the minted token DOES take over the active trainer
// connection (the intended reconnect path).
func TestTrainerTakeoverSucceedsWithToken(t *testing.T) {
	ts := NewTestServer(t)
	defer ts.Close(t)

	trainer := NewWSClient(t, ts.WebSocketURL())
	defer trainer.Close()
	trainer.SendMessage(TrainerJoin("new").Build())
	created := trainer.WaitForType("session_created", 2*time.Second)
	sessionCode := created["sessionCode"].(string)
	token, _ := created["trainerToken"].(string)
	if token == "" {
		t.Fatal("trainerToken required")
	}
	trainer.WaitForType("connected_count", 2*time.Second)

	// A second connection with the token should take over.
	reconnect := NewWSClient(t, ts.WebSocketURL())
	defer reconnect.Close()
	reconnect.SendMessage(TrainerJoin(sessionCode).TrainerToken(token).Build())
	// The old trainer is told it's being replaced.
	trainer.WaitForType("error", 2*time.Second)
	// The reconnect receives session_created.
	reconnect.WaitForType("session_created", 2*time.Second)
	reconnect.WaitForType("connected_count", 2*time.Second)
}

// TestTrainerJoinUnknownSessionRecordsFailure covers S2: hitting a valid
// but non-existent code via trainer_join must count against the per-IP
// failed-join backoff. Without this, all 12,167 codes are enumerable in
// ~20 min because the branch skipped RecordFailedJoin.
func TestTrainerJoinUnknownSessionRecordsFailure(t *testing.T) {
	ts := NewTestServer(t)
	defer ts.Close(t)

	client := NewWSClient(t, ts.WebSocketURL())
	defer client.Close()

	codes := []string{"AAA", "BBB", "CCC"}
	for _, code := range codes {
		client.SendMessage(TrainerJoin(code).Build())
		msg := client.WaitForType("error", 2*time.Second)
		if msg["message"] != "Session introuvable" {
			t.Fatalf("expected 'Session introuvable' for %s, got %v", code, msg["message"])
		}
	}

	// The per-IP failed-join counter must reflect all three attempts. The
	// loopback connection resolves to either 127.0.0.1 (IPv4) or ::1 (IPv6)
	// depending on the host resolver, so check both.
	total := ts.Hub().Security.FailedJoinCount("127.0.0.1") + ts.Hub().Security.FailedJoinCount("::1")
	if total < 3 {
		t.Errorf("expected ≥3 recorded failed joins for loopback, got %d (S2: Session introuvable must record)", total)
	}
}

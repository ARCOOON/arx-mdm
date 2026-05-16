package policy

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestMergeBoolRestrictiveFalseWins(t *testing.T) {
	a := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	b := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	inputs := []AssignedPayload{
		{Source: ProfileSource{ID: a, Name: "A"}, Payload: json.RawMessage(`{"camera_enabled":true}`)},
		{Source: ProfileSource{ID: b, Name: "B"}, Payload: json.RawMessage(`{"camera_enabled":false}`)},
	}
	mr, err := MergeAssignedPayloads(inputs)
	if err != nil {
		t.Fatal(err)
	}
	v, ok := mr.EffectivePayload["camera_enabled"].(bool)
	if !ok || v != false {
		t.Fatalf("expected camera_enabled=false, got %#v", mr.EffectivePayload["camera_enabled"])
	}
	if len(mr.Conflicts) != 1 || mr.Conflicts[0].Path != "camera_enabled" {
		t.Fatalf("expected conflict on camera_enabled, got %+v", mr.Conflicts)
	}
}

func TestMergeNumericMaxAttemptsMostRestrictive(t *testing.T) {
	a := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	b := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	inputs := []AssignedPayload{
		{Source: ProfileSource{ID: a, Name: "Loose"}, Payload: json.RawMessage(`{"max_password_attempts":5}`)},
		{Source: ProfileSource{ID: b, Name: "Strict"}, Payload: json.RawMessage(`{"max_password_attempts":3}`)},
	}
	mr, err := MergeAssignedPayloads(inputs)
	if err != nil {
		t.Fatal(err)
	}
	v, ok := mr.EffectivePayload["max_password_attempts"].(float64)
	if !ok || v != 3 {
		t.Fatalf("expected 3 attempts, got %#v", mr.EffectivePayload["max_password_attempts"])
	}
	if len(mr.Conflicts) != 1 {
		t.Fatalf("expected numeric disagreement flagged as conflict")
	}
}

func TestMergeMinPasswordLengthHigherWins(t *testing.T) {
	a := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	b := uuid.MustParse("66666666-6666-6666-6666-666666666666")
	inputs := []AssignedPayload{
		{Source: ProfileSource{ID: a, Name: "P1"}, Payload: json.RawMessage(`{"min_password_length":8}`)},
		{Source: ProfileSource{ID: b, Name: "P2"}, Payload: json.RawMessage(`{"min_password_length":12}`)},
	}
	mr, err := MergeAssignedPayloads(inputs)
	if err != nil {
		t.Fatal(err)
	}
	v, ok := mr.EffectivePayload["min_password_length"].(float64)
	if !ok || v != 12 {
		t.Fatalf("expected min length 12, got %#v", mr.EffectivePayload["min_password_length"])
	}
	if len(mr.Conflicts) != 1 {
		t.Fatalf("expected conflicting min_password_length contributions flagged")
	}
}

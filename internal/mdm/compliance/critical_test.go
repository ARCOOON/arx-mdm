package compliance

import (
	"testing"
)

func TestPayloadCarriesCriticalSecurityControls(t *testing.T) {
	if PayloadCarriesCriticalSecurityControls(nil) {
		t.Fatal("nil expected false")
	}
	if PayloadCarriesCriticalSecurityControls(map[string]any{}) {
		t.Fatal("empty expected false")
	}
	if !PayloadCarriesCriticalSecurityControls(map[string]any{"security": map[string]any{"x": true}}) {
		t.Fatal("security subtree")
	}
	if !PayloadCarriesCriticalSecurityControls(map[string]any{"windows": map[string]any{"registry": []any{map[string]any{"hive": "HKLM"}}}}) {
		t.Fatal("windows.registry")
	}
	if PayloadCarriesCriticalSecurityControls(map[string]any{"windows": map[string]any{"wifi": map[string]any{}}}) {
		t.Fatal("empty wifi map should not count")
	}
}

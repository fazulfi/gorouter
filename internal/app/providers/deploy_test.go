package providers

import (
	"reflect"
	"testing"

	"github.com/google/uuid"
)

func TestGenerateCloudflareConfig(t *testing.T) {
	poolID := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	cfg := GenerateCloudflareConfig(poolID, "https://gw.example.com")
	if cfg == nil {
		t.Fatal("nil config")
	}
	if cfg["pool_id"] != poolID.String() {
		t.Errorf("pool_id = %v", cfg["pool_id"])
	}
	if cfg["base_url"] != "https://gw.example.com" {
		t.Errorf("base_url = %v", cfg["base_url"])
	}
	if cfg["name"] == "" || cfg["script"] == "" {
		t.Error("name and script must be present")
	}
	if !reflect.DeepEqual(cfg, GenerateCloudflareConfig(poolID, "https://gw.example.com")) {
		t.Error("generator must be deterministic")
	}
	if reflect.DeepEqual(cfg, GenerateCloudflareConfig(uuid.New(), "https://gw.example.com")) {
		t.Error("different pool must yield different payload")
	}
}

func TestGenerateDenoConfig(t *testing.T) {
	poolID := uuid.MustParse("33333333-3333-3333-3333-333333333333")

	cfg := GenerateDenoConfig(poolID, "https://gw.example.com")
	if cfg == nil {
		t.Fatal("nil config")
	}
	if cfg["pool_id"] != poolID.String() {
		t.Errorf("pool_id = %v", cfg["pool_id"])
	}
	if cfg["base_url"] != "https://gw.example.com" {
		t.Errorf("base_url = %v", cfg["base_url"])
	}
	if cfg["deno_json"] == nil || cfg["main_ts"] == "" {
		t.Error("deno_json and main_ts must be present")
	}
	if !reflect.DeepEqual(cfg, GenerateDenoConfig(poolID, "https://gw.example.com")) {
		t.Error("generator must be deterministic")
	}
}

func TestGenerateVercelConfig(t *testing.T) {
	poolID := uuid.MustParse("44444444-4444-4444-4444-444444444444")

	cfg := GenerateVercelConfig(poolID, "https://gw.example.com")
	if cfg == nil {
		t.Fatal("nil config")
	}
	if cfg["pool_id"] != poolID.String() {
		t.Errorf("pool_id = %v", cfg["pool_id"])
	}
	if cfg["base_url"] != "https://gw.example.com" {
		t.Errorf("base_url = %v", cfg["base_url"])
	}
	if cfg["vercel_json"] == nil || cfg["edge_function"] == "" {
		t.Error("vercel_json and edge_function must be present")
	}
	if !reflect.DeepEqual(cfg, GenerateVercelConfig(poolID, "https://gw.example.com")) {
		t.Error("generator must be deterministic")
	}
}

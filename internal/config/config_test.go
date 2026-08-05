package config

import "testing"

func TestDefaultSemanticThreshold(t *testing.T) {
	cfg, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Embedding.Threshold != 0.60 {
		t.Fatalf("semantic threshold = %f, want 0.60", cfg.Embedding.Threshold)
	}
}

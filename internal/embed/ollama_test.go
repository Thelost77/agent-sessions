package embed

import (
	"math"
	"testing"
)

func TestVectorEncodingRoundTrip(t *testing.T) {
	original := []float32{1, -0.5, 0.25}
	decoded, err := Decode(Encode(original))
	if err != nil {
		t.Fatal(err)
	}
	for index := range original {
		if original[index] != decoded[index] {
			t.Fatalf("value %d = %f", index, decoded[index])
		}
	}
}

func TestDotRejectsDifferentDimensions(t *testing.T) {
	if _, err := Dot([]float32{1}, []float32{1, 2}); err == nil {
		t.Fatal("Dot accepted different dimensions")
	}
	score, err := Dot([]float32{1, 0}, []float32{0.5, 0.5})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(score-0.5) > 0.0001 {
		t.Fatalf("score = %f", score)
	}
}

func TestNewRejectsRemoteEmbeddingURL(t *testing.T) {
	if _, err := New("https://embedding.example.com", "model"); err == nil {
		t.Fatal("New accepted a non-loopback URL")
	}
	if _, err := New("http://127.0.0.1:11434", "model"); err != nil {
		t.Fatal(err)
	}
}

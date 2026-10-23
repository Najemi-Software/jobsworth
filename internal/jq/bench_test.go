package jq

import (
	"testing"

	"github.com/najemi-software/jobsworth/internal/shape"
)

// BenchmarkJQApply applies a representative filter to a mid-sized shape
// (test spec §4.2).
func BenchmarkJQApply(b *testing.B) {
	n, err := Parse(`.items | map(select(.tag != "x") | {id, name: (.tag | ascii_upcase)}) | group_by(.id) | map(first)`)
	if err != nil {
		b.Fatal(err)
	}
	in := shape.ObjectShape(map[string]*shape.Shape{
		"items": shape.ArrayOf(shape.ObjectShape(map[string]*shape.Shape{"id": shape.NumberShape(), "tag": shape.StringShape()})),
	})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Apply(n, in, Options{})
	}
}

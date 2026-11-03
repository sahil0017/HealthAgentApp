package vault

import (
	"strings"
	"testing"
)

func TestChunkTextRespectsLimits(t *testing.T) {
	if got := chunkText("  "); len(got) != 0 {
		t.Fatalf("blank text gave %d chunks", len(got))
	}
	long := strings.Repeat("word ", 5000)
	got := chunkText(long)
	if len(got) != maxChunksPerPage {
		t.Fatalf("chunks = %d, want cap %d", len(got), maxChunksPerPage)
	}
	for _, c := range got {
		if len(c) > pageChunkChars {
			t.Fatalf("chunk of %d chars exceeds %d", len(c), pageChunkChars)
		}
	}
}

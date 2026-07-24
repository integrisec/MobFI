package diff

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestTrees(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()

	write(t, a, "same.txt", "hello")
	write(t, b, "same.txt", "hello") // unchanged -> no Change

	write(t, a, "sub/mod.txt", "version1")
	write(t, b, "sub/mod.txt", "version2") // same size, different content

	write(t, a, "size.txt", "abc")
	write(t, b, "size.txt", "abcd") // size change

	write(t, a, "only_a.txt", "x") // removed
	write(t, b, "only_b.txt", "y") // added

	res, err := Trees(context.Background(), a, b, nil)
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]Change{}
	for _, c := range res.Changes {
		got[c.Path] = c
	}
	if len(got) != 4 {
		t.Fatalf("got %d changes, want 4: %+v", len(got), res.Changes)
	}
	if _, ok := got["same.txt"]; ok {
		t.Error("same.txt should not be reported")
	}
	if c := got["sub/mod.txt"]; c.Kind != Modified || c.Detail != "content differs" {
		t.Errorf("sub/mod.txt = %+v", c)
	}
	if c := got["size.txt"]; c.Kind != Modified || c.Detail != "size 3 -> 4 bytes" {
		t.Errorf("size.txt = %+v", c)
	}
	if got["only_a.txt"].Kind != Removed {
		t.Errorf("only_a.txt = %+v", got["only_a.txt"])
	}
	if got["only_b.txt"].Kind != Added {
		t.Errorf("only_b.txt = %+v", got["only_b.txt"])
	}
}

func TestTreesSorted(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	write(t, a, "c.txt", "1")
	write(t, a, "a.txt", "1")
	write(t, a, "b.txt", "1") // all removed

	res, err := Trees(context.Background(), a, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a.txt", "b.txt", "c.txt"}
	for i, c := range res.Changes {
		if c.Path != want[i] {
			t.Fatalf("changes not sorted: %+v", res.Changes)
		}
	}
}

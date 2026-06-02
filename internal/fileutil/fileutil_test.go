package fileutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteIfChanged_CreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.txt")
	changed, err := WriteIfChanged(path, []byte("hello\n"))
	if err != nil {
		t.Fatalf("WriteIfChanged: %v", err)
	}
	if !changed {
		t.Error("changed = false, want true for a new file")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "hello\n" {
		t.Errorf("content = %q, want %q", got, "hello\n")
	}
}

func TestWriteIfChanged_UnchangedReturnsFalse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.txt")
	if _, err := WriteIfChanged(path, []byte("hello\n")); err != nil {
		t.Fatalf("first write: %v", err)
	}
	info1, _ := os.Stat(path)

	changed, err := WriteIfChanged(path, []byte("hello\n"))
	if err != nil {
		t.Fatalf("second write: %v", err)
	}
	if changed {
		t.Error("changed = true, want false for identical content")
	}
	info2, _ := os.Stat(path)
	if info1.ModTime() != info2.ModTime() {
		t.Error("file was rewritten despite identical content")
	}
}

func TestWriteIfChanged_OverwritesChangedContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.txt")
	if _, err := WriteIfChanged(path, []byte("v1\n")); err != nil {
		t.Fatalf("first write: %v", err)
	}
	changed, err := WriteIfChanged(path, []byte("v2\n"))
	if err != nil {
		t.Fatalf("second write: %v", err)
	}
	if !changed {
		t.Error("changed = false, want true when content differs")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "v2\n" {
		t.Errorf("content = %q, want %q", got, "v2\n")
	}
}

func TestWriteIfChanged_Mode0644(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.txt")
	if _, err := WriteIfChanged(path, []byte("x\n")); err != nil {
		t.Fatalf("WriteIfChanged: %v", err)
	}
	info, _ := os.Stat(path)
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Errorf("mode = %o, want 644", perm)
	}
}

func TestWriteIfChanged_LeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	if _, err := WriteIfChanged(path, []byte("x\n")); err != nil {
		t.Fatalf("WriteIfChanged: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "out.txt" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("temp file left behind, dir contains: %v", names)
	}
}

func TestUnifiedDiff_IdenticalContent(t *testing.T) {
	if got := UnifiedDiff("f", []byte("a\nb\n"), []byte("a\nb\n")); got != "" {
		t.Errorf("expected empty diff, got:\n%s", got)
	}
}

func TestUnifiedDiff_Addition(t *testing.T) {
	got := UnifiedDiff("f", []byte("a\nb\n"), []byte("a\nb\nc\n"))
	if !strings.Contains(got, "+c") {
		t.Errorf("diff missing added line, got:\n%s", got)
	}
	if !strings.Contains(got, "@@") {
		t.Errorf("diff missing hunk header, got:\n%s", got)
	}
}

func TestUnifiedDiff_Removal(t *testing.T) {
	got := UnifiedDiff("f", []byte("a\nb\nc\n"), []byte("a\nc\n"))
	if !strings.Contains(got, "-b") {
		t.Errorf("diff missing removed line, got:\n%s", got)
	}
}

func TestUnifiedDiff_NewFile(t *testing.T) {
	got := UnifiedDiff("f", nil, []byte("a\nb\n"))
	if !strings.Contains(got, "+a") || !strings.Contains(got, "+b") {
		t.Errorf("new-file diff missing content, got:\n%s", got)
	}
	if !strings.Contains(got, "@@ -1,0") {
		t.Errorf("new-file diff should have @@ -1,0 header, got:\n%s", got)
	}
}

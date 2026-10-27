package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReplaceBlockWritesOnlyMetricsDoc(t *testing.T) {
	dir := t.TempDir()
	other := filepath.Join(dir, "README.md")
	if err := os.WriteFile(other, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := replaceBlock(other, "t"); err == nil {
		t.Error("запись в чужой файл должна отклоняться")
	}
	doc := filepath.Join(dir, "metrics.md")
	if err := os.WriteFile(doc, []byte("до\n"+metricsBegin+"\nстарое\n"+metricsEnd+"\nпосле\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := replaceBlock(doc, "новое"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(doc)
	got := string(b)
	if !strings.Contains(got, "новое") || strings.Contains(got, "старое") || !strings.HasPrefix(got, "до\n") || !strings.HasSuffix(got, "после\n") {
		t.Errorf("заменён не только блок: %q", got)
	}
	noMarks := filepath.Join(t.TempDir(), "metrics.md")
	_ = os.WriteFile(noMarks, []byte("без маркеров"), 0o644)
	if err := replaceBlock(noMarks, "x"); err == nil {
		t.Error("без маркеров файл менять нельзя")
	}
}

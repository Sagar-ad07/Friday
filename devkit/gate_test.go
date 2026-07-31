package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAllGreen(t *testing.T) {
	if !allGreen([]TestRun{{Pass: true}, {Pass: true}}) {
		t.Fatal("all true should be green")
	}
	if allGreen([]TestRun{{Pass: true}, {Pass: false}}) {
		t.Fatal("any false should not be green")
	}
}

func TestApplyGateRejectsFailingDraft(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "target")
	draft := filepath.Join(tmp, "draft")
	os.MkdirAll(target, 0o755)
	os.MkdirAll(draft, 0o755)
	os.WriteFile(filepath.Join(target, "f.txt"), []byte("original"), 0o644)
	os.WriteFile(filepath.Join(draft, "f.txt"), []byte("changed"), 0o644)

	_, err := applyDraft(draft, target, []TestRun{{Round: 1, Pass: true}, {Round: 2, Pass: false}})
	if err == nil {
		t.Fatal("apply must be rejected when any test round fails")
	}
	b, _ := os.ReadFile(filepath.Join(target, "f.txt"))
	if string(b) != "original" {
		t.Fatalf("target must stay untouched on rejection, got %q", b)
	}
}

func TestApplyGateAcceptsGreenDraft(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "target")
	draft := filepath.Join(tmp, "draft")
	os.MkdirAll(target, 0o755)
	os.MkdirAll(draft, 0o755)
	os.WriteFile(filepath.Join(target, "f.txt"), []byte("original"), 0o644)
	os.WriteFile(filepath.Join(draft, "f.txt"), []byte("verified"), 0o644)

	c, err := applyDraft(draft, target, []TestRun{{Pass: true}, {Pass: true}, {Pass: true}})
	if err != nil {
		t.Fatalf("apply should succeed with green rounds: %v", err)
	}
	if len(c.Files) != 1 || c.Files[0] != "f.txt" {
		t.Fatalf("unexpected files: %v", c.Files)
	}
	b, _ := os.ReadFile(filepath.Join(target, "f.txt"))
	if string(b) != "verified" {
		t.Fatalf("target should be updated, got %q", b)
	}

	if err := rollback(c.ID); err != nil {
		t.Fatalf("rollback failed: %v", err)
	}
	b, _ = os.ReadFile(filepath.Join(target, "f.txt"))
	if string(b) != "original" {
		t.Fatalf("rollback should restore original, got %q", b)
	}
}

func TestListChangedFiles(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "target")
	draft := filepath.Join(tmp, "draft")
	os.MkdirAll(target, 0o755)
	os.MkdirAll(draft, 0o755)
	os.WriteFile(filepath.Join(target, "same.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(draft, "same.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(target, "diff.txt"), []byte("old"), 0o644)
	os.WriteFile(filepath.Join(draft, "diff.txt"), []byte("new"), 0o644)
	os.WriteFile(filepath.Join(draft, "new.txt"), []byte("n"), 0o644)
	os.WriteFile(filepath.Join(draft, ".target"), []byte("t"), 0o644)

	changed, err := listChangedFiles(draft, target)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 2 {
		t.Fatalf("expected 2 changed files, got %v", changed)
	}
}

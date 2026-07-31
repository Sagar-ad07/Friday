package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type TestRun struct {
	Round  int
	Pass   bool
	Output string
}

func runGoTest(dir string, verbose bool) (bool, string) {
	args := []string{"test", "./..."}
	if verbose {
		args = append(args, "-v")
	}
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, string(out)
	}
	return true, string(out)
}

func runTests(dir string, runs int, verbose bool) ([]TestRun, error) {
	if runs < 1 {
		runs = 1
	}
	if runs > 10 {
		runs = 10
	}
	var out []TestRun
	for i := 0; i < runs; i++ {
		pass, output := runGoTest(dir, verbose)
		out = append(out, TestRun{Round: i + 1, Pass: pass, Output: output})
	}
	return out, nil
}

func allGreen(runs []TestRun) bool {
	for _, r := range runs {
		if !r.Pass {
			return false
		}
	}
	return true
}

func testSummary(runs []TestRun) []string {
	var lines []string
	for _, r := range runs {
		status := "PASS"
		if !r.Pass {
			status = "FAIL"
		}
		lines = append(lines, fmt.Sprintf("round %d: %s", r.Round, status))
		if !r.Pass {
			lines = append(lines, compactOutput(r.Output))
		}
	}
	return lines
}

func compactOutput(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 600 {
		return s[:600] + "..."
	}
	return s
}

func verifyDraft(draftDir string, runs int) ([]TestRun, error) {
	if _, err := os.Stat(filepath.Join(draftDir, "go.mod")); err != nil {
		return nil, fmt.Errorf("draft %q is not a Go module (no go.mod)", draftDir)
	}
	return runTests(draftDir, runs, false)
}

func copyTree(src, dst string, skip []string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if skipMatch(skip, rel, info.IsDir()) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		dest := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(dest, info.Mode())
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dest, b, info.Mode())
	})
}

func listChangedFiles(draftDir, target string) ([]string, error) {
	var changed []string
	err := filepath.Walk(draftDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(draftDir, path)
		if err != nil {
			return err
		}
		if rel == ".target" {
			return nil
		}
		tgt := filepath.Join(target, rel)
		a, err1 := os.ReadFile(path)
		b, err2 := os.ReadFile(tgt)
		if os.IsNotExist(err1) || os.IsNotExist(err2) {
			changed = append(changed, rel)
			return nil
		}
		if string(a) != string(b) {
			changed = append(changed, rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return changed, nil
}

func applyDraft(draftDir, target string, runs []TestRun) (Change, error) {
	c := Change{
		ID:      newID(),
		Time:    time.Now().Format(time.RFC3339),
		Kind:    "apply",
		Draft:   filepath.Base(draftDir),
		Target:  target,
		TestLog: testSummary(runs),
	}
	if !allGreen(runs) {
		return c, fmt.Errorf("gate REJECTED: not all test rounds are green (see test log)")
	}
	changed, err := listChangedFiles(draftDir, target)
	if err != nil {
		return c, err
	}
	if len(changed) == 0 {
		return c, fmt.Errorf("gate REJECTED: draft has no differences from target")
	}
	c.Files = changed

	backup := filepath.Join("data", "backups", c.ID)
	if err := os.MkdirAll(backup, 0o755); err != nil {
		return c, err
	}
	for _, rel := range changed {
		src := filepath.Join(draftDir, rel)
		dst := filepath.Join(target, rel)
		bkp := filepath.Join(backup, rel)
		if _, err := os.Stat(dst); err == nil {
			b, _ := os.ReadFile(dst)
			_ = os.MkdirAll(filepath.Dir(bkp), 0o755)
			if err := os.WriteFile(bkp, b, 0o644); err != nil {
				return c, err
			}
		}
		b, err := os.ReadFile(src)
		if err != nil {
			return c, err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return c, err
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			return c, err
		}
	}
	c.Backup = backup
	if err := appendJournal(c); err != nil {
		return c, err
	}
	return c, nil
}

func rollback(id string) error {
	all, err := loadJournal()
	if err != nil {
		return err
	}
	for i := range all {
		if all[i].ID == id && all[i].Kind == "apply" && !all[i].Reverted {
			c := &all[i]
			if c.Backup == "" {
				return fmt.Errorf("change %q has no backup", id)
			}
			for _, rel := range c.Files {
				bkp := filepath.Join(c.Backup, rel)
				dst := filepath.Join(c.Target, rel)
				if _, err := os.Stat(bkp); err != nil {
					_ = os.Remove(dst)
					continue
				}
				b, err := os.ReadFile(bkp)
				if err != nil {
					return err
				}
				if err := os.WriteFile(dst, b, 0o644); err != nil {
					return err
				}
			}
			c.Reverted = true
			rewriteJournal(all)
			return nil
		}
	}
	return fmt.Errorf("no applicable change found for %q", id)
}

func rewriteJournal(all []Change) error {
	if err := os.MkdirAll(filepath.Dir(journalPath()), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(journalPath(), os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, c := range all {
		b, _ := json.Marshal(c)
		if _, err := f.Write(append(b, '\n')); err != nil {
			return err
		}
	}
	return nil
}

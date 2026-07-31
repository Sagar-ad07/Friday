package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const draftsRoot = "drafts"

func initDraft(target, name string, skip []string) (string, error) {
	target = filepath.Clean(target)
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if name == "" {
		name = fmt.Sprintf("%s-%s", filepath.Base(abs), newID())
	}
	dst := filepath.Join(draftsRoot, name)
	if _, err := os.Stat(dst); err == nil {
		return "", fmt.Errorf("draft %q already exists", name)
	}
	if err := copyTree(target, dst, skip); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dst, ".target"), []byte(abs), 0o644); err != nil {
		return "", err
	}
	fmt.Printf("draft created: %s\n", dst)
	fmt.Printf("source target: %s\n", abs)
	return dst, nil
}

// skipMatch reports whether a path (relative to the copied root) should be
// excluded. Entries ending in "*" are suffix matches (e.g. "*.exe"); exact
// names match a file, and a plain dir name excludes the whole subtree.
func skipMatch(skip []string, rel string, isDir bool) bool {
	for _, s := range skip {
		if s == "" {
			continue
		}
		if strings.HasSuffix(s, "*") {
			if strings.HasSuffix(rel, strings.TrimSuffix(s, "*")) {
				return true
			}
			continue
		}
		if rel == s {
			return true
		}
		if isDir && strings.HasPrefix(rel, s+"/") {
			return true
		}
	}
	return false
}

func resolveDraft(name string) (string, error) {
	d := filepath.Join(draftsRoot, name)
	st, err := os.Stat(d)
	if err != nil {
		return "", fmt.Errorf("draft %q not found (available: %s)", name, listDrafts())
	}
	if !st.IsDir() {
		return "", fmt.Errorf("draft %q is not a directory", name)
	}
	return d, nil
}

func listDrafts() string {
	entries, err := os.ReadDir(draftsRoot)
	if err != nil {
		return "none"
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

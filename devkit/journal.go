package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Change struct {
	ID       string   `json:"id"`
	Time     string   `json:"time"`
	Kind     string   `json:"kind"`
	Draft    string   `json:"draft"`
	Target   string   `json:"target"`
	Files    []string `json:"files"`
	TestLog  []string `json:"test_log"`
	Backup   string   `json:"backup"`
	Reverted bool     `json:"reverted"`
}

func journalPath() string {
	return filepath.Join("data", "journal.jsonl")
}

func appendJournal(c Change) error {
	if err := os.MkdirAll(filepath.Dir(journalPath()), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(journalPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, _ := json.Marshal(c)
	_, err = f.Write(append(b, '\n'))
	return err
}

func loadJournal() ([]Change, error) {
	b, err := os.ReadFile(journalPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Change
	for _, line := range splitLines(string(b)) {
		if line == "" {
			continue
		}
		var c Change
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			return nil, fmt.Errorf("journal corrupt at line: %v", err)
		}
		out = append(out, c)
	}
	return out, nil
}

func splitLines(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '\n' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	out = append(out, cur)
	return out
}

func findChange(id string) (*Change, error) {
	all, err := loadJournal()
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].ID == id {
			return &all[i], nil
		}
	}
	return nil, fmt.Errorf("change %q not found in journal", id)
}

func newID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

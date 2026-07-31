package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func usage() {
	fmt.Println(`friday-devkit - two-zone self-improvement kit for Friday

USAGE:
  devkit init <target-dir> [name]        copy target into draft zone (outside!)
  devkit list                            list drafts
  devkit test <draft> [runs]             run tests N times (default 3)
  devkit verify <draft> [runs]           verify draft: all rounds must be green
  devkit diff <draft>                    list changed files vs target
  devkit apply <draft> <target>          merge draft into target ONLY if gate is green
  devkit rollback <change-id>            revert an applied change from the journal
  devkit journal                         show change history
  devkit skills                          show Friday's dev skill schemas

RULES:
  - edits happen only inside drafts/<name> (the OUTSIDE zone)
  - apply is blocked unless every test round is green
  - every apply is journaled with a backup; rollback restores instantly`)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		return
	}
	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "init":
		if len(args) < 1 {
			fmt.Println("usage: devkit init <target-dir> [name] [skip-list]")
			fmt.Println("  skip-list: comma-separated paths, '*.ext' suffix matches supported")
			return
		}
		name := ""
		if len(args) > 1 {
			name = args[1]
		}
		var skip []string
		if len(args) > 2 {
			skip = strings.Split(args[2], ",")
		}
		if extra := os.Getenv("DEVKIT_SKIP"); extra != "" {
			skip = append(skip, strings.Split(extra, ",")...)
		}
		d, err := initDraft(args[0], name, skip)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("work OUTSIDE here: %s\n", d)

	case "list":
		fmt.Println(listDrafts())

	case "test":
		if len(args) < 1 {
			fmt.Println("usage: devkit test <draft> [runs]")
			return
		}
		d, err := resolveDraft(args[0])
		if err != nil {
			fatal(err)
		}
		runs := 3
		if len(args) > 1 {
			fmt.Sscanf(args[1], "%d", &runs)
		}
		rounds, err := runTests(d, runs, false)
		if err != nil {
			fatal(err)
		}
		for _, r := range rounds {
			fmt.Printf("round %d: ", r.Round)
			if r.Pass {
				fmt.Println("PASS")
			} else {
				fmt.Println("FAIL")
			}
		}

	case "verify":
		if len(args) < 1 {
			fmt.Println("usage: devkit verify <draft> [runs]")
			return
		}
		d, err := resolveDraft(args[0])
		if err != nil {
			fatal(err)
		}
		runs := 3
		if len(args) > 1 {
			fmt.Sscanf(args[1], "%d", &runs)
		}
		rounds, err := verifyDraft(d, runs)
		if err != nil {
			fatal(err)
		}
		ok := allGreen(rounds)
		for _, line := range testSummary(rounds) {
			fmt.Println("  " + line)
		}
		if ok {
			fmt.Printf("VERIFIED: draft %q passed %d/%d rounds. Safe to apply.\n", args[0], runs, runs)
		} else {
			fmt.Printf("NOT VERIFIED: draft %q failed. Fix it OUTSIDE, never apply.\n", args[0])
		}

	case "diff":
		if len(args) < 1 {
			fmt.Println("usage: devkit diff <draft>")
			return
		}
		d, err := resolveDraft(args[0])
		if err != nil {
			fatal(err)
		}
		tgt := targetOf(d)
		if tgt == "" {
			fmt.Println("target unknown for this draft")
			return
		}
		changed, err := listChangedFiles(d, tgt)
		if err != nil {
			fatal(err)
		}
		if len(changed) == 0 {
			fmt.Println("no differences from target")
			return
		}
		for _, f := range changed {
			fmt.Println("  changed: " + f)
		}

	case "apply":
		if len(args) < 2 {
			fmt.Println("usage: devkit apply <draft> <target-dir>")
			return
		}
		d, err := resolveDraft(args[0])
		if err != nil {
			fatal(err)
		}
		rounds, err := verifyDraft(d, 3)
		if err != nil {
			fatal(err)
		}
		for _, line := range testSummary(rounds) {
			fmt.Println("  " + line)
		}
		if !allGreen(rounds) {
			fmt.Println("GATE REJECTED: tests not green. Fix OUTSIDE first.")
			return
		}
		c, err := applyDraft(d, args[1], rounds)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("GATE PASSED - applied change %s (%d files)\n", c.ID, len(c.Files))
		fmt.Println("  backup: " + c.Backup)
		fmt.Printf("  rollback: devkit rollback %s\n", c.ID)

	case "rollback":
		if len(args) < 1 {
			fmt.Println("usage: devkit rollback <change-id>")
			return
		}
		if err := rollback(args[0]); err != nil {
			fatal(err)
		}
		fmt.Printf("change %s rolled back. Target restored.\n", args[0])

	case "journal":
		all, err := loadJournal()
		if err != nil {
			fatal(err)
		}
		if len(all) == 0 {
			fmt.Println("journal is empty")
			return
		}
		for i := len(all) - 1; i >= 0; i-- {
			c := all[i]
			state := "applied"
			if c.Reverted {
				state = "reverted"
			}
			fmt.Printf("%s  %-8s  %-9s  draft=%s  target=%s  files=%d\n",
				c.ID, c.Time, state, c.Draft, c.Target, len(c.Files))
		}

	case "skills":
		for _, s := range DevSkills {
			b, _ := json.MarshalIndent(s, "  ", "  ")
			fmt.Println(string(b))
		}

	default:
		usage()
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

func targetOf(draft string) string {
	meta := filepath.Join(draft, ".target")
	b, err := os.ReadFile(meta)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

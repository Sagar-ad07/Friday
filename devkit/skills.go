package main

type Arg struct {
	Name        string
	Type        string
	Required    bool
	Description string
}

type Skill struct {
	Name        string
	Description string
	Args        []Arg
}

var DevSkills = []Skill{
	{
		Name:        "read_file",
		Description: "Read a file from the draft zone or target. Returns content with line numbers.",
		Args: []Arg{
			{Name: "path", Type: "string", Required: true, Description: "path relative to the zone root"},
		},
	},
	{
		Name:        "write_file",
		Description: "Write a new file inside the draft zone. Never allowed on the target directly.",
		Args: []Arg{
			{Name: "path", Type: "string", Required: true, Description: "draft-relative path"},
			{Name: "content", Type: "string", Required: true, Description: "full file content"},
		},
	},
	{
		Name:        "edit_file",
		Description: "Edit an existing file inside the draft zone using exact old->new replacement.",
		Args: []Arg{
			{Name: "path", Type: "string", Required: true, Description: "draft-relative path"},
			{Name: "old", Type: "string", Required: true, Description: "exact text to replace"},
			{Name: "new", Type: "string", Required: true, Description: "replacement text"},
		},
	},
	{
		Name:        "run_command",
		Description: "Run a shell command in the draft zone (build, go test, scripts).",
		Args: []Arg{
			{Name: "command", Type: "string", Required: true, Description: "command line to execute"},
			{Name: "cwd", Type: "string", Required: false, Description: "working directory, defaults to draft root"},
		},
	},
	{
		Name:        "search_code",
		Description: "Search for a pattern inside the draft zone.",
		Args: []Arg{
			{Name: "pattern", Type: "string", Required: true, Description: "regex or literal pattern"},
			{Name: "path", Type: "string", Required: false, Description: "restrict search to a path"},
		},
	},
	{
		Name:        "run_tests",
		Description: "Run the test suite in the draft zone multiple times. The gate only accepts stable green runs.",
		Args: []Arg{
			{Name: "runs", Type: "int", Required: false, Description: "number of test rounds, default 3"},
			{Name: "verbose", Type: "bool", Required: false, Description: "show test output, default false"},
		},
	},
	{
		Name:        "verify",
		Description: "Verify a draft: all test rounds green + feature demo succeeds. Output is the proof shown before apply.",
		Args: []Arg{
			{Name: "draft", Type: "string", Required: true, Description: "draft name"},
			{Name: "runs", Type: "int", Required: false, Description: "test rounds, default 3"},
		},
	},
}

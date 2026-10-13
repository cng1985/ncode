package repository

import "testing"

func TestCleanRepoPathRejectsTraversalAndGitRevisionSyntax(t *testing.T) {
	for _, value := range []string{"../secret", "src/../../secret", "/etc/passwd", `src\\main.go`, "HEAD:README.md", "a\x00b"} {
		if clean, ok := cleanRepoPath(value); ok {
			t.Errorf("cleanRepoPath(%q) = %q, accepted unsafe path", value, clean)
		}
	}
	for _, value := range []string{"", "README.md", "src/main.go", "docs/guide.md"} {
		if clean, ok := cleanRepoPath(value); !ok {
			t.Errorf("cleanRepoPath(%q) rejected a valid path", value)
		} else if value != "" && clean != value {
			t.Errorf("cleanRepoPath(%q) = %q", value, clean)
		}
	}
}

func TestValidRefRejectsOptionAndRevisionInjection(t *testing.T) {
	for _, value := range []string{"--help", "main..other", "main@{1}", "main:README.md", "feature//one", "feature/"} {
		if validRef(value) {
			t.Errorf("validRef(%q) accepted unsafe Git revision syntax", value)
		}
	}
	for _, value := range []string{"main", "feature/one", "refs/heads/release-1.2"} {
		if !validRef(value) {
			t.Errorf("validRef(%q) rejected a valid branch ref", value)
		}
	}
}

func TestParseTreeReturnsEntryNamesAndPaths(t *testing.T) {
	output := []byte("100644 blob abcdef\tREADME.md\x00100000 tree 123456\tsrc\x00")
	entries, err := parseTree("", output)
	if err != nil {
		t.Fatalf("parseTree() error = %v", err)
	}
	if len(entries) != 2 || entries[0].Name != "README.md" || entries[0].Path != "README.md" || entries[0].Type != "blob" || entries[1].Path != "src" || entries[1].Type != "tree" {
		t.Fatalf("parseTree() entries = %+v", entries)
	}
}

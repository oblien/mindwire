package agent

import (
	"reflect"
	"testing"
)

func TestShellWordsPreserveQuotedArguments(t *testing.T) {
	args := []string{"/bin/zsh", "-lc", "python3 -c 'print(\"hello\")'", "", "a b", "世界", "a\u00a0b", "$literal", "a\\b", "line\nbreak"}
	if got := ShellWords(JoinShellWords(args)); !reflect.DeepEqual(got, args) {
		t.Fatalf("argv changed: got %#v, want %#v", got, args)
	}
	for _, command := range []string{`/bin/sh -c 'echo "hello"'`, `/bin/sh -c "echo \"hello\""`} {
		if got := ShellWords(command); !reflect.DeepEqual(got, []string{"/bin/sh", "-c", `echo "hello"`}) {
			t.Errorf("quoting changed arguments: %#v", got)
		}
	}
	for _, command := range []string{`unterminated 'quote`, `trailing\`} {
		if got := ShellWords(command); got != nil {
			t.Errorf("malformed command matched as %#v", got)
		}
	}
}

func TestToolInvocationIdentityIgnoresOutputButPreservesArguments(t *testing.T) {
	left := &ToolAction{Kind: KindShell, Shell: &ShellCommand{Command: `/bin/sh -c 'echo "hello"'`, Cwd: "/work", Stdout: "hello"}}
	right := &ToolAction{Kind: KindShell, Shell: &ShellCommand{Command: `/bin/sh -c "echo \"hello\""`}}
	if !left.SameInvocation(right) {
		t.Fatal("equivalent commands did not match")
	}
	right.Shell.Cwd = "/other"
	if left.SameInvocation(right) {
		t.Fatal("different working directories matched")
	}
	right.Shell.Cwd, right.Shell.Command = "", "echo something-else"
	if left.SameInvocation(right) {
		t.Fatal("different commands matched")
	}
	left = &ToolAction{Kind: KindFileEdit, Files: []FileChange{{Path: "a", Op: "edit", Diff: "patch"}, {Path: "b", Op: "create", Diff: "new"}}}
	right = &ToolAction{Kind: KindFileEdit, Files: []FileChange{{Path: "b", Op: "create"}, {Path: "a", Op: "edit"}}}
	if !left.SameInvocation(right) {
		t.Fatal("a reordered path-only preview did not match the full diff")
	}
	right.Files[0].Path = "c"
	if left.SameInvocation(right) {
		t.Fatal("different changed files matched")
	}
}

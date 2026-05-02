package web

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestValidateEditorPath(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "SPEC.md")
	if err := os.WriteFile(specPath, []byte("# Spec\n"), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	got, err := validateEditorPath(specPath, dir)
	if err != nil {
		t.Fatalf("validateEditorPath: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("path = %q, want absolute", got)
	}

	if _, err := validateEditorPath(dir, dir); err == nil {
		t.Fatal("validateEditorPath accepted a directory")
	}
	if _, err := validateEditorPath(" ", dir); err == nil {
		t.Fatal("validateEditorPath accepted an empty path")
	}
	if _, err := validateEditorPath(filepath.Join(filepath.Dir(dir), "outside.md"), dir); err == nil {
		t.Fatal("validateEditorPath accepted a path outside the root")
	}
}

func TestEditorCommand(t *testing.T) {
	name, args, err := editorCommand("/tmp/SPEC.md", "default")
	if err != nil {
		t.Fatalf("editorCommand default: %v", err)
	}
	if runtime.GOOS == "darwin" {
		if name != "open" || len(args) != 1 || args[0] != "/tmp/SPEC.md" {
			t.Fatalf("darwin default command = %q %#v", name, args)
		}
		name, args, err = editorCommand("/tmp/SPEC.md", "vscode")
		if err != nil {
			t.Fatalf("editorCommand vscode: %v", err)
		}
		if name != "open" || len(args) != 3 || args[0] != "-a" || args[1] != "Visual Studio Code" || args[2] != "/tmp/SPEC.md" {
			t.Fatalf("darwin vscode command = %q %#v", name, args)
		}
	} else if runtime.GOOS == "windows" {
		if name != "rundll32.exe" || len(args) != 2 || args[0] != "url.dll,FileProtocolHandler" || args[1] != "/tmp/SPEC.md" {
			t.Fatalf("windows default command = %q %#v", name, args)
		}
	} else if name != "xdg-open" || len(args) != 1 || args[0] != "/tmp/SPEC.md" {
		t.Fatalf("non-darwin default command = %q %#v", name, args)
	}

	if _, _, err := editorCommand("/tmp/SPEC.md", "unknown"); err == nil {
		t.Fatal("editorCommand accepted an unknown editor")
	}
}

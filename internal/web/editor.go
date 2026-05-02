package web

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type editorOpener interface {
	Open(path string, editor string) error
}

type externalEditorOpener struct {
	root string
}

func (o externalEditorOpener) Open(path string, editor string) error {
	resolved, err := validateEditorPath(path, o.root)
	if err != nil {
		return err
	}
	name, args, err := editorCommand(resolved, editor)
	if err != nil {
		return err
	}
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() {
		if err := cmd.Wait(); err != nil {
			log.Printf("editor launcher exited with error: %v", err)
		}
	}()
	return nil
}

func validateEditorPath(path string, root string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("spec path is required")
	}
	if strings.ContainsRune(path, 0) {
		return "", fmt.Errorf("spec path is invalid")
	}
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("server working directory is unavailable")
		}
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("server working directory is unavailable")
	}
	root, err = filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", fmt.Errorf("server working directory is unavailable")
	}
	resolved, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("spec path is invalid")
	}
	resolved, err = filepath.EvalSymlinks(resolved)
	if err != nil {
		return "", fmt.Errorf("spec path is not accessible")
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("spec path must be inside the project directory")
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("spec path is not accessible")
	}
	if info.IsDir() {
		return "", fmt.Errorf("spec path must be a file")
	}
	return resolved, nil
}

func editorCommand(path string, editor string) (string, []string, error) {
	editor = strings.ToLower(strings.TrimSpace(editor))
	if editor == "" {
		editor = "default"
	}
	if runtime.GOOS == "darwin" {
		switch editor {
		case "default":
			return "open", []string{path}, nil
		case "vscode":
			return "open", []string{"-a", "Visual Studio Code", path}, nil
		case "xcode":
			return "open", []string{"-a", "Xcode", path}, nil
		case "macvim":
			return "open", []string{"-a", "MacVim", path}, nil
		default:
			return "", nil, fmt.Errorf("unsupported editor %q", editor)
		}
	}
	if runtime.GOOS == "windows" {
		switch editor {
		case "default":
			return "rundll32.exe", []string{"url.dll,FileProtocolHandler", path}, nil
		case "vscode":
			return windowsCodeCommand(path)
		default:
			return "", nil, fmt.Errorf("editor %q is not supported on %s", editor, runtime.GOOS)
		}
	}
	if editor == "vscode" {
		return "code", []string{"--", path}, nil
	}
	if editor == "default" {
		return "xdg-open", []string{path}, nil
	}
	return "", nil, fmt.Errorf("editor %q is not supported on %s", editor, runtime.GOOS)
}

func windowsCodeCommand(path string) (string, []string, error) {
	resolved, err := exec.LookPath("code")
	if err == nil {
		return resolved, []string{"--", path}, nil
	}
	return "", nil, fmt.Errorf("editor %q is not installed", "vscode")
}

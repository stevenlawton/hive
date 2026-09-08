package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// sameDevice reports whether two paths sit on the same filesystem. os.Rename is
// a single syscall and cannot cross a mount point, so this is what decides
// whether the test is exercising the fallback at all.
func sameDevice(a, b os.FileInfo) bool {
	sa, oka := a.Sys().(*syscall.Stat_t)
	sb, okb := b.Sys().(*syscall.Stat_t)
	if !oka || !okb {
		return false
	}
	return sa.Dev == sb.Dev
}

// crossDeviceDir returns a directory on a different filesystem from t.TempDir(),
// so os.Rename between them really does fail with EXDEV rather than a simulation
// of it. /dev/shm is its own tmpfs mount on Linux; if it isn't there, or it
// happens to share a device with the temp dir, the test has nothing to prove and
// skips.
func crossDeviceDir(t *testing.T, local string) string {
	t.Helper()
	other, err := os.MkdirTemp("/dev/shm", "hive-xdev-")
	if err != nil {
		t.Skip("no /dev/shm to test a cross-filesystem move against")
	}
	t.Cleanup(func() { os.RemoveAll(other) })

	a, err := os.Stat(other)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.Stat(local)
	if err != nil {
		t.Fatal(err)
	}
	if sameDevice(a, b) {
		t.Skip("/dev/shm and the temp dir are the same filesystem here")
	}
	return other
}

// scratch_dir defaults to /tmp/hive-scratch and Steve's is /tmp/kl-scratch —
// both tmpfs, while repos_dir is on disk. os.Rename is a single syscall that
// cannot cross a mount point, so promote failed with EXDEV every time on any
// normal setup: "failed to move scratch to repos: rename /tmp/kl-scratch/...".
func TestPromoteScratchAcrossFilesystems(t *testing.T) {
	repos := t.TempDir()
	scratchParent := crossDeviceDir(t, repos)
	scratch := filepath.Join(scratchParent, "scratch-20260908-003")

	if err := os.MkdirAll(filepath.Join(scratch, "notes", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scratch, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scratch, "notes", "deep", "run.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	newPath, err := PromoteScratch(scratch, repos, "modbus-lab")
	if err != nil {
		t.Fatalf("PromoteScratch across filesystems: %v", err)
	}

	if want := filepath.Join(repos, "modbus-lab"); newPath != want {
		t.Errorf("newPath = %q, want %q", newPath, want)
	}
	if got, err := os.ReadFile(filepath.Join(newPath, "main.go")); err != nil || string(got) != "package main\n" {
		t.Errorf("main.go = %q, %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(newPath, "notes", "deep", "run.sh")); err != nil || string(got) != "#!/bin/sh\n" {
		t.Errorf("nested run.sh = %q, %v", got, err)
	}
	// An executable that stops being executable is a broken scratch, not a moved one.
	if fi, err := os.Stat(filepath.Join(newPath, "notes", "deep", "run.sh")); err != nil {
		t.Fatal(err)
	} else if fi.Mode().Perm()&0o111 == 0 {
		t.Errorf("run.sh lost its executable bit: %v", fi.Mode())
	}
	// A move leaves nothing behind — otherwise the scratch is still listed in
	// the sidebar and the same work exists in two places.
	if _, err := os.Stat(scratch); !os.IsNotExist(err) {
		t.Errorf("the scratch survived the move: %v", err)
	}
	if _, err := os.Stat(filepath.Join(newPath, ".git")); err != nil {
		t.Errorf("promoted repo was not git init'd: %v", err)
	}
}

// The same-filesystem path must keep working, and must still be the cheap
// rename rather than a copy of a large tree.
func TestPromoteScratchSameFilesystem(t *testing.T) {
	root := t.TempDir()
	repos := filepath.Join(root, "repos")
	scratch := filepath.Join(root, "scratch", "scratch-20260908-001")
	if err := os.MkdirAll(repos, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scratch, "note.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	newPath, err := PromoteScratch(scratch, repos, "kept")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(newPath, "note.txt")); err != nil || string(got) != "hi" {
		t.Errorf("note.txt = %q, %v", got, err)
	}
	if _, err := os.Stat(scratch); !os.IsNotExist(err) {
		t.Errorf("the scratch survived the move: %v", err)
	}
}

// Promoting onto an existing name must refuse before anything moves, or the
// answer to "what happened to my other repo" is that hive ate it.
func TestPromoteScratchRefusesAnExistingName(t *testing.T) {
	root := t.TempDir()
	repos := filepath.Join(root, "repos")
	scratch := filepath.Join(root, "scratch-20260908-002")
	if err := os.MkdirAll(filepath.Join(repos, "taken"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := PromoteScratch(scratch, repos, "taken"); err == nil {
		t.Fatal("promoting onto an existing repo name succeeded")
	} else if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error = %v, want it to say the repo already exists", err)
	}
	if _, err := os.Stat(scratch); err != nil {
		t.Errorf("the scratch was disturbed by a refused promote: %v", err)
	}
}

// git init failing used to be discarded, so promote reported success and left a
// directory that was not a repo.
func TestPromoteScratchReportsAFailedGitInit(t *testing.T) {
	root := t.TempDir()
	repos := filepath.Join(root, "repos")
	scratch := filepath.Join(root, "scratch-20260908-004")
	if err := os.MkdirAll(repos, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", t.TempDir()) // no git on PATH

	if _, err := PromoteScratch(scratch, repos, "nogit"); err == nil {
		t.Error("promote reported success with no git available")
	}
}

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Identity tiers, most durable first. The tier is returned alongside the id
// because a repo can gain a stronger identity later — see adoptStore.
const (
	tierRemote = iota
	tierFirstCommit
	tierPath
)

// repoIdentity returns the most durable identity available for a repo. The
// backlog is addressed by this, so it has to survive the repo being moved or
// re-cloned — which a filesystem path does not.
func repoIdentity(repoPath string) (string, int) {
	main := mainWorktree(repoPath)
	if out, err := exec.Command("git", "-C", main, "remote", "get-url", "origin").Output(); err == nil {
		if id := normalizeRemote(string(out)); id != "" {
			return id, tierRemote
		}
	}
	if out, err := exec.Command("git", "-C", main, "rev-list", "--max-parents=0", "HEAD").Output(); err == nil {
		// A grafted history can report several roots; the last is the oldest,
		// so it is the one that stays put.
		if lines := strings.Fields(string(out)); len(lines) > 0 {
			return lines[len(lines)-1], tierFirstCommit
		}
	}
	return main, tierPath
}

// normalizeRemote reduces the forms git accepts for one remote to a single
// string, so an ssh clone and an https clone of the same project agree.
func normalizeRemote(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.LastIndex(s, "@"); i >= 0 {
		s = s[i+1:] // strips the ssh user and any embedded credentials
	}
	s = strings.Replace(s, ":", "/", 1) // git@host:x/y → host/x/y
	s = strings.TrimSuffix(s, "/")
	return strings.TrimSuffix(s, ".git")
}

// repoKey hashes a repo's identity into the token that names its store. The
// result is memoised in the runtime dir: statusline runs on every Claude turn
// and already spends two git subprocesses, so resolving the identity afresh
// each time is a cost paid on a hot path for an answer that does not change.
func repoKey(repoPath string) string {
	main := mainWorktree(repoPath)
	memo := repoKeyMemoPath(main)
	if key, ok := readKeyMemo(memo, main); ok {
		return key
	}
	id, _ := repoIdentity(repoPath)
	sum := sha256.Sum256([]byte(id))
	key := hex.EncodeToString(sum[:4])
	if err := os.MkdirAll(filepath.Dir(memo), 0o700); err == nil {
		_ = os.WriteFile(memo, []byte(key+"\n"), 0o600)
	}
	return key
}

// repoKeyMemoPath is where a resolved key is cached, addressed by the main
// worktree path — the one thing already known before resolution starts.
func repoKeyMemoPath(main string) string {
	sum := sha256.Sum256([]byte(main))
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "hive", "repokey-"+hex.EncodeToString(sum[:4]))
}

// readKeyMemo returns a memoised key if it is still trustworthy. A memo written
// before the repo's identity changed would pin the old key forever and silently
// strand the backlog under it, so git's config being written after the memo
// invalidates it: `git remote add` is what changes a repo's identity, and that
// is where it lands. One stat, rather than the subprocess the memo exists to
// avoid.
func readKeyMemo(memo, main string) (string, bool) {
	mi, err := os.Stat(memo)
	if err != nil {
		return "", false
	}
	if ci, err := os.Stat(filepath.Join(main, ".git", "config")); err == nil && ci.ModTime().After(mi.ModTime()) {
		return "", false
	}
	data, err := os.ReadFile(memo)
	if err != nil {
		return "", false
	}
	key := strings.TrimSpace(string(data))
	if !isRepoKey(key) {
		return "", false
	}
	return key, true
}

// isRepoKey guards against believing a truncated or corrupt memo.
func isRepoKey(s string) bool {
	if len(s) != 8 {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}

// hiveDataDir is hive's data root. Data, not runtime: the backlog has to
// survive a reboot, which is why it does not live beside the lock.
func hiveDataDir() string {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "hive")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "hive")
	}
	return filepath.Join(home, ".local", "share", "hive")
}

// todoStorePath is where a repo's backlog lives. The slug is the repo's
// directory name, present only so the store directory can be read at a glance;
// the key is the identity.
func todoStorePath(repoPath string) string {
	slug := slugify(filepath.Base(mainWorktree(repoPath)))
	return filepath.Join(hiveDataDir(), "todos", slug+"-"+repoKey(repoPath)+".md")
}

// slugify reduces a directory name to something safe in a filename.
func slugify(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-.")
	if out == "" {
		return "repo"
	}
	return out
}

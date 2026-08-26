package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os/exec"
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

// repoKey hashes a repo's identity into the token that names its store.
func repoKey(repoPath string) string {
	id, _ := repoIdentity(repoPath)
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:4])
}

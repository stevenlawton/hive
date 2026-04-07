package ui

import (
	"strings"
	"testing"
)

func TestColorizeDiff(t *testing.T) {
	diff := `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,3 +1,4 @@
 package main
+import "fmt"
-import "os"
 func main() {`

	result := ColorizeDiff(diff)
	if len(result) == 0 {
		t.Error("expected non-empty colorized output")
	}
	if !strings.Contains(result, "package main") {
		t.Error("expected context line to be present")
	}
}

func TestParseDiffStats(t *testing.T) {
	diff := `--- a/main.go
+++ b/main.go
@@ -1,3 +1,4 @@
 package main
+import "fmt"
+import "os"
-import "log"
 func main() {`

	added, removed := ParseDiffStats(diff)
	if added != 2 {
		t.Errorf("expected 2 additions, got %d", added)
	}
	if removed != 1 {
		t.Errorf("expected 1 removal, got %d", removed)
	}
}

func TestParseDiffStatsEmpty(t *testing.T) {
	added, removed := ParseDiffStats("")
	if added != 0 || removed != 0 {
		t.Errorf("expected 0/0, got %d/%d", added, removed)
	}
}

func TestDiffPaneStatsString(t *testing.T) {
	d := NewDiffPane()
	d.SetDiff("+line1\n+line2\n-line3\n context")
	if d.StatsString() != "+2/-1" {
		t.Errorf("expected '+2/-1', got '%s'", d.StatsString())
	}

	d2 := NewDiffPane()
	d2.SetDiff("context only")
	if d2.StatsString() != "" {
		t.Errorf("expected empty stats, got '%s'", d2.StatsString())
	}
}

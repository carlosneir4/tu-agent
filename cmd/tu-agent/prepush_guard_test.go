package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// setupPrePushFixture builds a temp work repo with an initial commit on
// branch "main", a temp bare repo added as remote "origin" (the default
// guarded remote), and installs scripts/pre-push-guard.sh — copied from this
// real repo, resolved via repoRoot() before any chdir — as the work repo's
// pre-push hook. Returns the work repo's absolute path.
func setupPrePushFixture(t *testing.T) string {
	t.Helper()

	base := t.TempDir()
	workDir := filepath.Join(base, "work")
	bareDir := filepath.Join(base, "public.git")

	runGitIn(t, base, "init", "--bare", "public.git")

	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir work repo: %v", err)
	}
	runGitIn(t, workDir, "init", "-b", "main")
	runGitIn(t, workDir, "config", "user.email", "tester@example.com")
	runGitIn(t, workDir, "config", "user.name", "Tester")

	readme := filepath.Join(workDir, "README.md")
	if err := os.WriteFile(readme, []byte("fixture repo\n"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	runGitIn(t, workDir, "add", "README.md")
	runGitIn(t, workDir, "commit", "-m", "initial commit")

	runGitIn(t, workDir, "remote", "add", "origin", bareDir)

	guardSrc := filepath.Join(repoRoot(), "scripts", "pre-push-guard.sh")
	guardBody, err := os.ReadFile(guardSrc)
	if err != nil {
		t.Fatalf("read pre-push-guard.sh source (%s): %v", guardSrc, err)
	}
	hookPath := filepath.Join(workDir, ".git", "hooks", "pre-push")
	if err := os.WriteFile(hookPath, guardBody, 0o755); err != nil {
		t.Fatalf("install pre-push hook: %v", err)
	}

	return workDir
}

// gitPush runs `git push <args...>` in dir with extra env vars appended,
// returning combined stdout+stderr and the command error (nil on success).
func gitPush(t *testing.T, dir string, extraEnv []string, args ...string) (string, error) {
	t.Helper()
	cmdArgs := append([]string{"push"}, args...)
	cmd := exec.Command("git", cmdArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), extraEnv...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestPrePushGuardBlocksMainPush (@s1) verifies that pushing a ref that
// updates refs/heads/main on the guarded remote ("origin" by default) is
// refused by the main-ref rule specifically, and that the rejection message
// names public-sync.sh as the correct publishing path. PREPUSH_DENY_OVERRIDE
// is set to a term absent from the fixture tree so deny-term resolution
// succeeds and the deny-match check cannot fire — isolating the main-ref
// rule as the sole possible cause of failure (a rejection via the
// deny-resolution fail-closed path, e.g. an unset override, would also
// mention public-sync.sh and falsely pass this test otherwise).
func TestPrePushGuardBlocksMainPush(t *testing.T) {
	workDir := setupPrePushFixture(t)

	out, err := gitPush(t, workDir, []string{"PREPUSH_DENY_OVERRIDE=zzz-not-in-tree"}, "origin", "HEAD:main")
	if err == nil {
		t.Fatalf("want push updating refs/heads/main on the guarded remote to fail, got success; output:\n%s", out)
	}
	if !strings.Contains(out, "public main branch") {
		t.Fatalf("want push rejection to cite the main-ref rule (\"public main branch\"), got:\n%s", out)
	}
	if !strings.Contains(out, "public-sync.sh") {
		t.Fatalf("want push rejection to name public-sync.sh, got:\n%s", out)
	}
}

// TestPrePushGuardBlocksDenyTerm (@s2) verifies that a push whose tree
// contains a file matching PREPUSH_DENY_OVERRIDE is refused, naming the
// offending file. Uses only a generic fake term, never a real internal
// codename.
func TestPrePushGuardBlocksDenyTerm(t *testing.T) {
	workDir := setupPrePushFixture(t)
	runGitIn(t, workDir, "checkout", "-b", "feature/x")

	leaky := filepath.Join(workDir, "leaky.txt")
	if err := os.WriteFile(leaky, []byte("this file mentions fakecorp internally\n"), 0o644); err != nil {
		t.Fatalf("write leaky.txt: %v", err)
	}
	runGitIn(t, workDir, "add", "leaky.txt")
	runGitIn(t, workDir, "commit", "-m", "add leaky file")

	out, err := gitPush(t, workDir, []string{"PREPUSH_DENY_OVERRIDE=fakecorp"}, "origin", "feature/x")
	if err == nil {
		t.Fatalf("want push containing a deny-term match to fail, got success; output:\n%s", out)
	}
	if !strings.Contains(out, "leaky.txt") {
		t.Fatalf("want push rejection to name the offending file leaky.txt, got:\n%s", out)
	}
}

// TestPrePushGuardAllowsCleanBranch (@s3) verifies that a feature-branch
// push containing no deny-term match succeeds, even with an active
// PREPUSH_DENY_OVERRIDE.
func TestPrePushGuardAllowsCleanBranch(t *testing.T) {
	workDir := setupPrePushFixture(t)
	runGitIn(t, workDir, "checkout", "-b", "feature/clean")

	clean := filepath.Join(workDir, "clean.txt")
	if err := os.WriteFile(clean, []byte("nothing sensitive in here\n"), 0o644); err != nil {
		t.Fatalf("write clean.txt: %v", err)
	}
	runGitIn(t, workDir, "add", "clean.txt")
	runGitIn(t, workDir, "commit", "-m", "add clean file")

	out, err := gitPush(t, workDir, []string{"PREPUSH_DENY_OVERRIDE=fakecorp"}, "origin", "feature/clean")
	if err != nil {
		t.Fatalf("want clean feature-branch push to succeed, got error: %v\noutput:\n%s", err, out)
	}
}

// TestPrePushGuardInstaller (@s4) verifies that running
// scripts/install-pre-push.sh with the current directory set to a target
// repo places an executable pre-push hook at .git/hooks/pre-push in that
// repo.
func TestPrePushGuardInstaller(t *testing.T) {
	dir := t.TempDir()
	runGitIn(t, dir, "init")

	installerSrc := filepath.Join(repoRoot(), "scripts", "install-pre-push.sh")
	cmd := exec.Command(installerSrc)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run install-pre-push.sh (%s): %v\n%s", installerSrc, err, out)
	}

	hookPath := filepath.Join(dir, ".git", "hooks", "pre-push")
	info, err := os.Stat(hookPath)
	if err != nil {
		t.Fatalf("stat installed hook %s: %v", hookPath, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("installed hook %s is not executable: mode=%v", hookPath, info.Mode())
	}
}

// TestPublicSyncExcludesGuardScripts (@s5) verifies that
// scripts/public-sync.sh's EXCLUDE array lists both new guard scripts, so
// neither one — which carries or references the DENY machinery — is ever
// staged into the published tree.
func TestPublicSyncExcludesGuardScripts(t *testing.T) {
	path := filepath.Join(repoRoot(), "scripts", "public-sync.sh")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read public-sync.sh: %v", err)
	}

	re := regexp.MustCompile(`(?s)EXCLUDE=\(([^)]*)\)`)
	m := re.FindStringSubmatch(string(data))
	if m == nil {
		t.Fatalf("EXCLUDE array not found in %s", path)
	}
	excludeList := m[1]

	for _, want := range []string{"scripts/pre-push-guard.sh", "scripts/install-pre-push.sh"} {
		if !strings.Contains(excludeList, want) {
			t.Errorf("want public-sync.sh EXCLUDE to list %q, got EXCLUDE=(%s)", want, excludeList)
		}
	}
}

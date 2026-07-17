package tdd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// @s1 — Repo-wide rules load from the NEW path .tu-agent/rules/all.md, under
// the authoritative "Project rules" header.
func TestLoadProjectRulesFromAllMd(t *testing.T) {
	root := t.TempDir()
	writeRuleFile(t, root, ".tu-agent/rules/all.md", "REPO-WIDE-RULE")

	got := loadProjectRules(root, "developer")
	if !strings.Contains(got, "Project rules") {
		t.Errorf("result must contain authoritative header %q, got %q", "Project rules", got)
	}
	if !strings.Contains(got, "REPO-WIDE-RULE") {
		t.Errorf("result must contain %q, got %q", "REPO-WIDE-RULE", got)
	}
}

// @s2 — Per-role rules stack on top of rules/all.md (repo-wide first), and are
// scoped out for other roles.
func TestLoadProjectRulesAllMdComposesWithRole(t *testing.T) {
	root := t.TempDir()
	writeRuleFile(t, root, ".tu-agent/rules/all.md", "REPO-WIDE-RULE")
	writeRuleFile(t, root, ".tu-agent/rules/developer.md", "DEV-ONLY-RULE")

	dev := loadProjectRules(root, "developer")
	if !strings.Contains(dev, "REPO-WIDE-RULE") {
		t.Errorf("developer result must contain %q, got %q", "REPO-WIDE-RULE", dev)
	}
	if !strings.Contains(dev, "DEV-ONLY-RULE") {
		t.Errorf("developer result must contain %q, got %q", "DEV-ONLY-RULE", dev)
	}
	repoIdx := strings.Index(dev, "REPO-WIDE-RULE")
	devIdx := strings.Index(dev, "DEV-ONLY-RULE")
	if repoIdx == -1 || devIdx == -1 || repoIdx >= devIdx {
		t.Errorf(`"REPO-WIDE-RULE" must appear before "DEV-ONLY-RULE", got repo@%d dev@%d in %q`, repoIdx, devIdx, dev)
	}

	qa := loadProjectRules(root, "qa")
	if !strings.Contains(qa, "REPO-WIDE-RULE") {
		t.Errorf("qa result must contain %q, got %q", "REPO-WIDE-RULE", qa)
	}
	if strings.Contains(qa, "DEV-ONLY-RULE") {
		t.Errorf("qa result must NOT contain developer-only rule, got %q", qa)
	}
}

// @s3 — SeedRulesReadme creates .tu-agent/rules/README.md on a fresh repo and
// is idempotent: it never overwrites a user-edited README.
func TestSeedRulesReadme(t *testing.T) {
	t.Run("creates README when absent", func(t *testing.T) {
		root := t.TempDir()

		created, err := SeedRulesReadme(root)
		if err != nil {
			t.Fatalf("SeedRulesReadme: %v", err)
		}
		if !created {
			t.Fatalf("SeedRulesReadme on fresh repo = (%v, nil), want (true, nil)", created)
		}

		readme := filepath.Join(root, ".tu-agent", "rules", "README.md")
		data, rerr := os.ReadFile(readme)
		if rerr != nil {
			t.Fatalf("expected README at %s, read err = %v", readme, rerr)
		}
		content := string(data)
		if !strings.Contains(content, "all.md") {
			t.Errorf("README must mention %q, got %q", "all.md", content)
		}
		if !strings.Contains(content, "role") {
			t.Errorf("README must mention the per-%s files, got %q", "role", content)
		}
	})

	t.Run("does not overwrite a user-edited README", func(t *testing.T) {
		root := t.TempDir()
		const sentinel = "USER-EDITED-README-SENTINEL"
		writeRuleFile(t, root, ".tu-agent/rules/README.md", sentinel)

		created, err := SeedRulesReadme(root)
		if err != nil {
			t.Fatalf("SeedRulesReadme: %v", err)
		}
		if created {
			t.Fatalf("SeedRulesReadme with existing README = (%v, nil), want (false, nil)", created)
		}

		readme := filepath.Join(root, ".tu-agent", "rules", "README.md")
		data, rerr := os.ReadFile(readme)
		if rerr != nil {
			t.Fatalf("read README: %v", rerr)
		}
		if got := string(data); got != sentinel {
			t.Fatalf("SeedRulesReadme overwrote user-edited README: got %q, want %q", got, sentinel)
		}
	})
}

// @s4 — README.md is never read as a rules file: with only rules/README.md
// present, loadProjectRules returns "" (no header, no README text leaks).
func TestLoadProjectRulesIgnoresReadme(t *testing.T) {
	root := t.TempDir()
	writeRuleFile(t, root, ".tu-agent/rules/README.md", "README-EXPLANATORY-TEXT")

	got := loadProjectRules(root, "developer")
	if got != "" {
		t.Fatalf("loadProjectRules with only README.md = %q, want empty string", got)
	}
	if strings.Contains(got, "Project rules") {
		t.Errorf("result must NOT contain the project-rules header, got %q", got)
	}
	if strings.Contains(got, "README-EXPLANATORY-TEXT") {
		t.Errorf("README text must NOT leak into rules, got %q", got)
	}
}

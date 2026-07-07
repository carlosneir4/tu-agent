package testgen

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tu/tu-agent/internal/graph/query"
	"github.com/tu/tu-agent/internal/provider"
	"github.com/tu/tu-agent/internal/telemetry"
)

// Scaffold is the deterministic half of test gen: everything except who
// generates. Shared verbatim by the CLI pipeline and the MCP test_scaffold
// tool (spec decision 5).
type Scaffold struct {
	Target         Target      `json:"target"`
	Context        *GenContext `json:"context"`
	TestPath       string      `json:"test_path"`
	RunCommand     []string    `json:"run_command"`
	RunDir         string      `json:"run_dir"` // run cwd, repo-relative; "." = repo root
	PromptFragment string      `json:"prompt_fragment"`
}

// runDirResolver is implemented by adapters whose scoped test must run in a
// subdirectory (TSAdapter, for monorepo packages). Adapters without it run at
// the repo root.
type runDirResolver interface {
	runDir(repoRoot string, t Target) string
}

// resolveRunDir returns the adapter's run directory for t, or "." when the
// adapter does not implement runDirResolver.
func resolveRunDir(ad LanguageAdapter, repoRoot string, t Target) string {
	if r, ok := ad.(runDirResolver); ok {
		return r.runDir(repoRoot, t)
	}
	return "."
}

// BuildScaffold assembles the deterministic half for a resolved target.
func BuildScaffold(g *query.Graph, ad LanguageAdapter, repoRoot string, t Target, budget int) (*Scaffold, error) {
	testPath, err := ad.TestPath(repoRoot, t)
	if err != nil {
		return nil, fmt.Errorf("testgen.BuildScaffold: %w", err)
	}
	gc, err := BuildContext(g, repoRoot, t, budget)
	if err != nil {
		return nil, fmt.Errorf("testgen.BuildScaffold: %w", err)
	}
	runCmd, err := ad.RunCommand(repoRoot, testPath, t)
	if err != nil {
		return nil, fmt.Errorf("testgen.BuildScaffold: %w", err)
	}
	return &Scaffold{
		Target: t, Context: gc, TestPath: testPath,
		RunCommand: runCmd, RunDir: resolveRunDir(ad, repoRoot, t),
		PromptFragment: ad.PromptFragment(t, testPath),
	}, nil
}

const (
	// DefaultMaxRepair is the spec/PRD default repair budget.
	DefaultMaxRepair = 2
	// DefaultTimeout bounds one scoped test run.
	DefaultTimeout = 120 * time.Second
	distillCap     = 4 * 1024
)

// ErrVerificationFailed reports that the generated test never passed within
// the repair budget. The Result still describes what was left on disk.
var ErrVerificationFailed = errors.New("generated test failed verification")

// Runner executes argv in dir under a timeout, returning combined output.
// Injectable so unit tests never shell out.
type Runner func(ctx context.Context, dir string, argv []string, timeout time.Duration) (string, error)

// Options configures one generation run.
type Options struct {
	RepoRoot       string
	MaxRepair      int
	Timeout        time.Duration // 0 → DefaultTimeout
	Budget         int           // 0 → DefaultContextBudget
	DryRun         bool
	DiscardFailing bool
}

// Result describes what one run produced.
type Result struct {
	TestPath   string
	RunCommand []string
	Attempts   int // provider calls made
	Passed     bool
	FIXME      bool
	Discarded  bool
	Code       string // last generated source (what --dry-run prints)
}

// Pipeline wires the collaborators of test generation.
type Pipeline struct {
	Graph    *query.Graph
	Adapter  LanguageAdapter
	Provider provider.Provider
	Tel      *telemetry.Logger // nil → no telemetry (tests only)
	Run      Runner            // nil → ExecRunner
}

// commentPrefix returns the line-comment token for the FIXME marker.
// Pipeline-owned: too small to justify widening LanguageAdapter.
func commentPrefix(language string) string {
	if language == "python" {
		return "#"
	}
	return "//"
}

// Generate runs scaffold→generate→write→verify→repair for one resolved target.
func (p Pipeline) Generate(ctx context.Context, t Target, opts Options) (*Result, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}
	run := p.Run
	if run == nil {
		run = ExecRunner
	}
	if !opts.DryRun {
		if err := p.Adapter.Detect(opts.RepoRoot); err != nil {
			return nil, err
		}
	}
	sc, err := BuildScaffold(p.Graph, p.Adapter, opts.RepoRoot, t, opts.Budget)
	if err != nil {
		return nil, err
	}
	abs := filepath.Join(opts.RepoRoot, sc.TestPath)
	var original string
	originalExists := false
	if data, statErr := os.ReadFile(abs); statErr == nil {
		original, originalExists = string(data), true
	} else if !os.IsNotExist(statErr) {
		return nil, fmt.Errorf("testgen: reading %s: %w", sc.TestPath, statErr)
	}

	res := &Result{TestPath: sc.TestPath, RunCommand: sc.RunCommand}
	failure := ""
	for res.Attempts <= opts.MaxRepair {
		var userPrompt string
		if failure == "" {
			userPrompt = BuildGenerationPrompt(sc.Context, sc.PromptFragment, sc.TestPath)
		} else {
			userPrompt = BuildRepairPrompt(sc.Context, sc.PromptFragment, sc.TestPath, res.Code, failure)
		}
		text, err := p.send(ctx, userPrompt, res.Attempts)
		if err != nil {
			return nil, err
		}
		res.Attempts++
		code, err := ExtractCode(text)
		if err != nil {
			failure = "your previous response contained no code block; respond with only the complete test file in one fenced code block"
			continue
		}
		res.Code = code
		if opts.DryRun {
			return res, nil
		}
		toWrite := code
		if originalExists {
			merged, mErr := Merge(t.Language, original, code, t)
			if mErr != nil {
				failure = fmt.Sprintf("could not merge the generated test into the existing file (%v); output one complete, parseable test file with the generated tests wrapped in the required sentinels", mErr)
				continue
			}
			toWrite = merged
		}
		if err := writeTest(abs, toWrite); err != nil {
			return nil, err
		}
		out, runErr := run(ctx, filepath.Join(opts.RepoRoot, sc.RunDir), sc.RunCommand, opts.Timeout)
		noTests := strings.Contains(out, "no tests to run")
		if runErr == nil && !noTests {
			res.Passed = true
			return res, nil
		}
		if runErr != nil {
			out += "\n" + runErr.Error()
		}
		if noTests {
			out += "\nthe runner matched no generated tests: every generated test name must follow the naming rule in the prompt"
		}
		failure = DistillFailure(out, distillCap)
	}

	// Repair budget exhausted — never clobber hand-written code.
	if res.Code == "" {
		return res, fmt.Errorf("testgen: %w: provider never returned code", ErrVerificationFailed)
	}
	if opts.DiscardFailing {
		if originalExists {
			if err := writeTest(abs, original); err != nil {
				return nil, err
			}
		} else if err := os.Remove(abs); err != nil {
			return nil, fmt.Errorf("testgen: removing failed test: %w", err)
		}
		res.Discarded = true
		return res, fmt.Errorf("testgen: %w after %d attempts (file restored/discarded)", ErrVerificationFailed, res.Attempts)
	}
	var marked string
	if originalExists {
		marked = fixmeAppend(original, res.Code, t.Language, res.Attempts)
	} else {
		marked = fmt.Sprintf("%s FIXME: generated test failed verification (after %d attempts)\n%s",
			commentPrefix(t.Language), res.Attempts, res.Code)
	}
	if err := writeTest(abs, marked); err != nil {
		return nil, err
	}
	res.FIXME = true
	return res, fmt.Errorf("testgen: %w after %d attempts (FIXME marker added)", ErrVerificationFailed, res.Attempts)
}

// fixmeAppend leaves hand-written content untouched and appends the failing
// generated test, commented out under a FIXME note so the file still compiles.
// Before commenting out, any sentinel text in generated is neutralized so this
// dead FIXME block can never be mistaken for a live gen region on a later
// merge — commentOut alone is not enough, since a commented sentinel line's
// trimmed text would otherwise still equal a live start/end line exactly.
func fixmeAppend(original, generated, language string, attempts int) string {
	cp := commentPrefix(language)
	header := fmt.Sprintf("\n\n%s FIXME: generated tests failed verification (after %d attempts); review and uncomment.\n", cp, attempts)
	neutralized := strings.ReplaceAll(generated, genStart, genStart+" (disabled)")
	neutralized = strings.ReplaceAll(neutralized, genEnd, genEnd+" (disabled)")
	return strings.TrimRight(original, "\n") + header + commentOut(neutralized, cp) + "\n"
}

// commentOut prefixes every line of code with the language's line comment.
func commentOut(code, cp string) string {
	lines := strings.Split(strings.TrimRight(code, "\n"), "\n")
	for i, ln := range lines {
		lines[i] = cp + " " + ln
	}
	return strings.Join(lines, "\n")
}

// send performs one provider call and logs it to telemetry (standing rule).
func (p Pipeline) send(ctx context.Context, user string, attempt int) (string, error) {
	start := time.Now()
	resp, err := p.Provider.Send(ctx, SystemPrompt, []provider.Message{
		{Role: "user", Blocks: []provider.Block{{Type: "text", Text: user}}},
	}, nil)
	if err != nil {
		return "", fmt.Errorf("testgen: provider send: %w", err)
	}
	if p.Tel != nil {
		entry := telemetry.Entry{
			Timestamp:    time.Now(),
			Provider:     p.Provider.Name(),
			Model:        p.Provider.Model(),
			InputTokens:  resp.Usage.InputTokens,
			OutputTokens: resp.Usage.OutputTokens,
			LatencyMS:    time.Since(start).Milliseconds(),
			CostUSD: telemetry.EstimateCost(p.Provider.Name(), p.Provider.Model(),
				resp.Usage.InputTokens, resp.Usage.OutputTokens),
			SubAgent: fmt.Sprintf("test_gen:attempt%d", attempt),
		}
		if logErr := p.Tel.Log(entry); logErr != nil {
			slog.Warn("testgen: telemetry log failed", "err", logErr)
		}
	}
	var sb strings.Builder
	for _, b := range resp.Blocks {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	return sb.String(), nil
}

// writeTest is the pipeline's single write path — nothing else in this
// package writes repo files, which is what keeps source under test safe.
func writeTest(abs, content string) error {
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return fmt.Errorf("testgen.writeTest: %w", err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		return fmt.Errorf("testgen.writeTest: %w", err)
	}
	return nil
}

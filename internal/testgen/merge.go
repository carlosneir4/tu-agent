package testgen

import (
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"regexp"
	"sort"
	"strings"
)

// errUnmergeable signals the per-language merge could not be performed
// safely. The pipeline then falls back to a FIXME-marked, commented-out
// append — never a partial or clobbering write.
var errUnmergeable = errors.New("testgen: cannot merge generated tests into existing file")

// Sentinel lines the model wraps generated tests in (Python/TS/Java), so the
// merge can splice the region without parsing function bodies. Go does not use
// them — it identifies generated funcs by name via the AST.
const (
	genStart = "tu-agent:gen:start"
	genEnd   = "tu-agent:gen:end"
)

// genStartFor and genEndFor are the per-target sentinel texts: the bare
// constant plus this target's sentinelKey, so generating target B can never
// match (and clobber) target A's region in the same file.
func genStartFor(t Target) string { return genStart + ":" + sentinelKey(t) }
func genEndFor(t Target) string   { return genEnd + ":" + sentinelKey(t) }

// Merge folds the generated test file into existing conventional-file content
// for one target, replacing the target's prior generated functions and
// unioning imports. Only generated functions are ever touched.
func Merge(language, existing, generated string, t Target) (string, error) {
	switch language {
	case "go":
		return mergeGo(existing, generated, t)
	case "python":
		return mergePython(existing, generated, t)
	case "typescript":
		return mergeTS(existing, generated, t)
	case "java":
		return mergeJava(existing, generated, t)
	default:
		return "", fmt.Errorf("testgen.Merge: no merger for language %q", language)
	}
}

// --- stubs replaced in Tasks 2–5; until then every present-file merge falls
// back to the safe FIXME append. ---

// mergeGo splices the generated marker funcs into the existing file by byte
// range: old marker funcs (incl. their doc comments) are cut from the original
// text, missing imports are inserted into the existing import block, and the
// generated funcs are appended. Everything hand-written — comments, build tags,
// formatting — stays byte-identical (modulo a final gofmt pass). format.Source
// (gofmt) never drops comments or build tags, which is why this splice is safe
// where the old parse-and-reprint reassembly (splitGo) was not: reprinting an
// *ast.File node with format.Node loses file-level comments (header, //go:build)
// that are attached to the file rather than to any single declaration.
func mergeGo(existing, generated string, t Target) (string, error) {
	marker := goGenPrefix(t)
	fset := token.NewFileSet()
	exFile, err := parser.ParseFile(fset, "existing.go", existing, parser.ParseComments)
	if err != nil {
		return "", fmt.Errorf("%w: existing: %v", errUnmergeable, err)
	}
	genFset := token.NewFileSet()
	genFile, err := parser.ParseFile(genFset, "generated.go", generated, parser.ParseComments)
	if err != nil {
		return "", fmt.Errorf("%w: generated: %v", errUnmergeable, err)
	}

	// 1. Collect generated marker funcs (text by byte range) + their imports.
	// genImports is keyed by Path.Value (for membership/de-dup) but stores the
	// full entry text (e.g. `x "strings"`, `_ "embed"`) so an alias, blank, or
	// dot import survives the merge instead of being flattened to a bare path.
	var genDecls []string
	genImports := map[string]string{}
	for _, d := range genFile.Decls {
		switch dd := d.(type) {
		case *ast.FuncDecl:
			if dd.Recv == nil && isGenFuncName(dd.Name.Name, marker) {
				start, end := declRange(genFset, dd)
				genDecls = append(genDecls, strings.TrimSpace(generated[start:end]))
			}
		case *ast.GenDecl:
			if dd.Tok == token.IMPORT {
				for _, sp := range dd.Specs {
					is := sp.(*ast.ImportSpec)
					genImports[is.Path.Value] = importSpecText(is)
				}
			}
		}
	}
	if len(genDecls) == 0 {
		return "", fmt.Errorf("%w: generated has no %s* function", errUnmergeable, marker)
	}

	// 2. Cut old marker funcs from the ORIGINAL text (reverse order keeps offsets valid).
	// existingImports is keyed by Path.Value the same way as genImports, so a
	// path already present — in whatever aliased/blank/plain form the existing
	// file uses — is never re-inserted; the existing file's form always wins on
	// conflict.
	type cut struct{ start, end int }
	var cuts []cut
	existingImports := map[string]string{}
	var importDecl *ast.GenDecl
	for _, d := range exFile.Decls {
		switch dd := d.(type) {
		case *ast.FuncDecl:
			if dd.Recv == nil && isGenFuncName(dd.Name.Name, marker) {
				start, end := declRange(fset, dd)
				cuts = append(cuts, cut{start, end})
			}
		case *ast.GenDecl:
			if dd.Tok == token.IMPORT {
				importDecl = dd
				for _, sp := range dd.Specs {
					is := sp.(*ast.ImportSpec)
					existingImports[is.Path.Value] = importSpecText(is)
				}
			}
		}
	}
	out := existing
	for i := len(cuts) - 1; i >= 0; i-- {
		out = out[:cuts[i].start] + out[cuts[i].end:]
	}

	// 3. Insert missing imports into the existing block (or add one). missing
	// holds full entry text (preserving any alias/blank/dot qualifier), not
	// bare paths — a path already present in existingImports is skipped
	// regardless of which form the generated side used.
	var missing []string
	for p, text := range genImports {
		if _, ok := existingImports[p]; !ok {
			missing = append(missing, text)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		hasBlock := importDecl != nil && importDecl.Lparen.IsValid()
		out, err = insertImports(out, hasBlock, missing)
		if err != nil {
			return "", fmt.Errorf("%w: imports: %v", errUnmergeable, err)
		}
	}

	// 4. Append the generated funcs and gofmt the result.
	out = strings.TrimRight(out, "\n") + "\n\n" + strings.Join(genDecls, "\n\n") + "\n"
	formatted, err := format.Source([]byte(out))
	if err != nil {
		return "", fmt.Errorf("%w: format: %v", errUnmergeable, err)
	}
	return string(formatted), nil
}

// declRange returns the byte offsets of d including its doc comment, so a cut
// or extraction never leaves an orphaned doc comment behind (or, for the
// generated side, always brings the doc comment along).
func declRange(fset *token.FileSet, d *ast.FuncDecl) (int, int) {
	start := d.Pos()
	if d.Doc != nil {
		start = d.Doc.Pos()
	}
	return fset.Position(start).Offset, fset.Position(d.End()).Offset
}

// importSpecText returns the full import entry text for sp: `name "path"`
// when sp has a name (alias, blank `_`, or dot `.` import), else the bare
// `"path"`. Keying import maps by Path.Value while storing this text is what
// lets mergeGo de-dup on path but still emit the qualifier the generated or
// existing file actually used, instead of silently flattening every import to
// its bare path.
func importSpecText(sp *ast.ImportSpec) string {
	if sp.Name != nil {
		return sp.Name.Name + " " + sp.Path.Value
	}
	return sp.Path.Value
}

// insertImports textually inserts entries (full import spec text, e.g.
// `"fmt"` or `x "strings"` or `_ "embed"`) into src's import block, without
// reprinting any AST. When hasBlock is true, src has a `import (...)` block:
// insertImports re-parses src to find that GenDecl's closing paren and inserts
// one entry per line just before it. Otherwise (a single `import "x"` line, or
// no import at all) it inserts a brand-new `import (...)` block right after
// the `package X` line.
func insertImports(src string, hasBlock bool, entries []string) (string, error) {
	if hasBlock {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "x.go", src, parser.ParseComments)
		if err != nil {
			return "", fmt.Errorf("insertImports: reparse: %w", err)
		}
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.IMPORT || !gd.Lparen.IsValid() {
				continue
			}
			closeOffset := fset.Position(gd.Rparen).Offset
			var ins strings.Builder
			for _, e := range entries {
				ins.WriteString("\t")
				ins.WriteString(e)
				ins.WriteString("\n")
			}
			return src[:closeOffset] + ins.String() + src[closeOffset:], nil
		}
		return "", fmt.Errorf("insertImports: no import block found despite hasBlock=true")
	}

	// Single non-paren import, or no import at all: add a fresh block after
	// the package clause.
	re := regexp.MustCompile(`(?m)^package \w+.*$`)
	loc := re.FindStringIndex(src)
	if loc == nil {
		return "", fmt.Errorf("insertImports: no package clause found")
	}
	var b strings.Builder
	b.WriteString("\n\nimport (\n")
	for _, e := range entries {
		b.WriteString("\t")
		b.WriteString(e)
		b.WriteString("\n")
	}
	b.WriteString(")")
	return src[:loc[1]] + b.String() + src[loc[1]:], nil
}

// isGenFuncName reports whether name is a generated test func for this target:
// exactly the marker, or the marker followed by "_" (sub-case). A name where
// the marker prefix is followed by another letter (e.g. a hand-written
// "TestStoreSave_GenEdgeCase") is NOT generated and must be preserved.
func isGenFuncName(name, marker string) bool {
	return name == marker || strings.HasPrefix(name, marker+"_")
}

func pyIsImport(line string) bool {
	return strings.HasPrefix(line, "import ") || strings.HasPrefix(line, "from ")
}

func mergePython(existing, generated string, t Target) (string, error) {
	_, region, _, ok := splitRegion(generated, "#", t)
	if !ok {
		return "", fmt.Errorf("%w: generated missing %s/%s sentinels", errUnmergeable, genStartFor(t), genEndFor(t))
	}
	genImports, _ := stripLineImports(generated, pyIsImport)
	exImports, exBody := stripLineImports(removeRegion(existing, "#", t), pyIsImport)
	imports := unionStrings(exImports, genImports)

	var b strings.Builder
	if len(imports) > 0 {
		b.WriteString(strings.Join(imports, "\n"))
		b.WriteString("\n\n")
	}
	if body := strings.TrimSpace(exBody); body != "" {
		b.WriteString(body)
		b.WriteString("\n\n\n")
	}
	b.WriteString(strings.TrimSpace(region))
	b.WriteString("\n")
	return b.String(), nil
}
func tsIsImport(line string) bool { return strings.HasPrefix(line, "import ") }

func mergeTS(existing, generated string, t Target) (string, error) {
	_, region, _, ok := splitRegion(generated, "//", t)
	if !ok {
		return "", fmt.Errorf("%w: generated missing %s/%s sentinels", errUnmergeable, genStartFor(t), genEndFor(t))
	}
	genImports, _ := stripLineImports(generated, tsIsImport)
	exImports, exBody := stripLineImports(removeRegion(existing, "//", t), tsIsImport)
	imports := unionStrings(exImports, genImports)

	var b strings.Builder
	if len(imports) > 0 {
		b.WriteString(strings.Join(imports, "\n"))
		b.WriteString("\n\n")
	}
	if body := strings.TrimSpace(exBody); body != "" {
		b.WriteString(body)
		b.WriteString("\n\n")
	}
	b.WriteString(strings.TrimSpace(region))
	b.WriteString("\n")
	return b.String(), nil
}
func javaIsImport(line string) bool { return strings.HasPrefix(line, "import ") }

// javaPackageLine returns the first `package ...;` line and the source with it
// removed.
func javaPackageLine(src string) (pkg, rest string) {
	lines := strings.Split(src, "\n")
	for i, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), "package ") {
			pkg = strings.TrimSpace(ln)
			return pkg, strings.Join(append(append([]string{}, lines[:i]...), lines[i+1:]...), "\n")
		}
	}
	return "", src
}

// mergeJava assumes the conventional single-top-level-class file (spec §10:
// multi-class Java test files are out of scope). The generated region is
// inserted before the file's final "}" — correct when that brace closes the
// test class (including after any @Nested/inner classes). If a hand-written
// SECOND top-level class follows the test class, the region would land in the
// wrong class; that mis-merge is caught downstream because the generated test
// fails to compile and the pipeline falls back to the FIXME marker rather than
// silently shipping it.
func mergeJava(existing, generated string, t Target) (string, error) {
	_, region, _, ok := splitRegion(generated, "//", t)
	if !ok {
		return "", fmt.Errorf("%w: generated missing %s/%s sentinels", errUnmergeable, genStartFor(t), genEndFor(t))
	}
	exPkg, exNoPkg := javaPackageLine(existing)
	genPkg, _ := javaPackageLine(generated)
	pkg := exPkg
	if pkg == "" {
		pkg = genPkg
	}
	exImports, exAfterImports := stripLineImports(exNoPkg, javaIsImport)
	genImports, _ := stripLineImports(generated, javaIsImport)
	imports := unionStrings(exImports, genImports)

	// Class body: drop any prior gen region FOR THIS TARGET ONLY, then insert
	// the new region before the final closing brace — methods stay inside the
	// class without parsing method bodies. Another target's suffixed region
	// (a different key) does not match and survives untouched.
	body := removeRegion(exAfterImports, "//", t)
	idx := strings.LastIndex(body, "}")
	if idx < 0 {
		return "", fmt.Errorf("%w: existing class has no closing brace", errUnmergeable)
	}
	bodyWithRegion := strings.TrimRight(body[:idx], "\n") + "\n" + strings.TrimSpace(region) + "\n" + body[idx:]

	var b strings.Builder
	if pkg != "" {
		b.WriteString(pkg)
		b.WriteString("\n\n")
	}
	if len(imports) > 0 {
		b.WriteString(strings.Join(imports, "\n"))
		b.WriteString("\n\n")
	}
	b.WriteString(strings.TrimSpace(bodyWithRegion))
	b.WriteString("\n")
	return b.String(), nil
}

// splitRegion locates t's per-target sentinel-delimited region in src. A
// region opens ONLY on a line whose strings.TrimSpace equals exactly
// `<commentPrefix> <genStartFor(t)>` (and closes the same way for
// genEndFor(t)) — a sentinel merely mentioned inside a string literal, prose,
// or a fixmeAppend-neutralized FIXME block (which mangles the sentinel text
// by construction) never matches. Only a BALANCED start/end pair counts: a
// duplicate start before the matching end, or an end with no preceding start,
// makes the region unbalanced and splitRegion fails closed (ok=false) rather
// than guessing — callers surface that as errUnmergeable, never a delete.
//
// Back-compat: when src has no suffixed pair for t (checked first), a LEGACY
// bare `<commentPrefix> tu-agent:gen:start` / `...:gen:end` pair (no suffix)
// is treated as t's region — a one-time migration; the next merge re-emits
// the suffixed form via genStartFor/genEndFor.
func splitRegion(src, commentPrefix string, t Target) (before, region, after string, ok bool) {
	start := commentPrefix + " " + genStartFor(t)
	end := commentPrefix + " " + genEndFor(t)
	if b, r, a, ok := splitRegionExact(src, start, end); ok {
		return b, r, a, true
	}
	legacyStart := commentPrefix + " " + genStart
	legacyEnd := commentPrefix + " " + genEnd
	return splitRegionExact(src, legacyStart, legacyEnd)
}

// splitRegionExact implements the exact-trimmed-line, balanced-pair matching
// splitRegion documents, for one concrete start/end line pair.
func splitRegionExact(src, start, end string) (before, region, after string, ok bool) {
	lines := strings.Split(src, "\n")
	s, e := -1, -1
	for i, ln := range lines {
		switch strings.TrimSpace(ln) {
		case start:
			if s >= 0 {
				return src, "", "", false // duplicate start before a matching end: unbalanced
			}
			s = i
		case end:
			if s < 0 || e >= 0 {
				return src, "", "", false // end with no open start, or a second end: unbalanced
			}
			e = i
		}
	}
	if s < 0 || e < 0 {
		return src, "", "", false
	}
	return strings.Join(lines[:s], "\n"), strings.Join(lines[s:e+1], "\n"), strings.Join(lines[e+1:], "\n"), true
}

// removeRegion returns src with t's sentinel region removed (per splitRegion's
// per-target, exact-line, back-compat rules); src unchanged when none is
// present. Another target's suffixed region never matches and is preserved.
func removeRegion(src, commentPrefix string, t Target) string {
	before, _, after, ok := splitRegion(src, commentPrefix, t)
	if !ok {
		return src
	}
	return strings.TrimRight(before, "\n") + "\n" + strings.TrimLeft(after, "\n")
}

// stripLineImports splits src into top-level import lines (per isImport) and
// the remaining body, preserving order.
func stripLineImports(src string, isImport func(string) bool) (imports []string, body string) {
	var rest []string
	for _, ln := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(ln)
		indented := len(ln) > 0 && (ln[0] == ' ' || ln[0] == '\t')
		if !indented && isImport(trimmed) {
			imports = append(imports, trimmed)
			continue
		}
		rest = append(rest, ln)
	}
	return imports, strings.Join(rest, "\n")
}

// unionStrings returns a followed by every element of b not already present.
func unionStrings(a, b []string) []string {
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, s := range append(append([]string{}, a...), b...) {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

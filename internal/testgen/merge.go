package testgen

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
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

func mergeGo(existing, generated string, t Target) (string, error) {
	marker := goGenPrefix(t)
	exPkg, exImports, exDecls, err := splitGo(existing, marker, false)
	if err != nil {
		return "", fmt.Errorf("%w: existing: %v", errUnmergeable, err)
	}
	_, genImports, genDecls, err := splitGo(generated, marker, true)
	if err != nil {
		return "", fmt.Errorf("%w: generated: %v", errUnmergeable, err)
	}
	if strings.TrimSpace(genDecls) == "" {
		return "", fmt.Errorf("%w: generated has no %s* function", errUnmergeable, marker)
	}
	imports := unionStrings(exImports, genImports)
	var b strings.Builder
	b.WriteString(exPkg)
	b.WriteString("\n\n")
	if len(imports) > 0 {
		b.WriteString("import (\n")
		for _, im := range imports {
			b.WriteString("\t")
			b.WriteString(im)
			b.WriteString("\n")
		}
		b.WriteString(")\n\n")
	}
	if exDecls != "" {
		b.WriteString(exDecls)
		b.WriteString("\n\n")
	}
	b.WriteString(genDecls)
	out, err := format.Source([]byte(b.String()))
	if err != nil {
		return "", fmt.Errorf("%w: format: %v", errUnmergeable, err)
	}
	return string(out), nil
}

// splitGo parses src and returns the package clause, the import specs (source
// fragments like `"fmt"` or `m "math"`), and the reprinted top-level
// declarations. With onlyMarker true, decls holds only top-level funcs whose
// name starts with marker; otherwise it holds every non-import decl EXCEPT
// those marker funcs.
func splitGo(src, marker string, onlyMarker bool) (pkg string, imports []string, decls string, err error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, parser.ParseComments)
	if err != nil {
		return "", nil, "", err
	}
	pkg = "package " + f.Name.Name
	var buf bytes.Buffer
	for _, d := range f.Decls {
		if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
			for _, spec := range gd.Specs {
				is := spec.(*ast.ImportSpec)
				txt := is.Path.Value
				if is.Name != nil {
					txt = is.Name.Name + " " + txt
				}
				imports = append(imports, txt)
			}
			continue
		}
		isMarker := false
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && isGenFuncName(fd.Name.Name, marker) {
			isMarker = true
		}
		if isMarker != onlyMarker {
			continue
		}
		var one bytes.Buffer
		if err := format.Node(&one, fset, d); err != nil {
			return "", nil, "", err
		}
		buf.WriteString(one.String())
		buf.WriteString("\n\n")
	}
	return pkg, imports, strings.TrimRight(buf.String(), "\n"), nil
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
	_, region, _, ok := splitRegion(generated)
	if !ok {
		return "", fmt.Errorf("%w: generated missing %s/%s sentinels", errUnmergeable, genStart, genEnd)
	}
	genImports, _ := stripLineImports(generated, pyIsImport)
	exImports, exBody := stripLineImports(removeRegion(existing), pyIsImport)
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
	_, region, _, ok := splitRegion(generated)
	if !ok {
		return "", fmt.Errorf("%w: generated missing %s/%s sentinels", errUnmergeable, genStart, genEnd)
	}
	genImports, _ := stripLineImports(generated, tsIsImport)
	exImports, exBody := stripLineImports(removeRegion(existing), tsIsImport)
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
	_, region, _, ok := splitRegion(generated)
	if !ok {
		return "", fmt.Errorf("%w: generated missing %s/%s sentinels", errUnmergeable, genStart, genEnd)
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

	// Class body: drop any prior gen region, then insert the new region before
	// the final closing brace — methods stay inside the class without parsing
	// method bodies.
	body := removeRegion(exAfterImports)
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

// splitRegion locates the sentinel-delimited generated region in src.
func splitRegion(src string) (before, region, after string, ok bool) {
	lines := strings.Split(src, "\n")
	s, e := -1, -1
	for i, ln := range lines {
		if s < 0 && strings.Contains(ln, genStart) {
			s = i
		}
		if strings.Contains(ln, genEnd) {
			e = i
		}
	}
	if s < 0 || e < s {
		return src, "", "", false
	}
	return strings.Join(lines[:s], "\n"), strings.Join(lines[s:e+1], "\n"), strings.Join(lines[e+1:], "\n"), true
}

// removeRegion returns src with any sentinel region removed; src unchanged when
// none is present.
func removeRegion(src string) string {
	before, _, after, ok := splitRegion(src)
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

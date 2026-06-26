package testgen

import (
	"regexp"
	"strings"
)

// Generated-test marker helpers. The marker follows each language's own
// identifier conventions; detection is anchored to the specific target, so a
// merge only ever touches the functions we generated for that target.

// goGenPrefix is the mandatory name prefix for generated Go test funcs:
// "TestStoreSave" -> "TestStoreSave_Gen".
func goGenPrefix(t Target) string { return t.TestFuncPrefix() + "_Gen" }

// pyGenStem is the snake_case stem shared by generated pytest funcs:
// "Store.Save" -> "store_save_gen".
func pyGenStem(t Target) string { return snakeCase(t.Name) + "_gen" }

// pyGenPrefix is the full generated-func name prefix: "test_store_save_gen".
func pyGenPrefix(t Target) string { return "test_" + pyGenStem(t) }

// javaMethod returns the camelCase method name from a "Class.method" target.
func javaMethod(t Target) string {
	if i := strings.LastIndex(t.Name, "."); i >= 0 {
		return t.Name[i+1:]
	}
	return t.Name
}

// javaGenPrefix is the generated JUnit method-name prefix (camelCase, no
// underscores): "placeOrder" -> "placeOrderGen".
func javaGenPrefix(t Target) string { return javaMethod(t) + "Gen" }

// tsGenTitle is the describe-block title that holds generated TS tests:
// "Store.save (gen)".
func tsGenTitle(t Target) string { return t.Name + " (gen)" }

// tsGenRunPattern scopes a vitest/jest -t run to this target's gen tests.
func tsGenRunPattern(t Target) string { return regexp.QuoteMeta(t.Name) + `.*\(gen\)` }

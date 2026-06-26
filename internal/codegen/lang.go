package codegen

import "fmt"

// SupportedLanguages are the languages with dedicated agent templates.
var SupportedLanguages = []string{"go", "java", "python", "typescript"}

// ResolveLanguage decides the project language. A non-empty flag wins but must
// be a supported language. Otherwise the language is detected from filePaths.
// When the repo is empty (no recognized files) and no flag is given, it returns
// an error instructing the user to pass --lang.
func ResolveLanguage(flag string, filePaths []string) (string, error) {
	if flag != "" {
		for _, l := range SupportedLanguages {
			if flag == l {
				return flag, nil
			}
		}
		return "", fmt.Errorf("codegen.ResolveLanguage: unsupported --lang %q (supported: %v)", flag, SupportedLanguages)
	}
	lang := DetectPrimaryLanguage(filePaths)
	if lang == "unknown" {
		return "", fmt.Errorf("codegen.ResolveLanguage: no recognized source files; pass --lang (one of %v) for an empty/new repo", SupportedLanguages)
	}
	return lang, nil
}

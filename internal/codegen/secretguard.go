package codegen

import (
	"regexp"
)

// secretPathPattern matches file paths the secret-guard refuses to read or
// modify (the Write/Edit tool path and the Read/Write deny rules).
const secretPathPattern = `(^|/)(\.env($|\.)|secrets/|\.ssh/|\.aws/|\.gnupg/|\.config/gcloud/|id_rsa|id_ed25519|id_ecdsa)|\.(pem|key)$`

// secretPathRe is secretPathPattern compiled once for IsSecretPath.
var secretPathRe = regexp.MustCompile(secretPathPattern)

// IsSecretPath reports whether a single file path is a secret/credential file.
func IsSecretPath(p string) bool {
	return secretPathRe.MatchString(p)
}

// secretCommandPattern matches a secret/credential path mentioned anywhere in a
// Bash command (block-on-mention). Directory and key-name tokens match at a
// shell-ish boundary (start, space, /, ~, quote, =); .env requires a
// trailing path boundary so a commit message mentioning ".env" does not trip it.
const secretCommandPattern = `(^|[\s/~"'=])(\.ssh/|\.aws/|\.gnupg/|\.config/gcloud/|secrets/|id_rsa|id_ed25519|id_ecdsa|\.env($|[./]))`

// secretCommandExtPattern matches .pem and .key file extensions with a trailing
// path boundary (end-of-string, whitespace, or quote/slash) but no leading
// boundary requirement, so extensions on arbitrary filenames are caught.
const secretCommandExtPattern = `\.(pem|key)($|[\s"'/])`

// secretCommandRe and secretCommandExtRe are compiled once for CommandTouchesSecret.
var (
	secretCommandRe    = regexp.MustCompile(secretCommandPattern)
	secretCommandExtRe = regexp.MustCompile(secretCommandExtPattern)
)

// CommandTouchesSecret reports whether a Bash command names a secret/credential
// path. It is a string scan, not a shell parser: deny-wins on mention.
func CommandTouchesSecret(command string) bool {
	return secretCommandRe.MatchString(command) || secretCommandExtRe.MatchString(command)
}

// sensitiveEnvMarker matches the secret-bearing segment of an environment
// variable name (API_KEY, TOKEN, SECRET, …). Kept as a fragment so it can be
// reused inside the expansion, printenv, and dump patterns below.
const sensitiveEnvMarker = `(?:API_?KEY|APIKEY|SECRET|TOKEN|PASSWORD|PASSWD|PASSPHRASE|PRIVATE_?KEY|ACCESS_?KEY|CREDENTIALS?)`

var (
	// printBuiltinRe matches echo/printf appearing as a command word (start, or
	// after a separator), i.e. a command that writes its arguments to stdout.
	printBuiltinRe = regexp.MustCompile("(?i)(^|[\\s;&|(`])(echo|printf)([\\s]|$)")
	// envSecretExpansionRe matches a $VAR / ${VAR…} expansion whose name carries a
	// secret marker — the value that would reach stdout if printed.
	envSecretExpansionRe = regexp.MustCompile(`(?i)\$\{?[A-Z0-9_]*` + sensitiveEnvMarker)
	// printenvSecretRe matches `printenv <SECRET_NAME>`, a direct value print.
	printenvSecretRe = regexp.MustCompile("(?i)(^|[\\s;&|(`])printenv\\s+[A-Z0-9_]*" + sensitiveEnvMarker)
	// envDumpRe matches a bare `env`/`printenv` that dumps EVERY variable (no
	// run-target follows): end-of-command, a pipe, a redirect, or a separator.
	// `env FOO=bar cmd` / `env cmd` (a run-target follows) do not match.
	envDumpRe = regexp.MustCompile("(?i)(^|[;&|(`])[ \\t]*(env|printenv)[ \\t]*($|[|>;&\\n])")
)

// CommandExposesEnvSecret reports whether a Bash command could print a secret
// environment variable's VALUE to stdout — the exposure class CommandTouchesSecret
// (secret FILES) does not cover. Deny-wins on the pattern, not a shell parse.
// It blocks: a dump of the whole environment (`env`, `printenv`, `env | …`);
// `printenv <SECRET_NAME>`; and echo/printf combined with a $-expansion of a
// secret-named variable. It deliberately does NOT block merely USING a secret
// (e.g. passing $API_KEY to curl), only printing it.
func CommandExposesEnvSecret(command string) bool {
	if envDumpRe.MatchString(command) || printenvSecretRe.MatchString(command) {
		return true
	}
	return envSecretExpansionRe.MatchString(command) && printBuiltinRe.MatchString(command)
}

var (
	// contentSecretAssignmentRe matches a secret-named variable assigned to a
	// real value inline (e.g. "API_KEY: sk-abc123", "aws_secret_access_key=wJa...").
	// The value must look credential-shaped: it starts with an alphanumeric (NOT
	// one of the shell parameter-expansion operators - + ? = } or a $-prefixed
	// expansion), then continues with token/base64 chars. This keeps the pattern
	// from tripping on ordinary prose that merely mentions a secret-adjacent word,
	// and — critically — from tripping on shell parameter-expansion defaults like
	// "${API_KEY:-UNSET}" or "${API_KEY:+altvalue}", whose value half is not a
	// credential at all.
	contentSecretAssignmentRe = regexp.MustCompile(`(?i)` + sensitiveEnvMarker + `["' ]*[:=]["' ]*[A-Za-z0-9][\w+/=.~-]{5,}`)
	// contentSecretPEMRe matches a PEM private-key header.
	contentSecretPEMRe = regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`)
	// contentSecretAWSKeyRe matches an AWS access key id.
	contentSecretAWSKeyRe = regexp.MustCompile(`AKIA[0-9A-Z]{16}`)
	// contentSecretGitHubTokenRe matches a GitHub personal-access/OAuth/app token
	// prefix (ghp_/gho_/ghu_/ghs_/ghr_) or the newer fine-grained github_pat_
	// prefix, each followed by a long token body — near-zero false positives.
	contentSecretGitHubTokenRe = regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}`)
	// contentSecretSlackTokenRe matches a Slack bot/app/user/config token prefix
	// (xoxb-/xoxa-/xoxp-/xoxr-/xoxs-) followed by a long token body.
	contentSecretSlackTokenRe = regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`)
)

// ContentLikelySecret reports whether free-text note content appears to embed a
// live secret/credential: a secret-named inline assignment, a PEM private-key
// header, or an AWS/GitHub/Slack token shape. String scan, deny-wins,
// best-effort — not exhaustive.
func ContentLikelySecret(content string) bool {
	return contentSecretAssignmentRe.MatchString(content) ||
		contentSecretPEMRe.MatchString(content) ||
		contentSecretAWSKeyRe.MatchString(content) ||
		contentSecretGitHubTokenRe.MatchString(content) ||
		contentSecretSlackTokenRe.MatchString(content)
}

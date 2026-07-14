# Go security checklist

Concrete patterns to sweep for in Go scopes. Each is a *candidate* — the
adversarial verification pass decides whether it is reported.

## Injection
- SQL built with `fmt.Sprintf`/`+` instead of placeholder args
  (`db.Query("... WHERE id = " + id)`). Grep: `Sprintf(.*SELECT|Query(.*+`.
- `exec.Command("sh", "-c", ...)` or `exec.Command("bash", "-c", ...)` with
  any interpolated input — argument-vector form (`exec.Command(bin, args...)`)
  is safe from shell injection; `-c` with concatenation is not.
- HTML rendered with `text/template` instead of `html/template` — no
  auto-escaping. Also `template.HTML(...)` casts on user data.
- Log/header injection: user input written into logs or response headers
  without stripping `\r\n`.

## Filesystem & network
- Path traversal: `filepath.Join(base, userInput)` then a
  `strings.HasPrefix(path, base)` check — bypassable (`base+"evil"`); the
  robust check is `filepath.Rel` + reject `..` components, or
  `os.OpenRoot`/`fs.Sub` confinement.
- Zip-slip: archive extraction writing entries without validating the joined
  destination stays inside the target dir.
- SSRF: `http.Get(userURL)` or any client hitting a user-supplied URL with no
  allowlist — Go's default client follows redirects and reaches link-local
  metadata endpoints.
- `&tls.Config{InsecureSkipVerify: true}` anywhere outside a test.

## Crypto & randomness
- `math/rand` (or `math/rand/v2`) for tokens, session IDs, password resets —
  must be `crypto/rand`.
- MD5/SHA-1 for password storage (want bcrypt/scrypt/argon2); ECB-style
  manual block cipher use; static IVs/nonces.
- Comparing secrets with `==` or `bytes.Equal` instead of
  `subtle.ConstantTimeCompare` (timing side channel on MACs/tokens).

## Language-specific traps
- Swallowed errors on security decisions: `ok, _ := authz.Check(...)` — a
  dropped error that defaults to allow. Grep for `, _ =` and `_ =` around
  auth/verify/validate calls.
- Unmarshaling untrusted input into `map[string]interface{}` then trusting
  type assertions; missing `DisallowUnknownFields` where extra fields change
  behavior.
- Goroutine races on auth/session state — anything concurrent touching a
  shared map of sessions/permissions without a lock (would fail `-race`).
- Integer conversion truncation on sizes/offsets from input
  (`int(userInt64)` on 32-bit, negative-to-unsigned).
- `net/http` servers without timeouts (`ReadHeaderTimeout` etc.) —
  slow-loris DoS surface; flag as LOW/hardening.

## Dependencies
- New deps in `go.mod`: typosquats of well-known module paths, `replace`
  directives pointing at forks, retracted versions. `govulncheck ./...` if
  available runs in seconds and checks reachability, not just presence.

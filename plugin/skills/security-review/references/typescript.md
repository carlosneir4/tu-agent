# TypeScript / JavaScript security checklist

Concrete patterns to sweep for in TS/JS scopes (Node services and browser
code). Each is a *candidate* — the adversarial verification pass decides
whether it is reported.

## Injection & code execution
- SQL via template literals instead of parameterized queries
  (`` db.query(`... ${id}`) ``). NoSQL injection: user objects spread into
  Mongo queries — `{ $where: ... }` / `{ password: { $ne: null } }` shapes
  arriving from `req.body` unvalidated.
- `child_process.exec(cmd + input)` — `execFile`/`spawn` with an args array
  is the safe shape. Also `execSync` in request handlers.
- `eval`, `new Function(input)`, `vm.runInContext` on user data;
  `setTimeout(string, ...)`.
- Prototype pollution: deep-merge/extend of user JSON into objects without
  guarding `__proto__`/`constructor`/`prototype` keys — corrupts every
  object downstream; also `Object.assign(target, req.body)`.

## Browser / rendering
- `innerHTML`/`outerHTML`/`insertAdjacentHTML`/`document.write` with user
  data; React `dangerouslySetInnerHTML`; framework escape hatches
  (`bypassSecurityTrust*` in Angular, `v-html` in Vue).
- URL-based XSS: user input into `href`/`src` without blocking
  `javascript:` URLs.
- `postMessage` listeners that skip the `event.origin` check; `postMessage`
  sends with `"*"` target carrying sensitive data.
- Secrets shipped to the client: API keys in frontend bundles, env vars
  exposed via client-visible prefixes (e.g. `NEXT_PUBLIC_*`,
  `VITE_*`) that were meant to be server-side.

## Node services
- Path traversal: `path.join(base, userInput)` then serving the file — an
  absolute or `..` input escapes; use `path.resolve` + containment check on
  the resolved path. Express `res.sendFile` without the `root` option.
- SSRF: `fetch(userUrl)`/axios on user-supplied URLs without an allowlist.
- Missing validation boundary: handlers consuming `req.body`/`req.query` as
  `any` with no schema (zod/joi/ajv) — every downstream sink inherits
  untrusted shapes. In TS, `as` casts on request data are the tell.
- JWT: `algorithms` not pinned on verify (alg-confusion), secrets in code,
  tokens accepted from query strings; session cookies without
  `httpOnly`/`secure`/`sameSite`.
- ReDoS: user input tested against catastrophic-backtracking regexes
  (nested quantifiers like `(a+)+`) in hot request paths.
- Weak randomness: `Math.random()` for tokens/IDs — must be
  `crypto.randomBytes`/`crypto.randomUUID`.

## Dependencies
- New deps in `package.json`: typosquats of popular names, packages with
  `postinstall` scripts, forks pinned to git URLs. Lockfile diffs deserve
  the same review as code. `npm audit` is noisy — check whether a flagged
  advisory's vulnerable function is actually reachable before reporting
  above LOW.

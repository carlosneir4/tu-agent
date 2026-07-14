# Java security checklist

Concrete patterns to sweep for in Java scopes. Each is a *candidate* — the
adversarial verification pass decides whether it is reported.

## Injection
- SQL via `Statement`/string concatenation instead of `PreparedStatement`
  with bound parameters; JPA/Hibernate `createQuery("... " + input)` — HQL
  injects too. Grep: `createStatement|createQuery(.*\+`.
- `Runtime.getRuntime().exec(...)` / `ProcessBuilder` with a single
  concatenated string or `sh -c`; argument-list form with fixed binary is
  the safe shape.
- Expression-language injection: user input reaching SpEL
  (`ExpressionParser.parseExpression`), OGNL, or template engines
  (Freemarker/Velocity/Thymeleaf inline) unescaped.
- LDAP/XPath injection where directory or XML queries concatenate input.

## Unsafe parsing & deserialization
- `ObjectInputStream.readObject()` on anything derived from the network or
  user files — Java native deserialization is code execution with the right
  gadget chain. Also Jackson `enableDefaultTyping`/`@JsonTypeInfo` with
  attacker-influenced class names, and XStream without an allowlist.
- XXE: `DocumentBuilderFactory`, `SAXParserFactory`, `XMLInputFactory` used
  without disabling DTDs/external entities
  (`disallow-doctype-decl`, `XMLConstants.FEATURE_SECURE_PROCESSING`).
- Zip-slip in archive extraction: entry names joined to a destination
  without a canonical-path containment check.

## Filesystem & network
- Path traversal: `new File(base, userInput)` checked with `startsWith` on
  the raw path — use `getCanonicalPath`/`toRealPath` before the containment
  check.
- SSRF: `URL.openConnection()`/HTTP clients on user-supplied URLs without an
  allowlist.
- Custom `TrustManager` that no-ops `checkServerTrusted`, or
  `HostnameVerifier` returning `true`.

## Framework (Spring & friends)
- Endpoints missing authorization: public `@RequestMapping` handlers where
  sibling handlers carry `@PreAuthorize`/security-config rules — the odd one
  out is the finding. Method-level checks absent when the URL-level config
  was bypassed by a new route shape.
- CSRF protection disabled (`csrf().disable()`) on state-changing,
  cookie-authenticated endpoints.
- Actuator/management endpoints exposed without auth
  (`management.endpoints.web.exposure.include=*`).
- Mass assignment: request bodies bound straight to entities that carry
  privilege fields (`role`, `isAdmin`) without a DTO or field allowlist.

## Crypto & randomness
- `java.util.Random` (or `Math.random()`) for tokens/OTPs — must be
  `SecureRandom`.
- MD5/SHA-1 for passwords (want bcrypt/argon2 via a vetted library);
  `"AES"` default (ECB) instead of an authenticated mode (GCM); static keys
  or IVs in code.
- Secrets compared with `String.equals` (timing) or logged via `toString`.

## Dependencies
- Known-vulnerable versions in the build files (old log4j 2.x with JNDI
  lookup, old Jackson/XStream/commons-collections). New deps: check
  coordinates for typosquats; `mvn dependency:tree`/OWASP dependency-check
  if configured.

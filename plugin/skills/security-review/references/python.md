# Python security checklist

Concrete patterns to sweep for in Python scopes. Each is a *candidate* — the
adversarial verification pass decides whether it is reported.

## Injection & code execution
- SQL via f-strings/`%`/`.format()` instead of driver parameters
  (`cursor.execute(f"... {user_id}")`). ORMs inject too:
  `Model.objects.raw("... " + input)`, SQLAlchemy `text()` with
  interpolation.
- `subprocess` with `shell=True` and any interpolated input; `os.system`,
  `os.popen`. List-args with `shell=False` is the safe shape.
- `eval`/`exec`/`compile` on anything user-influenced, including "safe-looking"
  uses like eval'ing config values.
- `pickle.loads` / `shelve` / `pandas.read_pickle` on untrusted data — code
  execution by design. `yaml.load` without `SafeLoader` (want
  `yaml.safe_load`).
- Template injection: Jinja2 with `autoescape=False` rendering user data;
  user input used as the *template* (not just a variable) —
  `Template(user_input).render()` is code execution.

## Filesystem & network
- Path traversal: `os.path.join(base, user_input)` — an absolute
  `user_input` **discards** `base` entirely (classic gotcha). Resolve with
  `Path.resolve()` and check containment via `Path.is_relative_to`.
- `tarfile.extractall`/`zipfile.extractall` without member validation
  (zip-slip); Python's tarfile happily writes `../../` paths pre-3.12
  filters.
- SSRF: `requests.get(user_url)`/`urllib` on user-supplied URLs without an
  allowlist.
- `verify=False` on requests/httpx calls; `ssl._create_unverified_context`.

## Language-specific traps
- `assert` used for auth/validation checks — stripped entirely under
  `python -O`; security checks must be real conditionals.
- Mutable default arguments or module-level caches holding per-user data
  (cross-user leakage in long-lived servers).
- `random` module for tokens/resets/session IDs — must be `secrets`
  (`secrets.token_urlsafe`).
- Comparing secrets with `==` instead of `hmac.compare_digest`.

## Framework (Django / Flask / FastAPI)
- Django: `DEBUG = True` in anything deployable; hardcoded `SECRET_KEY`;
  `mark_safe`/`|safe` on user data; `ALLOWED_HOSTS = ["*"]`; raw SQL as
  above; missing `csrf_protect` where cookie auth is in play.
- Flask: `app.run(debug=True)` (Werkzeug debugger = RCE); `send_file` with
  user paths (traversal, see above); session cookie with a weak/hardcoded
  secret.
- FastAPI/Pydantic: endpoints taking `dict`/`Any` bodies instead of models —
  no validation boundary; privilege fields accepted in the same model users
  submit (mass assignment).

## Dependencies
- New deps in the manifest: typosquats of popular packages
  (requests/urllib3 lookalikes), install-time code (`setup.py` with side
  effects). `pip-audit` if available checks known CVEs quickly.

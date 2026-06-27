#!/usr/bin/env python3
"""Validate dev-flow agent files.

Default (template) mode validates the curated skeletons in
plugin/agent-templates/*.md: required frontmatter, no banned tokens, an ENRICH
slot, and the __PROJECT__ token. developer/qa must also carry __TEST_COMMAND__.

`--generated <dir>` mode validates the *output* agents written by the enricher
(.claude/agents/<role>.md): no template tokens or marker comments may survive,
and no XML/wrapper tag (e.g. </content>) may leak into the Markdown body.
"""
import sys, glob, os

REQUIRED = ("name:", "description:", "tools:")
# Wrapper tags must never appear in a skeleton or a generated agent — the body
# is pure Markdown; a stray </content> means the enricher wrapped its output.
WRAPPER_TAGS = ("<content>", "</content>", "<content ")
SKELETON_FORBIDDEN = ("tool_subset", "default_model", "load_skill", "{{.") + WRAPPER_TAGS
GENERATED_FORBIDDEN = ("__PROJECT__", "__TEST_COMMAND__", "<!-- ENRICH", "{{.") + WRAPPER_TAGS
TEST_COMMAND_ROLES = {"developer", "qa"}
ROLES = {"architect", "developer", "qa", "pr-reviewer", "security-reviewer"}


def validate_templates():
    found, ok = set(), True
    for path in glob.glob("plugin/agent-templates/*.md"):
        role = os.path.basename(path)[:-3]
        found.add(role)
        text = open(path).read()
        if not text.startswith("---\n"):
            print(f"{path}: missing frontmatter"); ok = False; continue
        fm = text.split("---\n", 2)[1]
        for r in REQUIRED:
            if r not in fm:
                print(f"{path}: frontmatter missing {r}"); ok = False
        for f in SKELETON_FORBIDDEN:
            if f in text:
                print(f"{path}: forbidden token {f!r}"); ok = False
        if "<!-- ENRICH:" not in text:
            print(f"{path}: no ENRICH slot"); ok = False
        if "__PROJECT__" not in text:
            print(f"{path}: no __PROJECT__ token"); ok = False
        if role in TEST_COMMAND_ROLES and "__TEST_COMMAND__" not in text:
            print(f"{path}: {role} skeleton missing __TEST_COMMAND__ token"); ok = False
    missing = ROLES - found
    if missing:
        print(f"missing skeletons: {sorted(missing)}"); ok = False
    return ok


def validate_generated(dir_path):
    ok = True
    paths = sorted(glob.glob(os.path.join(dir_path, "*.md")))
    for path in paths:
        text = open(path).read()
        if not text.startswith("---\n"):
            print(f"{path}: missing frontmatter"); ok = False; continue
        for f in GENERATED_FORBIDDEN:
            if f in text:
                print(f"{path}: leftover/forbidden token {f!r}"); ok = False
    return ok


def fix_generated(dir_path):
    """Deterministically strip stray wrapper tags from output agents.

    A lone </content> is hallucinated garbage, safe to remove without a model.
    Leftover TEMPLATE TOKENS (__PROJECT__, __TEST_COMMAND__, ENRICH) are NOT
    fixable here — they need a value only the enricher can supply — so they are
    left for validate_generated to report and the caller to re-enrich.
    """
    for path in sorted(glob.glob(os.path.join(dir_path, "*.md"))):
        text = open(path).read()
        out, changed = [], False
        for ln in text.split("\n"):
            s = ln.strip()
            if s in ("<content>", "</content>") or s.startswith("<content "):
                changed = True  # drop the whole wrapper-only line
                continue
            new_ln = ln
            for tag in WRAPPER_TAGS:
                if tag in new_ln:
                    new_ln, changed = new_ln.replace(tag, ""), True
            out.append(new_ln)
        if changed:
            with open(path, "w") as fh:
                fh.write("\n".join(out))
            print(f"{path}: stripped stray wrapper tag(s)")


def main():
    args = sys.argv[1:]
    if args and args[0] == "--generated" and len(args) >= 2:
        dir_path = args[1]
        if "--fix" in args:
            fix_generated(dir_path)  # auto-remove wrapper tags; tokens still fail below
        ok = validate_generated(dir_path)
    else:
        ok = validate_templates()
    print("OK" if ok else "FAIL")
    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()

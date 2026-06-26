#!/usr/bin/env bash
# Set the on-disk knowledge state of the repo under test so the bench can compare
# cold / graph / learned. Skills and graph(+mcp) toggle independently.
# Usage: [ROOT=<repo>] with-learn.sh cold|graph|learned|restore
set -euo pipefail
mode="${1:?usage: with-learn.sh cold|graph|learned|restore}"
root="${ROOT:-.}"
hold="$root/.bench-holding"

SKILLS_ARTIFACTS=(".claude/skills")
GRAPH_ARTIFACTS=(".mcp.json" ".tu-agent/graph.db")

hide() {
  local a="$1"
  if [ -e "$root/$a" ]; then
    mkdir -p "$hold/$(dirname "$a")"
    mv "$root/$a" "$hold/$a"
  fi
}
show() {
  local a="$1"
  if [ -e "$hold/$a" ]; then
    mkdir -p "$root/$(dirname "$a")"
    mv "$hold/$a" "$root/$a"
  fi
}
hide_group() { local a; for a in "$@"; do hide "$a"; done; }
show_group() { local a; for a in "$@"; do show "$a"; done; }

case "$mode" in
  cold)    hide_group "${SKILLS_ARTIFACTS[@]}"; hide_group "${GRAPH_ARTIFACTS[@]}" ;;
  graph)   hide_group "${SKILLS_ARTIFACTS[@]}"; show_group "${GRAPH_ARTIFACTS[@]}" ;;
  learned) show_group "${SKILLS_ARTIFACTS[@]}"; show_group "${GRAPH_ARTIFACTS[@]}" ;;
  restore) show_group "${SKILLS_ARTIFACTS[@]}"; show_group "${GRAPH_ARTIFACTS[@]}" ;;
  *) echo "usage: with-learn.sh cold|graph|learned|restore" >&2; exit 2 ;;
esac
echo "knowledge state: $mode"

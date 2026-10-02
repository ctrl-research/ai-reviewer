#!/usr/bin/env bash
# Builds the docs site from docs/ with nebula-md.
#
#   NEBULA_BIN=/path/to/nebula scripts/build-docs.sh [output-dir]
#
# Pages are copied to a staging directory, where each line of the form
#   <!-- include: examples/<file>.yaml -->
# is replaced by that file as a fenced code block. The recipe pages stay in
# sync with the linted example workflows that way. Run from the repo root.
set -euo pipefail

nebula="${NEBULA_BIN:?set NEBULA_BIN to a nebula-md binary}"
out="${1:-site}"

stage="$(mktemp -d)"
trap 'rm -rf "$stage"' EXIT
cp -R docs/. "$stage/"

expand_includes() {
  awk '
    /^<!-- include: [A-Za-z0-9_.\/-]+ -->$/ {
      path = $0
      sub(/^<!-- include: /, "", path)
      sub(/ -->$/, "", path)
      # Only files under examples/, so a page cannot pull in arbitrary files.
      if (path !~ /^examples\// || path ~ /\.\./) {
        printf "%s: include outside examples/: %s\n", FILENAME, path > "/dev/stderr"
        exit 1
      }
      lang = path
      sub(/.*\./, "", lang)
      if (lang == "yml") lang = "yaml"
      print "```" lang
      n = 0
      while ((r = (getline line < path)) > 0) { print line; n++ }
      close(path)
      if (r < 0 || n == 0) {
        printf "%s: cannot include %s\n", FILENAME, path > "/dev/stderr"
        exit 1
      }
      print "```"
      next
    }
    { print }
  ' "$1"
}

while IFS= read -r -d '' page; do
  expand_includes "$page" > "$page.tmp"
  mv "$page.tmp" "$page"
done < <(find "$stage" -name '*.md' -print0)

if grep -rq -- '<!-- include:' "$stage"; then
  echo "unexpanded include markers (check their exact format):" >&2
  grep -rn -- '<!-- include:' "$stage" >&2
  exit 1
fi

rm -rf "$out"
NEBULA_INPUT="$stage" \
  NEBULA_OUTPUT="$out" \
  NEBULA_SITE_NAME="ego" \
  NEBULA_SITE_THEME="dark" \
  NEBULA_CLEAN_URLS="true" \
  NEBULA_GRAPH_MODE="2d" \
  "$nebula"
echo "Built docs into $out/"

#!/usr/bin/env bash

set -euo pipefail

KAIROS_SLUG="kairos-io/kairos"
# Archived, frozen at the monorepo migration (2026-08-19/21). Not queried
# for changes any more (see MONOREPO_PATHS) -- kept only as a read-only
# fallback for load_makefile_versions/load_gomod_versions when OLD_REF
# predates the migration and neither kairos-init/Makefile nor go.mod exist
# yet at that ref in kairos-io/kairos itself.
KAIROS_INIT_SLUG="kairos-io/kairos-init"

# Components whose source now lives inside kairos-io/kairos itself (the
# 2026-08-19/21 monorepo migration). They have no version of their own
# anymore -- diffed by path against the same OLD_REF/NEW_REF as "Kairos
# changes" itself, not against a separate repo or version pin.
declare -A MONOREPO_PATHS=(
  [kairos-init]="kairos-init/"
  [kairos-agent]="agent/"
  [immucore]="immucore/"
  [kairos-sdk]="sdk/"
  [kcrypt-discovery-challenger]="kcrypt/discovery/"
)

declare -A COMPONENT_SLUG_HINT=()

usage() {
  cat <<'EOF'
Usage:
  scripts/kairos-diff.sh <old-ref> <new-ref> [--output <path>]

Examples:
  scripts/kairos-diff.sh v3.7.2 v4.0.0
  scripts/kairos-diff.sh v3.7.2 v4.0.0 --output RELEASE_NOTES_v4.0.0.md
EOF
}

die() {
  printf 'Error: %s\n' "$*" >&2
  exit 1
}

gh_ready() {
  command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1
}

sanitize_author() {
  local raw="$1"
  raw="${raw// /-}"
  raw="${raw//_/\-}"
  raw="${raw,,}"
  printf '%s\n' "$raw"
}

is_filtered_author() {
  local author="$1"
  [[ "$author" == "renovate[bot]" || "$author" == "dependabot[bot]" ]]
}

component_to_slug() {
  local component="$1"
  case "$component" in
    kairos) printf '%s\n' "$KAIROS_SLUG" ;;
    provider-kairos) printf 'kairos-io/provider-kairos\n' ;;
    edgevpn) printf 'mudler/edgevpn\n' ;;
    entities) printf 'mudler/entities\n' ;;
    go-pluggable) printf 'mudler/go-pluggable\n' ;;
    yip) printf 'mudler/yip\n' ;;
    xpasswd) printf 'mauromorales/xpasswd\n' ;;
    *)
      if [[ -n "${COMPONENT_SLUG_HINT[$component]:-}" ]]; then
        printf '%s\n' "${COMPONENT_SLUG_HINT[$component]}"
      fi
      ;;
  esac
}

ensure_ref_exists_gh() {
  local slug="$1"
  local ref="$2"
  gh api "repos/${slug}/commits/${ref}" >/dev/null 2>&1
}

set_assoc_entry() {
  local map_name="$1"
  local key="$2"
  local value="$3"
  printf -v "${map_name}[$key]" '%s' "$value"
}

get_assoc_entry() {
  local map_name="$1"
  local key="$2"
  local value=""
  eval "value=\${${map_name}[\"$key\"]:-}"
  printf '%s\n' "$value"
}

get_file_content_gh() {
  local slug="$1"
  local ref="$2"
  local path="$3"
  gh api "repos/${slug}/contents/${path}?ref=${ref}" --jq '.content' | tr -d '\n' | base64 -d
}

get_file_content() {
  local slug="$1"
  local ref="$2"
  local path="$3"
  get_file_content_gh "$slug" "$ref" "$path"
}

normalize_ref_gh() {
  local slug="$1"
  local ref="$2"
  if ensure_ref_exists_gh "$slug" "$ref"; then
    printf '%s\n' "$ref"
    return 0
  fi
  if [[ "$ref" =~ -([0-9a-f]{12})$ ]]; then
    local short_hash="${BASH_REMATCH[1]}"
    if ensure_ref_exists_gh "$slug" "$short_hash"; then
      printf '%s\n' "$short_hash"
      return 0
    fi
  fi
  return 1
}

# Pre-migration fallback only: kairos-init/Makefile and go.mod did not
# exist in kairos-io/kairos before the 2026-08-19/21 merge, so a ref from
# before it has to read them from the archived kairos-io/kairos-init repo
# instead, the same way this script always did pre-migration. Resolves via
# the Dockerfile's KAIROS_INIT= pin, which is the one thing that was
# already accurate for pre-migration refs and still is.
resolve_pre_migration_init_ref() {
  local kairos_ref="$1"
  local dockerfile init_version
  dockerfile="$(get_file_content "$KAIROS_SLUG" "$kairos_ref" "images/Dockerfile")" || return 1

  local line
  while IFS= read -r line; do
    if [[ "$line" =~ ^ARG[[:space:]]+KAIROS_INIT=([^[:space:]]+) ]]; then
      init_version="${BASH_REMATCH[1]}"
      break
    fi
  done <<<"$dockerfile"
  [[ -n "${init_version:-}" ]] || return 1

  ensure_ref_exists_gh "$KAIROS_INIT_SLUG" "$init_version" || return 1
  printf '%s\n' "$init_version"
}

load_makefile_versions() {
  # kairos-init/Makefile still pins provider-kairos post-migration, but
  # AGENT_VERSION/IMMUCORE_VERSION/KCRYPT_DISCOVERY_CHALLENGER_VERSION are
  # vestigial there now: those components are monorepo paths (see
  # MONOREPO_PATHS), not external repos with a version to bump.
  #
  # EDGEVPN_VERSION is a special case: pre-migration, edgevpn's pin lived
  # ONLY in this Makefile, never in kairos-init's go.mod, so the fallback
  # branch below still needs it. Post-migration, edgevpn genuinely is a
  # go.mod dependency of the root module, and this Makefile variable has
  # already been observed drifted from it (v0.35.4 here vs. v0.35.3 in
  # go.mod) -- so it's deliberately NOT captured from the primary,
  # post-migration branch, only from the pre-migration fallback, letting
  # load_gomod_versions's root-go.mod read be authoritative post-migration.
  local kairos_ref="$1"
  local map_name="$2"
  local content
  local pre_migration=0
  content="$(get_file_content "$KAIROS_SLUG" "$kairos_ref" "kairos-init/Makefile")" || {
    local init_ref
    init_ref="$(resolve_pre_migration_init_ref "$kairos_ref")" || return 1
    content="$(get_file_content "$KAIROS_INIT_SLUG" "$init_ref" "Makefile")" || return 1
    pre_migration=1
  }

  local line value
  while IFS= read -r line; do
    case "$line" in
      "PROVIDER_KAIROS_VERSION :="*) value="${line#PROVIDER_KAIROS_VERSION := }"; set_assoc_entry "$map_name" "provider-kairos" "$value" ;;
      "EDGEVPN_VERSION :="*)
        if [[ "$pre_migration" -eq 1 ]]; then
          value="${line#EDGEVPN_VERSION := }"; set_assoc_entry "$map_name" "edgevpn" "$value"
        fi
        ;;
    esac
  done <<<"$content"
}

load_gomod_versions() {
  # Reads the monorepo's own root go.mod post-migration (there is no more
  # separate kairos-init go.mod -- it was folded into this one module).
  # This is also now the live source for edgevpn's pin, which used to be
  # read from kairos-init's Makefile and had already drifted from go.mod's
  # actual resolved version by the time this was fixed. Falls back to the
  # archived kairos-init repo's go.mod for a pre-migration ref, same as
  # load_makefile_versions above.
  local kairos_ref="$1"
  local map_name="$2"
  local content
  content="$(get_file_content "$KAIROS_SLUG" "$kairos_ref" "go.mod")" || {
    local init_ref
    init_ref="$(resolve_pre_migration_init_ref "$kairos_ref")" || return 1
    content="$(get_file_content "$KAIROS_INIT_SLUG" "$init_ref" "go.mod")" || return 1
  }

  local line module owner version rest component
  while IFS= read -r line; do
    if [[ "$line" =~ ^[[:space:]]*(github\.com/(kairos-io|mudler|mauromorales)/[^[:space:]]+)[[:space:]]+([^[:space:]]+) ]]; then
      module="${BASH_REMATCH[1]}"
      owner="${BASH_REMATCH[2]}"
      version="${BASH_REMATCH[3]}"
      rest="${module#github.com/*/}"
      component="${rest%%/*}"
      if [[ -z "$(get_assoc_entry "$map_name" "$component")" ]]; then
        set_assoc_entry "$map_name" "$component" "$version"
      fi
      if [[ -z "${COMPONENT_SLUG_HINT[$component]:-}" ]]; then
        COMPONENT_SLUG_HINT["$component"]="${owner}/${component}"
      fi
    fi
  done <<<"$content"
}

collect_changes_gh() {
  local slug="$1"
  local from_ref="$2"
  local to_ref="$3"

  local commit_lines
  commit_lines="$(gh api "repos/${slug}/compare/${from_ref}...${to_ref}" --paginate --jq '.commits[]? | "\(.sha)|\(.commit.message|split("\n")[0])|\(.commit.author.name // "")|\(.author.login // "")|\(.commit.author.email // "")"' 2>/dev/null || true)"
  [[ -z "$commit_lines" ]] && return 0

  declare -A seen_pr=()
  local line sha subject author_name author_login author_email
  local pr_line pr_number pr_title pr_author commit_author short_sha pr_ref

  while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    IFS='|' read -r sha subject author_name author_login author_email <<<"$line"

    pr_line="$(gh api -H 'Accept: application/vnd.github+json' "repos/${slug}/commits/${sha}/pulls" --jq '.[0] | select(.) | "\(.number)|\(.title)|\(.user.login)"' 2>/dev/null || true)"
    if [[ -n "$pr_line" ]]; then
      IFS='|' read -r pr_number pr_title pr_author <<<"$pr_line"
      if is_filtered_author "$pr_author"; then
        continue
      fi
      if [[ -n "$pr_number" && -z "${seen_pr[$pr_number]:-}" ]]; then
        pr_ref="[#${pr_number}](https://github.com/${slug}/pull/${pr_number})"
        printf -- '- %s by @%s in %s\n' "$pr_title" "$pr_author" "$pr_ref"
        seen_pr["$pr_number"]=1
      fi
      continue
    fi

    commit_author="$author_login"
    if [[ -z "$commit_author" || "$commit_author" == "null" ]]; then
      if [[ "$author_email" =~ ^([0-9]+\+)?([^@]+)@users\.noreply\.github\.com$ ]]; then
        commit_author="${BASH_REMATCH[2]}"
      else
        commit_author="$(sanitize_author "$author_name")"
      fi
    fi

    if is_filtered_author "$commit_author"; then
      continue
    fi

    short_sha="${sha:0:7}"
    printf -- '- %s by @%s in %s\n' "$subject" "$commit_author" "$short_sha"
  done <<<"$commit_lines"
}

section_title_for_component() {
  local component="$1"
  case "$component" in
    immucore) printf 'Immucore' ;;
    *) printf '%s' "$component" ;;
  esac
}

# Same shape as collect_changes_gh, but for a component whose source is a
# path within a monorepo rather than its own repository: every commit in
# the range is inspected (via its PR's file list, or the commit's own file
# list when it has no PR) and kept only if it actually touched path_prefix.
collect_changes_gh_path() {
  local slug="$1"
  local from_ref="$2"
  local to_ref="$3"
  local path_prefix="$4"

  local commit_lines
  commit_lines="$(gh api "repos/${slug}/compare/${from_ref}...${to_ref}" --paginate --jq '.commits[]? | "\(.sha)|\(.commit.message|split("\n")[0])|\(.commit.author.name // "")|\(.author.login // "")|\(.commit.author.email // "")"' 2>/dev/null || true)"
  [[ -z "$commit_lines" ]] && return 0

  declare -A seen_pr=()
  declare -A pr_touches=()
  local line sha subject author_name author_login author_email
  local pr_line pr_number pr_title pr_author commit_author short_sha pr_ref touched

  while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    IFS='|' read -r sha subject author_name author_login author_email <<<"$line"

    pr_line="$(gh api -H 'Accept: application/vnd.github+json' "repos/${slug}/commits/${sha}/pulls" --jq '.[0] | select(.) | "\(.number)|\(.title)|\(.user.login)"' 2>/dev/null || true)"
    if [[ -n "$pr_line" ]]; then
      IFS='|' read -r pr_number pr_title pr_author <<<"$pr_line"
      if is_filtered_author "$pr_author"; then
        continue
      fi
      if [[ -z "$pr_number" || -n "${seen_pr[$pr_number]:-}" ]]; then
        continue
      fi
      seen_pr["$pr_number"]=1

      if [[ -z "${pr_touches[$pr_number]:-}" ]]; then
        touched="$(gh api "repos/${slug}/pulls/${pr_number}/files" --paginate --jq '.[].filename' 2>/dev/null | grep -c "^${path_prefix}" || true)"
        pr_touches["$pr_number"]="${touched:-0}"
      fi
      [[ "${pr_touches[$pr_number]}" -gt 0 ]] || continue

      pr_ref="[#${pr_number}](https://github.com/${slug}/pull/${pr_number})"
      printf -- '- %s by @%s in %s\n' "$pr_title" "$pr_author" "$pr_ref"
      continue
    fi

    touched="$(gh api "repos/${slug}/commits/${sha}" --jq '.files[]?.filename' 2>/dev/null | grep -c "^${path_prefix}" || true)"
    [[ "${touched:-0}" -gt 0 ]] || continue

    commit_author="$author_login"
    if [[ -z "$commit_author" || "$commit_author" == "null" ]]; then
      if [[ "$author_email" =~ ^([0-9]+\+)?([^@]+)@users\.noreply\.github\.com$ ]]; then
        commit_author="${BASH_REMATCH[2]}"
      else
        commit_author="$(sanitize_author "$author_name")"
      fi
    fi

    if is_filtered_author "$commit_author"; then
      continue
    fi

    short_sha="${sha:0:7}"
    printf -- '- %s by @%s in %s\n' "$subject" "$commit_author" "$short_sha"
  done <<<"$commit_lines"
}

append_path_diff_section() {
  local out_file="$1"
  local component="$2"
  local path_prefix="$3"

  local heading
  heading="$(section_title_for_component "$component") changes"

  local changes
  changes="$(collect_changes_gh_path "$KAIROS_SLUG" "$OLD_REF" "$NEW_REF" "$path_prefix")"
  append_section_changes "$out_file" "$heading" "$changes"
}

append_section_changes() {
  local out_file="$1"
  local heading="$2"
  local body="$3"
  {
    printf '## %s\n' "$heading"
    if [[ -n "$body" ]]; then
      printf '%s\n' "$body"
    else
      printf -- '- No changes\n'
    fi
    printf '\n'
  } >>"$out_file"
}

append_component_section() {
  local out_file="$1"
  local component="$2"
  local old_version="$3"
  local new_version="$4"

  local heading
  heading="$(section_title_for_component "$component") changes"

  if [[ -z "$old_version" && -z "$new_version" ]]; then
    append_section_changes "$out_file" "$heading" "- No changes"
    return 0
  fi
  if [[ "$old_version" == "$new_version" ]]; then
    if [[ -n "$old_version" ]]; then
      append_section_changes "$out_file" "$heading" "- No changes (${old_version})"
    else
      append_section_changes "$out_file" "$heading" "- No changes"
    fi
    return 0
  fi
  if [[ -z "$old_version" || -z "$new_version" ]]; then
    append_section_changes "$out_file" "$heading" "- Version: ${old_version:-n/a} -> ${new_version:-n/a}\n- Unable to compare: missing one side of the version range"
    return 0
  fi

  local slug
  slug="$(component_to_slug "$component" || true)"
  if [[ -z "$slug" ]]; then
    append_section_changes "$out_file" "$heading" "- Version: ${old_version} -> ${new_version}\n- Unable to map component to GitHub repository"
    return 0
  fi

  local old_ref new_ref
  old_ref="$(normalize_ref_gh "$slug" "$old_version" || true)"
  new_ref="$(normalize_ref_gh "$slug" "$new_version" || true)"

  if [[ -z "$old_ref" || -z "$new_ref" ]]; then
    append_section_changes "$out_file" "$heading" "- Version: ${old_version} -> ${new_version}\n- Unable to resolve refs in repository"
    return 0
  fi

  local body changes
  body="- Version: ${old_version} -> ${new_version}"
  changes="$(collect_changes_gh "$slug" "$old_ref" "$new_ref")"
  if [[ -n "$changes" ]]; then
    body+=$'\n'
    body+="$changes"
  else
    body+=$'\n- No changes'
  fi
  append_section_changes "$out_file" "$heading" "$body"
}

OLD_REF=""
NEW_REF=""
OUTPUT_FILE=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --output)
      shift
      [[ $# -eq 0 ]] && die "Missing value for --output"
      OUTPUT_FILE="$1"
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    --*)
      die "Unknown option: $1"
      ;;
    *)
      if [[ -z "$OLD_REF" ]]; then
        OLD_REF="$1"
      elif [[ -z "$NEW_REF" ]]; then
        NEW_REF="$1"
      else
        die "Unexpected argument: $1"
      fi
      ;;
  esac
  shift
done

[[ -z "$OLD_REF" || -z "$NEW_REF" ]] && { usage; exit 1; }

gh_ready || die "gh CLI is required and must be authenticated"
ensure_ref_exists_gh "$KAIROS_SLUG" "$OLD_REF" || die "Ref not found in ${KAIROS_SLUG}: $OLD_REF"
ensure_ref_exists_gh "$KAIROS_SLUG" "$NEW_REF" || die "Ref not found in ${KAIROS_SLUG}: $NEW_REF"

declare -A old_deps=()
declare -A new_deps=()

load_makefile_versions "$OLD_REF" old_deps || die "Unable to read kairos-init/Makefile (post- or pre-migration) for ${KAIROS_SLUG}@${OLD_REF}"
load_makefile_versions "$NEW_REF" new_deps || die "Unable to read kairos-init/Makefile (post- or pre-migration) for ${KAIROS_SLUG}@${NEW_REF}"
load_gomod_versions "$OLD_REF" old_deps || die "Unable to read go.mod (post- or pre-migration) for ${KAIROS_SLUG}@${OLD_REF}"
load_gomod_versions "$NEW_REF" new_deps || die "Unable to read go.mod (post- or pre-migration) for ${KAIROS_SLUG}@${NEW_REF}"

declare -a fixed_components=(
  provider-kairos
  edgevpn
  entities
  go-pluggable
  yip
  xpasswd
)

declare -A component_seen=()
declare -a all_components=()

for c in "${fixed_components[@]}"; do
  all_components+=("$c")
  component_seen["$c"]=1
done

# Already handled by the path-diff loop above. The pre-migration go.mod
# fallback in load_gomod_versions can still populate these into old_deps
# when OLD_REF pre-dates the migration (that go.mod listed them as real
# dependencies back then) -- excluded here so they don't also get a second,
# stale, version-based section on top of their real path-diffed one.
for c in "${!MONOREPO_PATHS[@]}"; do
  component_seen["$c"]=1
done

for c in "${!old_deps[@]}" "${!new_deps[@]}"; do
  if [[ -z "${component_seen[$c]:-}" ]]; then
    all_components+=("$c")
    component_seen["$c"]=1
  fi
done

output_tmp="$(mktemp)"
trap 'rm -f "$output_tmp"' EXIT

append_section_changes "$output_tmp" "Kairos changes" "$(collect_changes_gh "$KAIROS_SLUG" "$OLD_REF" "$NEW_REF")"

# kairos-init, kairos-agent, immucore, kairos-sdk and kcrypt-discovery-challenger
# are monorepo paths post-migration, diffed the same way "Kairos changes"
# itself is (same repo, same OLD_REF/NEW_REF), not a separate version+repo.
# Order matches the pre-migration output: kairos-init first, same as before.
for component in kairos-init kairos-agent immucore kairos-sdk kcrypt-discovery-challenger; do
  append_path_diff_section "$output_tmp" "$component" "${MONOREPO_PATHS[$component]}"
done

for component in "${all_components[@]}"; do
  append_component_section "$output_tmp" "$component" "${old_deps[$component]:-}" "${new_deps[$component]:-}"
done

if [[ -n "$OUTPUT_FILE" ]]; then
  cp "$output_tmp" "$OUTPUT_FILE"
  printf 'Release notes written to %s\n' "$OUTPUT_FILE"
  printf 'Compared Kairos: %s -> %s\n' "$OLD_REF" "$NEW_REF"
else
  cat "$output_tmp"
fi

#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"

KAIROS_SLUG="kairos-io/kairos"
KAIROS_INIT_SLUG="kairos-io/kairos-init"

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
    kairos-init) printf '%s\n' "$KAIROS_INIT_SLUG" ;;
    kairos-agent) printf 'kairos-io/kairos-agent\n' ;;
    immucore) printf 'kairos-io/immucore\n' ;;
    kcrypt-discovery-challenger) printf 'kairos-io/kcrypt-discovery-challenger\n' ;;
    provider-kairos) printf 'kairos-io/provider-kairos\n' ;;
    kairos-sdk) printf 'kairos-io/kairos-sdk\n' ;;
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

github_slug_from_repo() {
  local repo="$1"
  local remote
  remote="$(git -C "$repo" config --get remote.origin.url 2>/dev/null || true)"

  if [[ "$remote" =~ ^git@github\.com:([^/]+/[^/.]+)(\.git)?$ ]]; then
    printf '%s\n' "${BASH_REMATCH[1]}"
    return 0
  fi
  if [[ "$remote" =~ ^https://github\.com/([^/]+/[^/.]+)(\.git)?$ ]]; then
    printf '%s\n' "${BASH_REMATCH[1]}"
    return 0
  fi
  return 1
}

ensure_ref_exists_local() {
  local repo="$1"
  local ref="$2"
  git -C "$repo" rev-parse --verify "${ref}^{commit}" >/dev/null 2>&1
}

ensure_ref_exists_gh() {
  local slug="$1"
  local ref="$2"
  gh api "repos/${slug}/commits/${ref}" >/dev/null 2>&1
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

extract_kairos_init_version() {
  local kairos_ref="$1"
  local dockerfile
  dockerfile="$(get_file_content "$KAIROS_SLUG" "$kairos_ref" "images/Dockerfile")" || return 1

  local line
  while IFS= read -r line; do
    if [[ "$line" =~ ^ARG[[:space:]]+KAIROS_INIT=([^[:space:]]+) ]]; then
      printf '%s\n' "${BASH_REMATCH[1]}"
      return 0
    fi
  done <<<"$dockerfile"

  return 1
}

load_makefile_versions() {
  local init_ref="$1"
  local out_name="$2"
  local content
  content="$(get_file_content "$KAIROS_INIT_SLUG" "$init_ref" "Makefile")" || return 1

  declare -n out_ref="$out_name"
  local line value
  while IFS= read -r line; do
    case "$line" in
      "AGENT_VERSION :="*) value="${line#AGENT_VERSION := }"; out_ref["kairos-agent"]="$value" ;;
      "IMMUCORE_VERSION :="*) value="${line#IMMUCORE_VERSION := }"; out_ref["immucore"]="$value" ;;
      "KCRYPT_DISCOVERY_CHALLENGER_VERSION :="*) value="${line#KCRYPT_DISCOVERY_CHALLENGER_VERSION := }"; out_ref["kcrypt-discovery-challenger"]="$value" ;;
      "PROVIDER_KAIROS_VERSION :="*) value="${line#PROVIDER_KAIROS_VERSION := }"; out_ref["provider-kairos"]="$value" ;;
      "EDGEVPN_VERSION :="*) value="${line#EDGEVPN_VERSION := }"; out_ref["edgevpn"]="$value" ;;
    esac
  done <<<"$content"
}

load_gomod_versions() {
  local init_ref="$1"
  local out_name="$2"
  local content
  content="$(get_file_content "$KAIROS_INIT_SLUG" "$init_ref" "go.mod")" || return 1

  declare -n out_ref="$out_name"
  local line module owner version rest component
  while IFS= read -r line; do
    if [[ "$line" =~ ^[[:space:]]*(github\.com/(kairos-io|mudler|mauromorales)/[^[:space:]]+)[[:space:]]+([^[:space:]]+) ]]; then
      module="${BASH_REMATCH[1]}"
      owner="${BASH_REMATCH[2]}"
      version="${BASH_REMATCH[3]}"
      rest="${module#github.com/*/}"
      component="${rest%%/*}"
      if [[ -z "${out_ref[$component]:-}" ]]; then
        out_ref["$component"]="$version"
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

collect_changes() {
  local _unused_target="$1"
  local from_ref="$2"
  local to_ref="$3"
  local slug="$4"
  collect_changes_gh "$slug" "$from_ref" "$to_ref"
}

section_title_for_component() {
  local component="$1"
  case "$component" in
    immucore) printf 'Immucore' ;;
    *) printf '%s' "$component" ;;
  esac
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

  local body changes target
  body="- Version: ${old_version} -> ${new_version}"
  target=""
  changes="$(collect_changes "$target" "$old_ref" "$new_ref" "$slug")"
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

OLD_INIT="$(extract_kairos_init_version "$OLD_REF" || true)"
NEW_INIT="$(extract_kairos_init_version "$NEW_REF" || true)"
[[ -n "$OLD_INIT" ]] || die "Could not determine KAIROS_INIT for $OLD_REF"
[[ -n "$NEW_INIT" ]] || die "Could not determine KAIROS_INIT for $NEW_REF"

ensure_ref_exists_gh "$KAIROS_INIT_SLUG" "$OLD_INIT" || die "kairos-init ref not found on GitHub: $OLD_INIT"
ensure_ref_exists_gh "$KAIROS_INIT_SLUG" "$NEW_INIT" || die "kairos-init ref not found on GitHub: $NEW_INIT"

declare -A old_deps=()
declare -A new_deps=()

load_makefile_versions "$OLD_INIT" old_deps || die "Unable to read Makefile at kairos-init ref $OLD_INIT"
load_makefile_versions "$NEW_INIT" new_deps || die "Unable to read Makefile at kairos-init ref $NEW_INIT"
load_gomod_versions "$OLD_INIT" old_deps || die "Unable to read go.mod at kairos-init ref $OLD_INIT"
load_gomod_versions "$NEW_INIT" new_deps || die "Unable to read go.mod at kairos-init ref $NEW_INIT"

declare -a fixed_components=(
  immucore
  kairos-agent
  kairos-sdk
  kcrypt-discovery-challenger
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

for c in "${!old_deps[@]}" "${!new_deps[@]}"; do
  if [[ -z "${component_seen[$c]:-}" ]]; then
    all_components+=("$c")
    component_seen["$c"]=1
  fi
done

output_tmp="$(mktemp)"
trap 'rm -f "$output_tmp"' EXIT

append_section_changes "$output_tmp" "Kairos changes" "$(collect_changes "" "$OLD_REF" "$NEW_REF" "$KAIROS_SLUG")"
init_changes="$(collect_changes "" "$OLD_INIT" "$NEW_INIT" "$KAIROS_INIT_SLUG")"

init_body="- Version: ${OLD_INIT} -> ${NEW_INIT}"
if [[ -n "$init_changes" ]]; then
  init_body+=$'\n'
  init_body+="$init_changes"
else
  init_body+=$'\n- No changes'
fi
append_section_changes "$output_tmp" "kairos-init changes" "$init_body"

for component in "${all_components[@]}"; do
  append_component_section "$output_tmp" "$component" "${old_deps[$component]:-}" "${new_deps[$component]:-}"
done

if [[ -n "$OUTPUT_FILE" ]]; then
  cp "$output_tmp" "$OUTPUT_FILE"
  printf 'Release notes written to %s\n' "$OUTPUT_FILE"
  printf 'Compared Kairos: %s -> %s\n' "$OLD_REF" "$NEW_REF"
  printf 'Resolved kairos-init: %s -> %s\n' "$OLD_INIT" "$NEW_INIT"
else
  cat "$output_tmp"
fi

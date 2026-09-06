#!/usr/bin/env bash

set -euo pipefail

KAIROS_SLUG="kairos-io/kairos"
KAIROS_INIT_SLUG="kairos-io/kairos-init"

# Components merged into the monorepo on 2026-08-19..21 (provider on 2026-08-31)
# and their subpath in this repo. Ordering here is the order they were archived
# externally and is followed by the render loop below.
declare -A INTREE_SUBPATH=(
  [kairos-init]="kairos-init"
  [kairos-agent]="agent"
  [immucore]="immucore"
  [kairos-sdk]="sdk"
  [kcrypt-discovery-challenger]="kcrypt"
  [provider-kairos]="provider"
)

declare -A COMPONENT_SLUG_HINT=()

KAIROS_REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

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

# git-based helpers, used when reading in-tree state from the local kairos
# checkout. Fall back to the GitHub API if git is unusable (no repo, ref
# unfetched, etc.).

git_repo_ok() {
  command -v git >/dev/null 2>&1 && \
    git -C "$KAIROS_REPO_ROOT" rev-parse --is-inside-work-tree >/dev/null 2>&1
}

intree_present_at() {
  local ref="$1"
  local subpath="$2"
  if git_repo_ok && git -C "$KAIROS_REPO_ROOT" cat-file -e "${ref}:${subpath}" 2>/dev/null; then
    return 0
  fi
  # Fall back to gh api. contents/ returns 200 on a directory too.
  gh api "repos/${KAIROS_SLUG}/contents/${subpath}?ref=${ref}" >/dev/null 2>&1
}

get_intree_content() {
  local ref="$1"
  local path="$2"
  if git_repo_ok; then
    git -C "$KAIROS_REPO_ROOT" cat-file -p "${ref}:${path}" 2>/dev/null && return 0
  fi
  get_file_content_gh "$KAIROS_SLUG" "$ref" "$path"
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

load_makefile_versions_from_content() {
  local content="$1"
  local map_name="$2"
  local line value
  while IFS= read -r line; do
    case "$line" in
      "AGENT_VERSION :="*) value="${line#AGENT_VERSION := }"; set_assoc_entry "$map_name" "kairos-agent" "$value" ;;
      "IMMUCORE_VERSION :="*) value="${line#IMMUCORE_VERSION := }"; set_assoc_entry "$map_name" "immucore" "$value" ;;
      "KCRYPT_DISCOVERY_CHALLENGER_VERSION :="*) value="${line#KCRYPT_DISCOVERY_CHALLENGER_VERSION := }"; set_assoc_entry "$map_name" "kcrypt-discovery-challenger" "$value" ;;
      "PROVIDER_KAIROS_VERSION :="*) value="${line#PROVIDER_KAIROS_VERSION := }"; set_assoc_entry "$map_name" "provider-kairos" "$value" ;;
      "EDGEVPN_VERSION :="*) value="${line#EDGEVPN_VERSION := }"; set_assoc_entry "$map_name" "edgevpn" "$value" ;;
    esac
  done <<<"$content"
}

load_gomod_versions_from_content() {
  local content="$1"
  local map_name="$2"
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

# For a given kairos_ref, populate <map_name> with the versions of every
# component this repo currently pins (via kairos-init's Makefile plus the
# go.mod for github.com/{kairos-io,mudler,mauromorales}/*). When kairos-init
# is in-tree at kairos_ref, both files are read from the monorepo at that
# ref (kairos-init/Makefile and the top-level go.mod). When it is not,
# they are read from the archived kairos-io/kairos-init at the version
# pinned in images/Dockerfile's ARG KAIROS_INIT.
populate_dep_versions() {
  local kairos_ref="$1"
  local map_name="$2"

  local makefile gomod intree=0
  if intree_present_at "$kairos_ref" "kairos-init"; then
    intree=1
    makefile="$(get_intree_content "$kairos_ref" "kairos-init/Makefile")" || return 1
    gomod="$(get_intree_content "$kairos_ref" "go.mod")" || return 1
  else
    local init_ver
    init_ver="$(extract_kairos_init_version "$kairos_ref")" || return 1
    [[ -n "$init_ver" ]] || return 1
    ensure_ref_exists_gh "$KAIROS_INIT_SLUG" "$init_ver" || return 1
    makefile="$(get_file_content "$KAIROS_INIT_SLUG" "$init_ver" "Makefile")" || return 1
    gomod="$(get_file_content "$KAIROS_INIT_SLUG" "$init_ver" "go.mod")" || return 1
  fi

  load_makefile_versions_from_content "$makefile" "$map_name"
  load_gomod_versions_from_content "$gomod" "$map_name"

  # Post-migration Makefile writes EDGEVPN_VERSION := $(shell cat EDGEVPN_VERSION),
  # which is only meaningful at make-time. The truth is in the sidecar file.
  if [[ "$intree" == "1" ]]; then
    local current="$(get_assoc_entry "$map_name" "edgevpn")"
    if [[ -z "$current" || "$current" == *'$(shell'* ]]; then
      local edgevpn_file
      edgevpn_file="$(get_intree_content "$kairos_ref" "kairos-init/EDGEVPN_VERSION" 2>/dev/null | head -n 1 | tr -d '[:space:]')"
      [[ -n "$edgevpn_file" ]] && set_assoc_entry "$map_name" "edgevpn" "$edgevpn_file"
    fi
  fi
}

# Render pipe-separated commit lines as bullet items. Each line is
#   <sha>|<subject>|<author_name>|<author_login>|<author_email>
# author_login may be empty (e.g. when the caller has no cheap way to get it,
# such as git log against local history); the PR lookup fills it in when the
# commit has a merged PR on the same slug.
_format_commit_lines() {
  local slug="$1"
  local commit_lines="$2"
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

collect_changes_gh() {
  local slug="$1"
  local from_ref="$2"
  local to_ref="$3"

  local commit_lines
  commit_lines="$(gh api "repos/${slug}/compare/${from_ref}...${to_ref}" --paginate --jq '.commits[]? | "\(.sha)|\(.commit.message|split("\n")[0])|\(.commit.author.name // "")|\(.author.login // "")|\(.commit.author.email // "")"' 2>/dev/null || true)"
  _format_commit_lines "$slug" "$commit_lines"
}

# Commits touching <subpath> in the from..to range on this repo. Uses git log
# on the local checkout when available (fast, no rate limit) and falls back to
# paging repos/kairos-io/kairos/commits?path=... otherwise.
collect_intree_changes() {
  local subpath="$1"
  local from_ref="$2"
  local to_ref="$3"

  local commit_lines=""
  if git_repo_ok && \
     git -C "$KAIROS_REPO_ROOT" rev-parse --verify --quiet "$from_ref" >/dev/null && \
     git -C "$KAIROS_REPO_ROOT" rev-parse --verify --quiet "$to_ref" >/dev/null; then
    commit_lines="$(git -C "$KAIROS_REPO_ROOT" log --no-merges \
      --format='%H|%s|%an||%ae' "${from_ref}..${to_ref}" -- "$subpath" 2>/dev/null || true)"
  else
    # Paged fallback: list commits touching the path on to_ref, bounded to
    # those newer than from_ref's committer timestamp. from_ref itself may not
    # touch the path, so a SHA sentinel is not reliable.
    local from_date
    from_date="$(gh api "repos/${KAIROS_SLUG}/commits/${from_ref}" --jq '.commit.committer.date' 2>/dev/null || true)"
    local page=1 raw
    local since_arg=""
    [[ -n "$from_date" ]] && since_arg="&since=${from_date}"
    while [[ "$page" -le 20 ]]; do
      raw="$(gh api "repos/${KAIROS_SLUG}/commits?sha=${to_ref}&path=${subpath}${since_arg}&per_page=100&page=${page}" --jq '.[] | "\(.sha)|\(.commit.message|split("\n")[0])|\(.commit.author.name // "")|\(.author.login // "")|\(.commit.author.email // "")"' 2>/dev/null || true)"
      [[ -z "$raw" ]] && break
      commit_lines+="${raw}"$'\n'
      page=$((page + 1))
    done
  fi

  _format_commit_lines "$KAIROS_SLUG" "$commit_lines"
}

# Regime of a component across an OLD_REF -> NEW_REF compare:
#   intree      subpath present at both refs
#   bridging    subpath present at NEW_REF only (imported between old and new)
#   external    subpath present at neither
#   removed     subpath present at OLD_REF only (unlikely; treated as external)
component_regime() {
  local subpath="$1"
  local old_ref="$2"
  local new_ref="$3"
  local at_old="no" at_new="no"
  intree_present_at "$old_ref" "$subpath" && at_old="yes"
  intree_present_at "$new_ref" "$subpath" && at_new="yes"
  if [[ "$at_old" == "yes" && "$at_new" == "yes" ]]; then
    printf 'intree\n'
  elif [[ "$at_old" == "no" && "$at_new" == "yes" ]]; then
    printf 'bridging\n'
  elif [[ "$at_old" == "yes" && "$at_new" == "no" ]]; then
    printf 'removed\n'
  else
    printf 'external\n'
  fi
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

_archival_ref_for_slug() {
  local slug="$1"
  local branch head
  branch="$(gh api "repos/${slug}" --jq '.default_branch' 2>/dev/null || true)"
  [[ -z "$branch" ]] && return 1
  head="$(gh api "repos/${slug}/branches/${branch}" --jq '.commit.sha' 2>/dev/null || true)"
  [[ -z "$head" ]] && return 1
  printf '%s\n' "$head"
}

append_intree_component_section() {
  local out_file="$1"
  local component="$2"
  local old_ref="$3"
  local new_ref="$4"
  local old_ext_version="$5"

  local subpath="${INTREE_SUBPATH[$component]}"
  local heading
  heading="$(section_title_for_component "$component") changes"

  local regime
  regime="$(component_regime "$subpath" "$old_ref" "$new_ref")"

  local body="" changes external_slug archival_ref ext_changes
  case "$regime" in
    intree)
      changes="$(collect_intree_changes "$subpath" "$old_ref" "$new_ref")"
      if [[ -n "$changes" ]]; then
        body="$changes"
      else
        body="- No changes"
      fi
      ;;
    bridging)
      # First half: everything the archived external repo received from the
      # old pinned version up to its final commit. Second half: everything
      # this repo has recorded on the subpath since it was imported.
      external_slug="$(component_to_slug "$component" || true)"
      if [[ -n "$old_ext_version" && -n "$external_slug" ]]; then
        archival_ref="$(_archival_ref_for_slug "$external_slug" || true)"
        if [[ -n "$archival_ref" ]] && ensure_ref_exists_gh "$external_slug" "$old_ext_version"; then
          body="- Version: ${old_ext_version} -> merged in-tree at kairos ${new_ref}"
          ext_changes="$(collect_changes_gh "$external_slug" "$old_ext_version" "$archival_ref")"
          [[ -n "$ext_changes" ]] && { body+=$'\n'; body+="$ext_changes"; }
        else
          body="- Version: ${old_ext_version} -> merged in-tree at kairos ${new_ref}"
          body+=$'\n- Unable to resolve archived repository history'
        fi
      else
        body="- Merged in-tree at kairos ${new_ref}"
      fi
      changes="$(collect_intree_changes "$subpath" "$old_ref" "$new_ref")"
      if [[ -n "$changes" ]]; then
        [[ -n "$body" ]] && body+=$'\n'
        body+="$changes"
      fi
      ;;
    removed)
      body="- Version: ${old_ext_version:-n/a} -> component no longer present"
      ;;
    external|*)
      # Delegate to the external path below by returning non-zero, so the
      # caller falls back to append_component_section for external components.
      return 2
      ;;
  esac

  append_section_changes "$out_file" "$heading" "$body"
  return 0
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

populate_dep_versions "$OLD_REF" old_deps || die "Unable to load dependency versions for $OLD_REF"
populate_dep_versions "$NEW_REF" new_deps || die "Unable to load dependency versions for $NEW_REF"

# kairos-init's version lives in images/Dockerfile (ARG KAIROS_INIT), not in
# the Makefile/go.mod that populate_dep_versions reads. Record it in the same
# maps so the external-regime fallback and the bridging label find it there.
_old_init="$(extract_kairos_init_version "$OLD_REF" 2>/dev/null || true)"
_new_init="$(extract_kairos_init_version "$NEW_REF" 2>/dev/null || true)"
[[ -n "$_old_init" ]] && old_deps[kairos-init]="$_old_init"
[[ -n "$_new_init" ]] && new_deps[kairos-init]="$_new_init"

declare -a fixed_components=(
  kairos-init
  kairos-agent
  immucore
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

append_section_changes "$output_tmp" "Kairos changes" "$(collect_changes_gh "$KAIROS_SLUG" "$OLD_REF" "$NEW_REF")"

for component in "${all_components[@]}"; do
  if [[ -n "${INTREE_SUBPATH[$component]:-}" ]]; then
    if append_intree_component_section "$output_tmp" "$component" "$OLD_REF" "$NEW_REF" "${old_deps[$component]:-}"; then
      continue
    fi
    # Regime came back "external" (both refs pre-migration): fall through.
  fi
  append_component_section "$output_tmp" "$component" "${old_deps[$component]:-}" "${new_deps[$component]:-}"
done

if [[ -n "$OUTPUT_FILE" ]]; then
  cp "$output_tmp" "$OUTPUT_FILE"
  printf 'Release notes written to %s\n' "$OUTPUT_FILE"
  printf 'Compared Kairos: %s -> %s\n' "$OLD_REF" "$NEW_REF"
else
  cat "$output_tmp"
fi

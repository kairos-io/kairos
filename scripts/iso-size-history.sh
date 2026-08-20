#!/usr/bin/env bash
# Track built ISO sizes over time, one data point per merge to master.
#
# Unlike the per-PR report (iso-size-diff.sh), which compares a PR against the
# last successful master build and writes an ephemeral step summary, this
# script maintains a *durable* time series: a CSV with one row per (merge,
# artifact) pair plus a regenerated Markdown table and a self-contained SVG
# chart (one independently scaled panel per ISO, so small changes stay
# visible). The CSV is the source of truth and is meant to be committed to a
# long-lived, orphan `size-history` branch so the history survives the log and
# step-summary purging that the per-PR report is subject to.
#
# The CSV is stored in a "long" format -- one line per (sha, artifact) -- so a
# changing set of ISOs (a variant added or removed over time) is handled
# gracefully without any column surgery. render() pivots it into a wide table.
#
# Usage:
#   iso-size-history.sh record <csv> <sha> <candidate-root>
#       Append one row per ISO found under <candidate-root> to <csv> (creating
#       it with a header if needed) and print a one-merge summary (with deltas
#       vs the previous recorded merge) to stdout.
#
#   iso-size-history.sh render <csv> <out-dir> [repo-slug] [tags-file]
#       (Re)generate <out-dir>/SIZE_HISTORY.md and <out-dir>/size-history.svg
#       from <csv>. Pure function of the inputs; needs no network. When a
#       <repo-slug> is given each sha is rendered as a link to its commit; when
#       a <tags-file> (lines "<full-sha> <tag>") is also given, a merge whose
#       sha is a release is annotated with that release's version, linked to the
#       release page, to the right of the sha (never as a separate row).
#
#   iso-size-history.sh all <csv> <sha> <candidate-root> <out-dir> \
#                           [repo-slug] [tags-file]
#       record then render; prints the per-merge summary to stdout.
#
# <candidate-root> follows the same layout as iso-size-diff.sh: a directory
# holding one sub-directory per downloaded artifact, each containing the built
# ISO. Sizes are the raw ISO file sizes (wc -c), matching iso-size-diff.sh.
set -euo pipefail

# Stable, locale-independent sorting so the pivots below are deterministic
# regardless of the runner locale.
export LC_ALL=C

# How many of the most recent merges to show in the rendered Markdown table.
HISTORY_TABLE_ROWS="${HISTORY_TABLE_ROWS:-20}"

die() {
  printf 'Error: %s\n' "$*" >&2
  exit 1
}

human() {
  awk -v b="$1" 'BEGIN { printf "%.2f MiB", b / 1048576 }'
}

# iso_in <dir>: print the path of the bootable ISO under <dir> (depth <= 2),
# excluding ipxe artifacts, or nothing. Mirrors iso-size-diff.sh.
iso_in() {
  [[ -d "$1" ]] || return 0
  find "$1" -maxdepth 2 -type f -name '*.iso' ! -name '*ipxe*' 2>/dev/null \
    | head -n1 || true
}

# canonical_key <artifact-dir-name>: derive a stable identity for an ISO that
# does not drift across builds. The artifact name embeds a per-build release
# token (custom_artifact_format is kairos-$FLAVOR-$FLAVOR_RELEASE-$VARIANT-...),
# so any dash-separated segment that starts with a digit (optionally a leading
# "v", e.g. "v0.4.0" or a "20240131" date) is dropped. The remaining segments
# (kairos-hadron-core-amd64-generic, ...) are stable and become the CSV key.
canonical_key() {
  local name="$1"
  name="${name%.iso.zip}"
  name="${name%.iso}"
  awk -F'-' 'BEGIN{OFS="-"} {
    out=""
    for (i=1;i<=NF;i++) {
      if ($i ~ /^v?[0-9]/) continue
      out = (out=="") ? $i : out OFS $i
    }
    print out
  }' <<<"$name"
}

# to_wide <csv>: convert the long CSV (sha,date,artifact,bytes) to a wide TSV on
# stdout: a header "sha<TAB>date<TAB>art1<TAB>art2..." followed by one line per
# merge, in first-seen order, with an empty field where an artifact has no data
# for that merge. Artifact columns are sorted for a stable layout.
to_wide() {
  awk -F',' '
    NR==1 { next }                     # skip header
    {
      sha=$1; date=$2; art=$3; bytes=$4
      if (!(sha in seen_sha)) { order[++nm]=sha; seen_sha[sha]=1; dateof[sha]=date }
      val[sha SUBSEP art]=bytes
      if (!(art in seen_art)) { arts[++na]=art; seen_art[art]=1 }
    }
    END {
      # Sort artifact names (simple insertion sort; the list is tiny).
      for (i=2;i<=na;i++) { k=arts[i]; j=i-1; while (j>=1 && arts[j]>k) { arts[j+1]=arts[j]; j-- } arts[j+1]=k }
      printf "sha\tdate"
      for (i=1;i<=na;i++) printf "\t%s", arts[i]
      printf "\n"
      for (m=1;m<=nm;m++) {
        s=order[m]
        printf "%s\t%s", s, dateof[s]
        for (i=1;i<=na;i++) {
          key=s SUBSEP arts[i]
          printf "\t%s", (key in val) ? val[key] : ""
        }
        printf "\n"
      }
    }
  ' "$1"
}

# record <csv> <sha> <candidate-root>: append one row per ISO for this merge.
record() {
  local csv="$1" sha="$2" root="$3"
  [[ -d "$root" ]] || die "Candidate root not found: $root"

  local date
  date="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

  # Create the file with a header on first use.
  if [[ ! -s "$csv" ]]; then
    printf 'sha,date,artifact,bytes\n' >"$csv"
  fi

  # Remember the previous merge's sizes (for the delta summary) before append.
  local prev_sha=""
  prev_sha="$(awk -F',' 'NR>1{s=$1} END{print s}' "$csv" || true)"
  declare -A prev_size
  if [[ -n "$prev_sha" ]]; then
    while IFS=, read -r s _ art bytes; do
      [[ "$s" == "$prev_sha" ]] && prev_size["$art"]="$bytes"
    done < <(tail -n +2 "$csv")
  fi

  # Collect this merge's ISOs, keyed by canonical name.
  declare -A cur_size
  local order=()
  local cand_dir iso key size
  for cand_dir in "$root"/*/; do
    [[ -d "$cand_dir" ]] || continue
    iso="$(iso_in "$cand_dir")"
    [[ -n "$iso" ]] || continue
    key="$(canonical_key "$(basename "$cand_dir")")"
    size="$(wc -c <"$iso" | tr -d '[:space:]')"
    cur_size["$key"]="$size"
    order+=("$key")
  done
  [[ "${#order[@]}" -gt 0 ]] || die "No ISOs found under $root"

  # Append one CSV line per artifact (sorted for a stable file).
  local sorted
  sorted="$(printf '%s\n' "${order[@]}" | sort -u)"
  while IFS= read -r key; do
    printf '%s,%s,%s,%s\n' "$sha" "$date" "$key" "${cur_size[$key]}" >>"$csv"
  done <<<"$sorted"

  # Per-merge summary with deltas vs the previous recorded merge.
  printf '## ISO size history — merge `%s`\n\n' "${sha:0:12}"
  printf '| Artifact | size | Δ vs previous merge |\n| --- | --- | --- |\n'
  local base delta sgn pct
  while IFS= read -r key; do
    if [[ -z "${prev_sha}" || -z "${prev_size[$key]:-}" ]]; then
      printf '| %s | %s | _no baseline_ |\n' "$key" "$(human "${cur_size[$key]}")"
    else
      base="${prev_size[$key]}"
      delta=$(( cur_size[$key] - base ))
      sgn='+'; [[ "$delta" -lt 0 ]] && sgn='-'
      pct=$(awk -v d="$delta" -v m="$base" 'BEGIN{printf "%+.2f", (m>0)?(d/m*100):0}')
      printf '| %s | %s | %s%s (%s%%) |\n' \
        "$key" "$(human "${cur_size[$key]}")" "$sgn" "$(human "${delta#-}")" "$pct"
    fi
  done <<<"$sorted"
  printf '\n'
}

# svg <wide-tsv> <out>: render a self-contained SVG chart with one small-
# multiple panel per artifact. Each panel has its own y-scale, tightly fit to
# that ISO's own min/max (plus headroom), so small changes are visible instead
# of being flattened by a shared scale. No external dependencies/services.
svg() {
  local wide="$1" out="$2"
  awk -F'\t' '
    function human(b,   u,i,val) {
      split("B KiB MiB GiB TiB", u, " ")
      i=1; val=b
      while (val>=1024 && i<5) { val/=1024; i++ }
      return sprintf("%.1f %s", val, u[i])
    }
    NR==1 { for (c=3;c<=NF;c++) names[c]=$c; ncol=NF; next }
    {
      n++
      for (c=3;c<=ncol;c++) {
        v[n,c]=$c
        if ($c!="") {
          if (!seen[c] || $c<min[c]) min[c]=$c
          if (!seen[c] || $c>max[c]) max[c]=$c
          seen[c]=1
        }
      }
    }
    END {
      color="#1f77b4"
      W=760; L=90; R=20; T=26; B=28; PH=120; GAP=30
      pw=W-L-R
      np=0
      for (c=3;c<=ncol;c++) if (seen[c]) panels[++np]=c
      if (np==0) { np=1; panels[1]=3; seen[3]=0 }
      H=np*(T+PH+B) + (np-1)*GAP + 24

      printf "<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"%d\" height=\"%d\" viewBox=\"0 0 %d %d\" font-family=\"sans-serif\" font-size=\"11\">\n", W, H, W, H
      printf "<rect width=\"%d\" height=\"%d\" fill=\"#ffffff\"/>\n", W, H

      xstep=(n>1)?pw/(n-1):0
      oy=0
      for (p=1;p<=np;p++) {
        c=panels[p]
        top=oy+T
        lo=min[c]; hi=max[c]
        if (!seen[c]) { lo=0; hi=1 }
        else if (lo==hi) { pad=(lo>0)?lo*0.05:1; lo-=pad; hi+=pad }
        else { pad=(hi-lo)*0.15; lo-=pad; hi+=pad }
        if (lo<0) lo=0
        if (hi==lo) hi=lo+1

        printf "<text x=\"%d\" y=\"%d\" fill=\"%s\" font-weight=\"bold\">%s</text>\n", L, top-8, color, names[c]
        printf "<line x1=\"%d\" y1=\"%d\" x2=\"%d\" y2=\"%d\" stroke=\"#888\"/>\n", L, top, L, top+PH
        printf "<line x1=\"%d\" y1=\"%d\" x2=\"%d\" y2=\"%d\" stroke=\"#888\"/>\n", L, top+PH, L+pw, top+PH
        for (g=0;g<=4;g++) {
          yy=top+PH-(g/4)*PH
          val=lo+(g/4)*(hi-lo)
          printf "<line x1=\"%d\" y1=\"%.1f\" x2=\"%d\" y2=\"%.1f\" stroke=\"#eee\"/>\n", L, yy, L+pw, yy
          printf "<text x=\"%d\" y=\"%.1f\" text-anchor=\"end\" fill=\"#555\">%s</text>\n", L-6, yy+3, human(val)
        }
        pts=""
        for (i=1;i<=n;i++) {
          if (v[i,c]=="") continue
          x=L+(i-1)*xstep
          y=top+PH-((v[i,c]-lo)/(hi-lo))*PH
          pts=pts sprintf("%.1f,%.1f ", x, y)
        }
        if (pts!="") printf "<polyline fill=\"none\" stroke=\"%s\" stroke-width=\"2\" points=\"%s\"/>\n", color, pts
        oy += T+PH+B+GAP
      }
      printf "<text x=\"%d\" y=\"%d\" text-anchor=\"middle\" fill=\"#555\">merges over time (oldest → newest)</text>\n", L+pw/2, H-8
      print "</svg>"
    }
  ' "$wide" >"$out"
}

# sha_cell <sha> <repo> <tag>: render the "sha" table cell. The short sha links
# to its commit (when <repo> is known); when the merge is a release, the version
# is appended to the right as a link to the release page (a release shares the
# merge's sha, so it never becomes a separate row).
sha_cell() {
  local sha="$1" repo="$2" tag="$3" short="${1:0:12}" out
  if [[ -n "$repo" ]]; then
    out="[\`${short}\`](https://github.com/${repo}/commit/${sha})"
  else
    out="\`${short}\`"
  fi
  if [[ -n "$tag" ]]; then
    if [[ -n "$repo" ]]; then
      out+=" [\`${tag}\`](https://github.com/${repo}/releases/tag/${tag})"
    else
      out+=" \`${tag}\`"
    fi
  fi
  printf '%s' "$out"
}

# render <csv> <out-dir> [repo] [tags-file]: regenerate SIZE_HISTORY.md and
# size-history.svg from the CSV.
render() {
  local csv="$1" out="$2" repo="${3:-}" tags="${4:-}"
  [[ -s "$csv" ]] || die "CSV not found or empty: $csv"
  mkdir -p "$out"

  local wide
  wide="$(mktemp)"
  to_wide "$csv" >"$wide"
  svg "$wide" "$out/size-history.svg"

  # Map merge sha -> release version, so a release annotates its own merge row.
  declare -A REL_TAG
  if [[ -n "$tags" && -s "$tags" ]]; then
    local rsha rtag
    while read -r rsha rtag; do
      [[ -n "$rsha" && -n "$rtag" ]] && REL_TAG["$rsha"]="$rtag"
    done <"$tags"
  fi

  # Read the wide header to get the ordered artifact column names.
  local header
  header="$(head -n1 "$wide")"
  IFS=$'\t' read -r -a cols <<<"$header"   # cols[0]=sha cols[1]=date cols[2..]=arts
  local nart=$(( ${#cols[@]} - 2 ))

  local md="$out/SIZE_HISTORY.md"
  {
    printf '# ISO size history\n\n'
    printf 'Raw ISO file size (`wc -c`) of each built ISO, recorded once per merge\n'
    printf 'to `master`. The CSV (`size-history.csv`) is the source of truth; this\n'
    printf 'page and the chart are regenerated from it by\n'
    printf '`scripts/iso-size-history.sh`.\n\n'
    printf '![ISO size history](./size-history.svg)\n\n'

    printf '## Most recent %s merges\n\n' "$HISTORY_TABLE_ROWS"
    printf '| date | sha |'
    local a
    for ((a=2; a<${#cols[@]}; a++)); do printf ' %s |' "${cols[$a]}"; done
    printf '\n|---|---|'
    for ((a=0; a<nart; a++)); do printf -- ' ---: |'; done
    printf '\n'

    # Wide data rows are oldest-first; show the last N, newest first, with a
    # per-cell delta vs the previous recorded merge.
    local i
    # Build an array of data lines for easy predecessor lookup.
    mapfile -t datalines < <(tail -n +2 "$wide")
    for (( i=${#datalines[@]}-1; i>=0 && ${#datalines[@]}-i<=HISTORY_TABLE_ROWS; i-- )); do
      IFS=$'\t' read -r -a cur <<<"${datalines[$i]}"
      local base_row=""
      [[ "$i" -gt 0 ]] && base_row="${datalines[$((i-1))]}"
      local -a base=()
      [[ -n "$base_row" ]] && IFS=$'\t' read -r -a base <<<"$base_row"

      printf '| %s | %s |' "${cur[1]%T*}" \
        "$(sha_cell "${cur[0]}" "$repo" "${REL_TAG[${cur[0]}]:-}")"
      local col c b delta sgn
      for ((col=2; col<${#cols[@]}; col++)); do
        c="${cur[$col]:-}"
        b="${base[$col]:-}"
        if [[ -z "$c" ]]; then
          printf ' n/a |'
        elif [[ -z "$b" ]]; then
          printf ' %s |' "$(human "$c")"
        else
          delta=$(( c - b ))
          sgn='+'; [[ "$delta" -lt 0 ]] && sgn='-'
          printf ' %s (%s%s) |' "$(human "$c")" "$sgn" "$(human "${delta#-}")"
        fi
      done
      printf '\n'
    done
    printf '\n'
  } >"$md"

  rm -f "$wide"
}

main() {
  local cmd="${1:?usage: iso-size-history.sh <record|render|all> ...}"
  shift
  case "$cmd" in
    record) record "$@" ;;
    render) render "$@" ;;
    all)
      local csv="$1" sha="$2" root="$3" out="$4" repo="${5:-}" tags="${6:-}"
      record "$csv" "$sha" "$root"
      render "$csv" "$out" "$repo" "$tags"
      ;;
    -h|--help)
      grep '^#' "$0" | sed 's/^# \{0,1\}//'
      ;;
    *) die "unknown command: $cmd" ;;
  esac
}

main "$@"

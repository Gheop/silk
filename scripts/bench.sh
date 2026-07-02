#!/usr/bin/env bash
# Compares silk against svgo on the benchmark corpus: output size and
# wall-clock time per file. Requires npx (svgo) and the corpus checkout.
#
# Usage: scripts/bench.sh [corpus-dir]
set -u
export LC_ALL=C

corpus="${1:-/home/sib/src/benchmarkpatu/datasets}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

go build -o "$tmp/silk" ./cmd/silk || exit 1

printf '%-55s %9s %9s %7s %9s %7s %8s %8s\n' \
  FILE INPUT SILK SILK% SVGO SVGO% SILK_MS SVGO_MS

declare -a ratios_silk ratios_svgo
total_in=0 total_silk=0 total_svgo=0

while IFS= read -r f; do
  base="$(basename "$f")"
  in_size=$(stat -c%s "$f")

  t0=$(date +%s%N)
  "$tmp/silk" "$f" > "$tmp/out_silk.svg" || continue
  t1=$(date +%s%N)
  silk_ms=$(( (t1 - t0) / 1000000 ))
  silk_size=$(stat -c%s "$tmp/out_silk.svg")

  rm -f "$tmp/out_svgo.svg"
  t0=$(date +%s%N)
  npx svgo --quiet -i "$f" -o "$tmp/out_svgo.svg" 2>/dev/null
  t1=$(date +%s%N)
  svgo_ms=$(( (t1 - t0) / 1000000 ))
  # A failed svgo run keeps the input size: no output means no reduction.
  svgo_size=$(stat -c%s "$tmp/out_svgo.svg" 2>/dev/null || echo "$in_size")

  silk_pct=$(awk "BEGIN{printf \"%.1f\", 100*$silk_size/$in_size}")
  svgo_pct=$(awk "BEGIN{printf \"%.1f\", 100*$svgo_size/$in_size}")
  ratios_silk+=("$silk_pct")
  ratios_svgo+=("$svgo_pct")
  total_in=$((total_in + in_size))
  total_silk=$((total_silk + silk_size))
  total_svgo=$((total_svgo + svgo_size))

  printf '%-55s %9d %9d %6s%% %9d %6s%% %8d %8d\n' \
    "${base:0:55}" "$in_size" "$silk_size" "$silk_pct" "$svgo_size" "$svgo_pct" "$silk_ms" "$svgo_ms"
done < <(find "$corpus" -name '*.svg' | sort)

median() {
  printf '%s\n' "$@" | sort -n | awk '{a[NR]=$1} END{print (NR%2? a[(NR+1)/2] : (a[NR/2]+a[NR/2+1])/2)}'
}

echo
printf 'TOTAL bytes: input=%d silk=%d (%.1f%%) svgo=%d (%.1f%%)\n' \
  "$total_in" "$total_silk" "$(awk "BEGIN{print 100*$total_silk/$total_in}")" \
  "$total_svgo" "$(awk "BEGIN{print 100*$total_svgo/$total_in}")"
printf 'MEDIAN size ratio: silk=%s%% svgo=%s%%\n' \
  "$(median "${ratios_silk[@]}")" "$(median "${ratios_svgo[@]}")"

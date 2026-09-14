#!/usr/bin/env bash
# Срез репозитория по годам: состояние на конец каждого года -> метрики формы.
# Рабочая копия не трогается, всё через git archive во временный каталог.
#
#   ./years.sh ~/src/postgres src/backend 1996 2026 > data/postgres.csv
#
# Язык без грамматики в shape (Swift, Kotlin) считается по отступам:
#   TOOL=depth ./years.sh ~/src/tenniarb Tenniarb 2017 2026
set -u
repo=$1; sub=${2:-.}; from=${3:-2001}; to=${4:-2026}
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
tool=${TOOL:-shape}

if [ "$tool" = shape ]; then
  probe="$here/shape/probe"
  [ -x "$probe" ] || (cd "$here/shape" && go build -o probe .) || exit 1
  echo "год,функций,cc50,cc90,cc95,sloc50,sloc90,depth95,cc_gt10,depth_gt4,sloc_gt60"
else
  echo "год,функций,depth50,depth90,depth95,depth99,sloc50,sloc90,sloc95,sloc99,depth_gt4,sloc_gt60"
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
for y in $(seq "$from" "$to"); do
  c=$(git -C "$repo" rev-list -1 --before="$y-12-31" HEAD 2>/dev/null)
  [ -z "$c" ] && continue
  rm -rf "$tmp/w"; mkdir -p "$tmp/w"
  git -C "$repo" archive "$c" "$sub" 2>/dev/null | tar x -C "$tmp/w" 2>/dev/null || continue
  if [ "$tool" = shape ]; then
    CSV=1 STATS=1 "$probe" "$tmp/w" 2>/dev/null | sed "s|^w,|$y,|"
  else
    CSV=1 python3 "$here/depth.py" "$tmp/w" 2>/dev/null | sed "s|^w,|$y,|"
  fi
done

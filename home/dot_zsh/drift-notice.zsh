# drift-notice.zsh: one line on shell start when athome, grofe or canopy is
# behind. The check itself (`athome status --quiet`) runs at most once a day in
# the background and caches its one-line result; a shell start only reads the
# cache and its mtime, with builtins, and forks only to start a due check.
zmodload -F zsh/stat b:zstat
zmodload zsh/datetime zsh/system

# _athome_drift_fresh <cache>: the cache was written in the last 24 hours. A
# future mtime counts as stale.
_athome_drift_fresh() {
  local -a mt
  zstat -A mt +mtime -- "$1" 2>/dev/null || return 1
  (( EPOCHSECONDS - mt[1] >= 0 && EPOCHSECONDS - mt[1] < 86400 ))
}

# _athome_drift_check <cache>: run the check and write its line to the cache.
# The flock lets one shell check while the others started meanwhile skip; a
# killed check releases it and writes nothing, so the next shell checks again.
_athome_drift_check() {
  emulate -L zsh
  local cache=$1 out rc line l fix
  local -A names
  local -a fixes parts
  [[ -d ${cache:h} ]] || command mkdir -p -- "${cache:h}" || return
  : >>"$cache.lock" && zsystem flock -t 0 "$cache.lock" || return
  _athome_drift_fresh "$cache" && return
  out="$(athome status --quiet 2>/dev/null)"
  rc=$?
  case $rc in
    0) line= ;;
    3)
      # "<name>: <fix>" lines, grouped by fix in the order athome prints them.
      for l in ${(f)out}; do
        fix=${l#*: }
        (( ${+names[$fix]} )) || fixes+=("$fix")
        names[$fix]+="${names[$fix]:+, }${l%%: *}"
      done
      for fix in $fixes; do parts+=("${names[$fix]} not current; run $fix"); done
      line="athome: ${(j:; :)parts} (athome status for details)"
      ;;
    *) line="athome: drift check failed (exit $rc); run athome status" ;;
  esac
  print -rn -- "$line" >"$cache.tmp" && command mv -f -- "$cache.tmp" "$cache"
}

_athome_drift_notice() {
  emulate -L zsh
  if (( ! $+commands[athome] )); then
    print -r -- "athome: not installed; run chezmoi apply"
    return
  fi
  local cache="${XDG_CACHE_HOME:-$HOME/.cache}/athome/drift"
  [[ -s $cache ]] && print -r -- "$(<$cache)"
  _athome_drift_fresh "$cache" && return
  _athome_drift_check "$cache" </dev/null >/dev/null 2>&1 &!
}
_athome_drift_notice
unfunction _athome_drift_notice _athome_drift_fresh _athome_drift_check

# drift-notice.zsh: one line on shell start when something athome manages is
# not current, which means a `chezmoi apply` is pending. The check itself
# (`athome status --quiet`) runs at most once a day in the background and
# caches its one-line result; a shell start only reads that cache.
_athome_drift_notice() {
  emulate -L zsh
  if (( ! $+commands[athome] )); then
    print -r -- "athome: not installed; run chezmoi apply"
    return
  fi
  local cache="${XDG_CACHE_HOME:-$HOME/.cache}/athome/drift"
  [[ -s $cache ]] && print -r -- "$(<$cache)"
  # Checked in the last 24 hours (by this or another shell): nothing to do.
  local -a fresh=($cache(N.mh-24))
  (( $#fresh )) && return
  # Claim today's check first, so shells started meanwhile do not run it too.
  command mkdir -p "${cache:h}" && command touch "$cache" || return
  {
    local names line=""
    names="$(athome status --quiet 2>/dev/null)"
    (( $? == 3 )) &&
      line="athome: ${(j:, :)${(f)names}} not current; run chezmoi apply (athome status for details)"
    print -rn -- "$line" >"$cache.$$" && command mv -f "$cache.$$" "$cache"
  } </dev/null >/dev/null 2>&1 &!
}
_athome_drift_notice
unfunction _athome_drift_notice

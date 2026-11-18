# ssh-nudge.zsh: in a grofe pane, plain `ssh <machine>` gives a remote shell
# without reconnects or forwarding, and starting grofe there nests two
# multiplexers. When the target is one of grofe's machines, say so once on
# stderr. The ssh call itself always runs, with the arguments you gave.

# Hostname ssh would connect to for a name or alias (`ssh -G` reads the config).
_athome_ssh_hostname() {
  command ssh -G "$1" 2>/dev/null | awk '$1 == "hostname" { print $2; exit }'
}

# Print the grofe machine name that ssh's arguments point at; fail when they
# point at none, or the call is not a plain login (-T, a remote command, ...).
_athome_ssh_machine() {
  local arg c rest word="" count=0 notty=0 skip=0 endopts=0
  for arg in "$@"; do
    if (( skip )); then skip=0; continue; fi
    if (( endopts )) || [[ $arg != -* ]]; then
      count=$(( count + 1 )); word=$arg; continue
    fi
    [[ $arg == -- ]] && { endopts=1; continue }
    rest=${arg#-}
    while [[ -n $rest ]]; do
      c=${rest[1]}; rest=${rest[2,-1]}
      if [[ $c == [TG] ]]; then
        notty=1
      elif [[ $c == [bBcDEeFIiJLlmOoPpQRSWw] ]]; then
        # Takes a value: the rest of this word, or else the next word.
        [[ -z $rest ]] && skip=1
        break
      fi
    done
  done
  (( notty || count != 1 )) && return 1

  local grofe rows name target t host=${word##*@} hn th
  grofe=${commands[grofe]:-$HOME/.local/lib/grofe/grofe}
  [[ -x $grofe ]] || return 1
  rows=$("$grofe" machine ls 2>/dev/null) || return 1
  hn=$(_athome_ssh_hostname "$word")
  while read -r name target _; do
    [[ -n $name ]] || continue
    t=${target##*@}
    if [[ $host == "$name" || $host == "$t" || ( -n $hn && ( $hn == "$name" || $hn == "$t" ) ) ]]; then
      print -r -- "$name"; return 0
    fi
    th=$(_athome_ssh_hostname "$t")
    if [[ -n $th && ( $host == "$th" || ( -n $hn && $hn == "$th" ) ) ]]; then
      print -r -- "$name"; return 0
    fi
  done <<< "$rows"
  return 1
}

ssh() {
  if [[ -n ${GROFE:-} ]]; then
    local machine
    if machine=$(_athome_ssh_machine "$@"); then
      print -u2 -r -- "grofe: for reconnects and forwarding, use grofe -r $machine"
    fi
  fi
  command ssh "$@"
}

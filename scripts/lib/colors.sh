# shellcheck shell=bash
# Shared colour helpers — `source` me; do NOT execute. Two coexisting styles
# (bash keeps variable and function namespaces separate, so the same names work
# as both):
#   • variables  $c_red $c_grn $c_blu $c_ylw $c_dim $c_rst  — inline / partial-line
#   • functions  c_red / c_green / c_blue / c_yellow "msg"  — a whole coloured line
# Both are TTY-gated: when stdout isn't a terminal (pipes, CI logs), no ANSI is
# emitted — so captured output stays clean.
# shellcheck disable=SC2034  # several vars are consumed by the *sourcing* scripts
if [ -t 1 ]; then
  c_red=$'\033[31m'; c_grn=$'\033[32m'; c_blu=$'\033[34m'; c_ylw=$'\033[33m'; c_dim=$'\033[2m'; c_rst=$'\033[0m'
else
  c_red=''; c_grn=''; c_blu=''; c_ylw=''; c_dim=''; c_rst=''
fi
c_red()    { printf '%s%s%s\n' "$c_red" "$*" "$c_rst"; }
c_green()  { printf '%s%s%s\n' "$c_grn" "$*" "$c_rst"; }
c_blue()   { printf '%s%s%s\n' "$c_blu" "$*" "$c_rst"; }
c_yellow() { printf '%s%s%s\n' "$c_ylw" "$*" "$c_rst"; }

# Shared by the ~/.zshrc and ~/.ssh/config modify_ scripts (included inside
# their awk programs, so no single quotes in here). The first input file holds
# every body athome ever rendered for that target, each after a
# "@@athome legacy vN@@" line; the second is the current file of the user, read into
# line[1..n].
#
# drop_legacy(lo, hi) marks drop[i] for the lines in line[lo..hi] that the
# closest old body accounts for, matched in order (a longest common
# subsequence), so a line the user put inside the old body stays where it was
# even when its text matches an athome line elsewhere.
NR == FNR {
  if ($0 ~ /^@@athome legacy v[0-9]+@@$/) { nv++; m[nv] = 0; next }
  ver[nv, ++m[nv]] = $0
  next
}
{ line[++n] = $0 }
function drop_legacy(lo, hi,    v, i, j, k, r, best, bestv, L) {
  r = hi - lo + 1
  best = -1
  for (v = 1; v <= nv; v++) {
    for (i = 0; i <= r; i++) L[i, m[v] + 1] = 0
    for (j = 0; j <= m[v] + 1; j++) L[r + 1, j] = 0
    for (i = r; i >= 1; i--)
      for (j = m[v]; j >= 1; j--)
        L[i, j] = (line[lo + i - 1] == ver[v, j]) ? L[i + 1, j + 1] + 1 : (L[i + 1, j] >= L[i, j + 1] ? L[i + 1, j] : L[i, j + 1])
    if (L[1, 1] > best) {
      best = L[1, 1]; bestv = v
      for (k in pick) delete pick[k]
      i = 1; j = 1
      while (i <= r && j <= m[v]) {
        if (line[lo + i - 1] == ver[v, j]) { pick[lo + i - 1] = 1; i++; j++ }
        else if (L[i + 1, j] >= L[i, j + 1]) i++
        else j++
      }
    }
    for (k in L) delete L[k]
  }
  for (k in pick) drop[k] = 1
}

#!/usr/bin/env bash
# Repo code, per-job $HOME, and stray dotfiles must never survive outside
# the run that created them (defense in depth kept from the warm-pool era,
# when job N+1 ran on job N's disk; each run now gets a fresh instance).
# "pre" self-heals a crashed/cancelled prior run (which would have skipped
# its own if: always() cleanup) by warning and wiping any leftover content
# before the new job uses the path. "post" silently wipes the same paths at
# the end of every job so nothing sits on the instance's disk after the
# run. Both modes leave every listed path existing and empty, so a healthy
# prior run's "post" is exactly what makes the next run's "pre" find nothing
# to warn about.
#
# Usage:
#   hygiene-sweep.sh pre <path> [<path>...]
#   hygiene-sweep.sh post <path> [<path>...]
#
# Never invoked before actions/checkout in the acceptance workflow -- this
# script only exists on disk once the repo has been checked out. Freshness
# of the checked-out workspace itself at job start comes from actions/
# checkout's own `clean: true`, not from this script; this script is
# responsible for the custom job-home path pre-checkout-availability-wise,
# and for both job-home and workspace post-job.
set -euo pipefail

log() {
  printf 'hygiene-sweep: %s\n' "$*" >&2
}

sweep_pre() {
  local path="$1"
  if [ -e "$path" ] && [ -n "$(ls -A -- "$path" 2>/dev/null || true)" ]; then
    printf '::warning::hygiene-sweep: removing leftover content from a prior run at %s (likely a crashed/cancelled job that skipped its always() cleanup)\n' "$path"
    rm -rf -- "$path"
  fi
  mkdir -p -- "$path"
}

sweep_post() {
  local path="$1"
  # If this shell's cwd is at or under $path, `rm -rf -- "$path"` leaves the
  # process pointed at a deleted inode -- `mkdir -p` immediately below
  # recreates an empty, valid directory at the same name, but the shell's own
  # $PWD still resolves to the removed one, so any later relative-path
  # operation in this SAME invocation would fail with a confusing ENOENT
  # despite `ls` showing a perfectly good directory. Step out first, then
  # back into the freshly recreated directory.
  local relocated=0
  case "${PWD}/" in
    "${path}"/*)
      relocated=1
      cd / || true
      ;;
  esac
  rm -rf -- "$path"
  mkdir -p -- "$path"
  if [ "$relocated" -eq 1 ]; then
    cd -- "$path" || true
  fi
}

main() {
  local mode="${1:-}"
  [ "$#" -ge 2 ] || {
    log "usage: hygiene-sweep.sh {pre|post} <path> [<path>...]"
    exit 1
  }
  shift

  case "$mode" in
    pre)
      for path in "$@"; do
        sweep_pre "$path"
      done
      ;;
    post)
      for path in "$@"; do
        sweep_post "$path"
      done
      ;;
    *)
      log "usage: hygiene-sweep.sh {pre|post} <path> [<path>...]"
      exit 1
      ;;
  esac
}

main "$@"

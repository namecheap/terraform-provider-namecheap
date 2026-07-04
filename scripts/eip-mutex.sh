#!/usr/bin/env bash
# Treat the sandbox acceptance suite's whitelisted Elastic IP as a cross-repo
# mutex. `associate-address --no-allow-reassociation` is an atomic,
# infrastructure-less test-and-set: it fails with Resource.AlreadyAssociated
# if another job already holds the EIP instead of silently stealing it, so
# "acquire" can retry/back off while the real owner finishes, and "release"
# lets the next contender in. `reap-if-stopped` is a belt-and-braces cleanup
# for the case where a run's own release step never fired.
#
# Usage:
#   eip-mutex.sh acquire <allocation-id> <instance-id>
#   eip-mutex.sh release <allocation-id> [<instance-id>]
#   eip-mutex.sh reap-if-stopped <allocation-id>
#
# acquire: on success, prints exactly one line to stdout -- the EIP's public
# IP -- and nothing else (all progress/retry logging goes to stderr, so
# callers can safely do `ip=$(eip-mutex.sh acquire ...)`). Retries only on
# the specific Resource.AlreadyAssociated contention error; any other AWS
# error (bad allocation id, IAM denial, network failure, ...) fails
# immediately with no retry. Env overrides: EIP_MUTEX_TIMEOUT_SECONDS
# (default 1800), EIP_MUTEX_POLL_INTERVAL_SECONDS (default 15).
#
# release: idempotent no-op if the EIP is already unassociated. disassociate
# -address takes --association-id, not --allocation-id, so this first
# resolves the AssociationId via describe-addresses. When <instance-id> (the
# instance THIS job started) is given, release refuses to disassociate unless
# the EIP's current association is still with that same instance -- this is
# what stops a caller from ripping the EIP away from a different,
# legitimately-running holder that reassociated it mid-run (e.g. a
# concurrent, non-cooperating run in another repo). Without <instance-id>,
# release cannot prove ownership and is a no-op whenever the EIP is
# associated with anything.
#
# reap-if-stopped: only disassociates if the instance currently holding the
# EIP is in state "stopped" -- a running/pending/stopping instance is a
# legitimate live claim, not a leak, and is left alone.
#
# All subcommands assume AWS CLI credentials and region are already exported
# into the environment by a prior aws-actions/configure-aws-credentials step
# in the same job; none of them take a region argument.
#
# describe-addresses failures (bad allocation id, IAM denial, throttling,
# network error, ...) are NEVER swallowed here -- they propagate under
# `set -e` and fail the subcommand. A successful describe-addresses on an
# unassociated EIP already returns the literal string "None" via
# --output text, which callers check explicitly, so there is no legitimate
# case where a describe-addresses failure should be read as "not associated".
set -euo pipefail

EIP_MUTEX_TIMEOUT_SECONDS="${EIP_MUTEX_TIMEOUT_SECONDS:-1800}"
EIP_MUTEX_POLL_INTERVAL_SECONDS="${EIP_MUTEX_POLL_INTERVAL_SECONDS:-15}"

log() {
  printf 'eip-mutex: %s\n' "$*" >&2
}

resolve_public_ip() {
  local allocation_id="$1"
  aws ec2 describe-addresses \
    --allocation-ids "$allocation_id" \
    --query 'Addresses[0].PublicIp' \
    --output text
}

resolve_association_id() {
  local allocation_id="$1"
  # No `2>/dev/null || true` here: a describe-addresses failure (bad
  # allocation id, IAM denial, throttling, network error) must propagate
  # (via set -e) rather than being read as "not associated" -- an
  # unassociated-but-valid EIP already yields the literal string "None"
  # here on SUCCESS, so swallowing failures would only ever mask a genuine
  # AWS-side problem, silently reporting "nothing to release/reap" instead.
  aws ec2 describe-addresses \
    --allocation-ids "$allocation_id" \
    --query 'Addresses[0].AssociationId' \
    --output text
}

resolve_associated_instance_id() {
  local allocation_id="$1"
  # See resolve_association_id: failures propagate, they are not swallowed.
  aws ec2 describe-addresses \
    --allocation-ids "$allocation_id" \
    --query 'Addresses[0].InstanceId' \
    --output text
}

cmd_acquire() {
  local allocation_id="$1" instance_id="$2"
  local start_time elapsed err_output
  start_time="$(date +%s)"

  while true; do
    err_output="$(mktemp)"
    if aws ec2 associate-address \
      --allocation-id "$allocation_id" \
      --instance-id "$instance_id" \
      --no-allow-reassociation \
      --output text \
      --query AssociationId \
      2>"$err_output" 1>/dev/null; then
      rm -f "$err_output"
      log "acquired allocation $allocation_id for instance $instance_id"
      local public_ip
      public_ip="$(resolve_public_ip "$allocation_id")"
      printf '%s\n' "$public_ip"
      return 0
    fi

    if grep -q 'Resource.AlreadyAssociated' "$err_output"; then
      rm -f "$err_output"
      elapsed="$(( $(date +%s) - start_time ))"
      if [ "$elapsed" -ge "$EIP_MUTEX_TIMEOUT_SECONDS" ]; then
        log "timed out after ${elapsed}s waiting for allocation $allocation_id to become free"
        return 1
      fi
      log "allocation $allocation_id is already associated elsewhere; retrying in ${EIP_MUTEX_POLL_INTERVAL_SECONDS}s (elapsed ${elapsed}s/${EIP_MUTEX_TIMEOUT_SECONDS}s)"
      sleep "$EIP_MUTEX_POLL_INTERVAL_SECONDS"
      continue
    fi

    log "associate-address failed with a non-contention error:"
    cat "$err_output" >&2
    rm -f "$err_output"
    return 1
  done
}

cmd_release() {
  local allocation_id="$1" expected_instance_id="${2:-}"
  local association_id current_instance_id
  association_id="$(resolve_association_id "$allocation_id")"

  if [ -z "$association_id" ] || [ "$association_id" = "None" ]; then
    log "allocation $allocation_id is already unassociated; nothing to release"
    return 0
  fi

  if [ -z "$expected_instance_id" ]; then
    log "no instance-id given to release; refusing to blindly disassociate allocation $allocation_id since ownership of the current association ($association_id) cannot be verified"
    return 0
  fi

  current_instance_id="$(resolve_associated_instance_id "$allocation_id")"
  if [ "$current_instance_id" != "$expected_instance_id" ]; then
    log "allocation $allocation_id is currently associated with instance $current_instance_id, not this job's own instance $expected_instance_id (likely reassociated mid-run by another repo/run); leaving it alone instead of stealing it back"
    return 0
  fi

  aws ec2 disassociate-address --association-id "$association_id"
  log "released allocation $allocation_id (association $association_id, instance $expected_instance_id)"
}

cmd_reap_if_stopped() {
  local allocation_id="$1"
  local association_id instance_id instance_state

  association_id="$(resolve_association_id "$allocation_id")"
  if [ -z "$association_id" ] || [ "$association_id" = "None" ]; then
    printf 'nothing to reap: allocation %s is not associated\n' "$allocation_id"
    return 0
  fi

  instance_id="$(resolve_associated_instance_id "$allocation_id")"
  if [ -z "$instance_id" ] || [ "$instance_id" = "None" ]; then
    printf 'nothing to reap: allocation %s has an association but no instance\n' "$allocation_id"
    return 0
  fi

  instance_state="$(aws ec2 describe-instances \
    --instance-ids "$instance_id" \
    --query 'Reservations[0].Instances[0].State.Name' \
    --output text)"

  if [ "$instance_state" = "stopped" ]; then
    aws ec2 disassociate-address --association-id "$association_id"
    printf 'reaped: released allocation %s from stopped instance %s\n' "$allocation_id" "$instance_id"
  else
    printf 'left alone: allocation %s is held by instance %s in state %s\n' "$allocation_id" "$instance_id" "$instance_state"
  fi
}

main() {
  local subcommand="${1:-}"
  case "$subcommand" in
    acquire)
      [ "$#" -eq 3 ] || { log "usage: eip-mutex.sh acquire <allocation-id> <instance-id>"; exit 1; }
      cmd_acquire "$2" "$3"
      ;;
    release)
      { [ "$#" -eq 2 ] || [ "$#" -eq 3 ]; } || { log "usage: eip-mutex.sh release <allocation-id> [<instance-id>]"; exit 1; }
      cmd_release "$2" "${3:-}"
      ;;
    reap-if-stopped)
      [ "$#" -eq 2 ] || { log "usage: eip-mutex.sh reap-if-stopped <allocation-id>"; exit 1; }
      cmd_reap_if_stopped "$2"
      ;;
    *)
      log "usage: eip-mutex.sh {acquire <allocation-id> <instance-id>|release <allocation-id> [<instance-id>]|reap-if-stopped <allocation-id>}"
      exit 1
      ;;
  esac
}

main "$@"

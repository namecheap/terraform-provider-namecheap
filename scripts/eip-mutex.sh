#!/usr/bin/env bash
# Treat the sandbox acceptance suite's whitelisted Elastic IP as a cross-repo
# shared resource whose association is managed almost entirely by
# namecheap/ec2-github-runner itself, not by this script.
#
# Lifecycle model:
#   - On a cold launch, ci.yml's "Start EC2 runner" step passes
#     eip-allocation-id directly to `mode: start`. The action associates the
#     EIP internally right after the instance reaches "running", which is
#     what gives the instance internet connectivity for the rest of its own
#     bootstrap (runner tarball download, config.sh/run.sh registration).
#     This script never calls associate-address itself.
#   - On a warm restart (`reuse: stop` reusing a stopped pool instance), the
#     action's warm-start code path never re-associates the EIP -- it relies
#     on the EIP already being attached from a previous run. So the EIP is
#     deliberately left attached to the pool instance across EVERY stop/start
#     cycle for as long as the instance is "ours"; it is never released on an
#     individual stop.
#   - The EIP is released only as an automatic AWS side effect of the
#     nightly full-drain in cleanup-ec2-runners.yml terminating the pool
#     instance. There is no explicit release subcommand or step anywhere in
#     either workflow.
#
# Usage:
#   eip-mutex.sh wait-until-free <allocation-id>
#   eip-mutex.sh verify <allocation-id> <instance-id>
#   eip-mutex.sh reap-if-stopped <allocation-id>
#
# wait-until-free: a PRECONDITION GATE run by ci.yml's start-runner job
# BEFORE the cold-launch "Start EC2 runner" step -- it is not an
# acquire-and-hold primitive, and it never associates the EIP itself. It
# blocks until the allocation is unassociated, or is held by a stopped
# instance that does not belong to this repository (in which case it reaps
# that instance's association and re-checks), so that the action's own
# eip-allocation-id association on the next step doesn't race a still-live
# holder elsewhere. If the current holder is THIS repository's own stopped
# warm-pool instance, it is deliberately left alone -- see
# cmd_wait_until_free's own comments for why. Prints nothing to stdout on
# success (all progress/retry logging goes to stderr); only its exit code
# matters. Env overrides: EIP_MUTEX_TIMEOUT_SECONDS (default 1800),
# EIP_MUTEX_POLL_INTERVAL_SECONDS (default 15). Retries happen on a flat,
# fixed poll interval -- there is no exponential/jittered backoff here, in
# the old acquire subcommand, or anywhere else in this script.
#
# verify: a READ-ONLY, post-hoc ownership check run by ci.yml's start-runner
# job AFTER `mode: start` returns (regardless of whether that was a cold
# launch or a warm restart). It confirms the EIP's current association is
# still with the given <instance-id> -- catching both the residual race
# window between wait-until-free and the action's own cold-launch associate
# call, and a warm-restart cross-repo steal that could have hit the pool
# instance while it sat stopped. On a match, prints exactly one line to
# stdout -- the EIP's public IP, via the same format resolve_public_ip
# returns -- and exits 0. On a mismatch or an unassociated EIP, it logs
# clearly to stderr and exits 1: a hard failure the caller must propagate,
# never proceeding to run tests without this confirmation.
#
# reap-if-stopped: only disassociates if the instance currently holding the
# EIP is in state "stopped" -- a running/pending/stopping instance is a
# legitimate live claim, not a leak, and is left alone. Its behavior is
# unchanged, but it is no longer invoked directly by any workflow step; it
# is now purely an internal helper that wait-until-free calls in-process
# (a plain function call, not a subprocess re-invocation), plus a manual
# escape hatch for operators who need to force-disassociate a leaked EIP.
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
# The one narrow exception to "never swallow a failure" anywhere in this
# script is a describe-instances call that fails specifically with
# InvalidInstanceID.NotFound on the EIP's current holder inside
# wait-until-free -- see cmd_wait_until_free's own comments for why that
# specific, identifiable failure (and only that one) is treated as
# corroborating "free" rather than as an unknown error.
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

cmd_wait_until_free() {
  local allocation_id="$1"
  local start_time elapsed instance_id instance_state owner_repo err_output
  start_time="$(date +%s)"

  while true; do
    instance_id="$(resolve_associated_instance_id "$allocation_id")"

    if [ -z "$instance_id" ] || [ "$instance_id" = "None" ]; then
      log "allocation $allocation_id is unassociated; free to launch"
      return 0
    fi

    err_output="$(mktemp)"
    if instance_state="$(aws ec2 describe-instances \
        --instance-ids "$instance_id" \
        --query 'Reservations[0].Instances[0].State.Name' \
        --output text 2>"$err_output")"; then
      rm -f "$err_output"
    elif grep -q 'InvalidInstanceID\.NotFound' "$err_output"; then
      # A genuinely gone instance is the one describe-instances failure mode
      # allowed to short-circuit to "free": AWS auto-disassociates an EIP
      # the moment its instance terminates, so an EIP whose association
      # metadata still points at a NotFound instance-id can only be stale,
      # lagging data about a claim that no longer exists -- it cannot
      # represent a live holder. The upcoming cold launch's own
      # eip-allocation-id association will simply overwrite/reassociate
      # over whatever stale metadata remains, so no disassociate call is
      # attempted here.
      log "holder instance $instance_id for allocation $allocation_id no longer exists (terminated and aged out of the default describe-instances view); treating the allocation as free"
      rm -f "$err_output"
      return 0
    else
      # Any OTHER describe-instances failure (throttling, IAM denial,
      # network blip, region misconfiguration, ...) is a transient/unknown
      # AWS-side problem, NEVER "free" -- it could be masking a real issue
      # (e.g. an IAM policy regression, or a throttling storm hiding a
      # still-running instance), so fall through to the generic retry/
      # backoff below instead of guessing.
      log "describe-instances failed for holder $instance_id (treating as a transient error, NOT as free -- will retry):"
      cat "$err_output" >&2
      rm -f "$err_output"
      instance_state=""
    fi

    if [ "$instance_state" = "stopped" ]; then
      # The second describe-instances call below (the ownership tag lookup)
      # is deliberately given ZERO error tolerance and is allowed to crash
      # this subcommand under set -e if it fails. At this point the
      # instance is known to exist (the first call just succeeded above),
      # so a failure here is a rare anomaly, and both ways of papering over
      # it -- assume "ours, don't reap" or assume "not ours, reap" -- are
      # worse than failing loudly. Reaping on a guess in particular could
      # strip the EIP off this repo's own live warm-pool instance,
      # recreating the exact production bug this design fixes.
      owner_repo="$(aws ec2 describe-instances \
        --instance-ids "$instance_id" \
        --query "Reservations[0].Instances[0].Tags[?Key=='GitHubRepository'].Value | [0]" \
        --output text)"
      if [ -n "${GITHUB_REPOSITORY:-}" ] && [ "$owner_repo" = "$GITHUB_REPOSITORY" ]; then
        log "allocation $allocation_id is held by $instance_id, a stopped instance tagged for this repository ($GITHUB_REPOSITORY) -- this is our own warm-pool instance about to be reused by the upcoming mode:start call; leaving the EIP attached"
        return 0
      fi
      log "allocation $allocation_id is held by stopped instance $instance_id (GitHubRepository tag: ${owner_repo:-none}, not this repository); reaping"
      # Called in-process (a plain function call, not a subprocess
      # re-invocation) -- see the header comment's reap-if-stopped section.
      # Its stdout is discarded here: cmd_reap_if_stopped reports status via
      # printf to stdout (it's also a standalone CLI subcommand), but
      # wait-until-free's own documented contract is "prints nothing to
      # stdout on success", so that status prose must not leak through.
      # The call is also guarded, unlike the read-only describe-* calls
      # above: if the foreign owner reclaims/restarts the instance in the
      # narrow window between the state check above and here,
      # disassociate-address inside cmd_reap_if_stopped can fail (e.g. a
      # stale association id). That failure is a transient race, not a
      # terminal error, so it is logged and retried like every other
      # holder-is-live path below rather than aborting the whole subcommand
      # under set -e.
      if ! cmd_reap_if_stopped "$allocation_id" >/dev/null; then
        log "reap attempt for allocation $allocation_id (stopped instance $instance_id) failed -- likely raced by its owner reclaiming the instance; will retry"
      fi
      elapsed="$(( $(date +%s) - start_time ))"
      if [ "$elapsed" -ge "$EIP_MUTEX_TIMEOUT_SECONDS" ]; then
        log "timed out after ${elapsed}s waiting for allocation $allocation_id to become free (holder: $instance_id, state: $instance_state)"
        return 1
      fi
      sleep 2 # let the disassociate-address above propagate before re-checking
      continue
    fi

    elapsed="$(( $(date +%s) - start_time ))"
    if [ "$elapsed" -ge "$EIP_MUTEX_TIMEOUT_SECONDS" ]; then
      log "timed out after ${elapsed}s waiting for allocation $allocation_id to become free (holder: $instance_id, state: ${instance_state:-unknown/error})"
      return 1
    fi
    log "allocation $allocation_id is held by instance $instance_id (state: ${instance_state:-unknown/error}); retrying in ${EIP_MUTEX_POLL_INTERVAL_SECONDS}s (elapsed ${elapsed}s/${EIP_MUTEX_TIMEOUT_SECONDS}s)"
    sleep "$EIP_MUTEX_POLL_INTERVAL_SECONDS"
  done
}

cmd_verify() {
  local allocation_id="$1" expected_instance_id="$2"
  local current_instance_id public_ip

  current_instance_id="$(resolve_associated_instance_id "$allocation_id")"

  if [ -z "$current_instance_id" ] || [ "$current_instance_id" = "None" ]; then
    log "ownership check failed: allocation $allocation_id is not associated with any instance (expected instance $expected_instance_id)"
    return 1
  fi

  if [ "$current_instance_id" != "$expected_instance_id" ]; then
    log "ownership check failed: allocation $allocation_id is associated with instance $current_instance_id, not this job's instance $expected_instance_id"
    return 1
  fi

  public_ip="$(resolve_public_ip "$allocation_id")"
  log "verified: allocation $allocation_id is associated with this job's instance $expected_instance_id"
  printf '%s\n' "$public_ip"
}

main() {
  local subcommand="${1:-}"
  case "$subcommand" in
    wait-until-free)
      [ "$#" -eq 2 ] || { log "usage: eip-mutex.sh wait-until-free <allocation-id>"; exit 1; }
      cmd_wait_until_free "$2"
      ;;
    verify)
      [ "$#" -eq 3 ] || { log "usage: eip-mutex.sh verify <allocation-id> <instance-id>"; exit 1; }
      cmd_verify "$2" "$3"
      ;;
    reap-if-stopped)
      [ "$#" -eq 2 ] || { log "usage: eip-mutex.sh reap-if-stopped <allocation-id>"; exit 1; }
      cmd_reap_if_stopped "$2"
      ;;
    *)
      log "usage: eip-mutex.sh {wait-until-free <allocation-id>|verify <allocation-id> <instance-id>|reap-if-stopped <allocation-id>}"
      exit 1
      ;;
  esac
}

main "$@"

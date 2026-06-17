#!/usr/bin/env bash
# Render a consolidated security-scan summary (GitHub-flavored Markdown) from the
# JSON outputs of the CI security jobs. Consumed by the `security-report` job in
# .github/workflows/ci.yml: written to the run's job summary, uploaded as the
# `security-summary` artifact, and posted as a sticky PR comment.
#
# Usage:
#   security-summary.sh TRIVY_JSON GOVULN_JSON SBOM_JSON GO_MOD CODEQL_JSON
#
# Every input is best-effort: a missing, empty, or unparseable file degrades
# that section gracefully rather than failing. This script is REPORTING ONLY and
# never exits non-zero on scan content — the merge gates live in the scan jobs.
# Pass/fail shown in the status table comes from the gate jobs' results
# (SECURITY_RESULT / GOVULNCHECK_RESULT env), not from re-deriving it here.
set -uo pipefail

TRIVY="${1:-}"
GOVULN="${2:-}"
SBOM="${3:-}"
GOMOD="${4:-go.mod}"
CODEQL="${5:-}"

MARKER='<!-- security-scan-summary -->'
SHA_SHORT="${COMMIT_SHA:0:8}"
BRANCH="${BRANCH:-unknown}"
NOW="$(date -u '+%Y-%m-%d %H:%M UTC')"

# jq wrapper: print the filter result, or a fallback when the file is
# missing/empty/invalid, so one dead scan job can't break the whole report.
# Args: <file> <filter> <fallback> [slurp-flag e.g. -s]
jqf() {
  local file="$1" filter="$2" fallback="$3" slurp="${4:-}"
  if [ -s "$file" ]; then
    jq ${slurp} -r "$filter" "$file" 2>/dev/null || printf '%s' "$fallback"
  else
    printf '%s' "$fallback"
  fi
}

# success -> check, failure -> cross, anything else (skipped/unknown) -> circle
emoji() {
  case "$1" in
    success) printf '✅' ;;
    failure) printf '❌' ;;
    *) printf '⚪' ;;
  esac
}

# --- Trivy counts ----------------------------------------------------------
tv_crit=$(jqf "$TRIVY" '[.Results[]?.Vulnerabilities[]? | select(.Severity=="CRITICAL")] | length' 0)
tv_high=$(jqf "$TRIVY" '[.Results[]?.Vulnerabilities[]? | select(.Severity=="HIGH")]     | length' 0)
tv_med=$(jqf  "$TRIVY" '[.Results[]?.Vulnerabilities[]? | select(.Severity=="MEDIUM")]   | length' 0)
tv_low=$(jqf  "$TRIVY" '[.Results[]?.Vulnerabilities[]? | select(.Severity=="LOW")]      | length' 0)
tv_unkn=$(jqf "$TRIVY" '[.Results[]?.Vulnerabilities[]? | select(.Severity=="UNKNOWN")]  | length' 0)
tv_misconf=$(jqf "$TRIVY" '[.Results[]?.Misconfigurations[]? | select(.Status=="FAIL")] | length' 0)
tv_secrets=$(jqf "$TRIVY" '[.Results[]?.Secrets[]?] | length' 0)
tv_lic=$(jqf "$TRIVY" '[.Results[]?.Licenses[]?] | length' 0)

# --- govulncheck counts (NDJSON stream -> slurp) ---------------------------
gv_reach=$(jqf "$GOVULN" '[.[] | select(.finding!=null) | .finding | select(.trace[0].function!=null) | .osv] | unique | length' 0 -s)
gv_total=$(jqf "$GOVULN" '[.[] | select(.finding!=null) | .finding.osv] | unique | length' 0 -s)
gv_imported=$(( gv_total - gv_reach ))
[ "$gv_imported" -lt 0 ] && gv_imported=0

# --- SBOM / dependency overview --------------------------------------------
sbom_comp=$(jqf "$SBOM" '[.components[]?] | length' 0)
read -r mod_direct mod_indirect < <(awk '
  /^require[[:space:]]*\(/ { inblk=1; next }
  inblk && /^\)/ { inblk=0; next }
  /^require[[:space:]]+[^(]/ { if ($0 ~ /\/\/[[:space:]]*indirect/) ind++; else dir++; next }
  inblk && NF>=2 && $1!="//" { if ($0 ~ /\/\/[[:space:]]*indirect/) ind++; else dir++ }
  END { printf "%d %d", dir+0, ind+0 }
' "$GOMOD" 2>/dev/null || echo "0 0")

# --- CodeQL (code scanning) open alerts ------------------------------------
cq_total=$(jqf "$CODEQL" 'if type=="array" then length else 0 end' 0)
cq_crit=$(jqf "$CODEQL"  'if type=="array" then [.[]|select(.rule.security_severity_level=="critical")]|length else 0 end' 0)
cq_high=$(jqf "$CODEQL"  'if type=="array" then [.[]|select(.rule.security_severity_level=="high")]|length else 0 end' 0)

# --- status from the actual gate jobs --------------------------------------
trivy_status=$(emoji "${SECURITY_RESULT:-}")
govuln_status=$(emoji "${GOVULNCHECK_RESULT:-}")

# ===========================================================================
# Markdown. First line is the sticky marker so the workflow can find + update
# this exact comment instead of posting a new one each push.
# ===========================================================================
printf '%s\n' "$MARKER"
printf '## 🔐 Security scan summary\n\n'
printf '_Commit `%s` · branch `%s` · generated %s_\n\n' "$SHA_SHORT" "$BRANCH" "$NOW"

printf '| Scan | Gate | Findings |\n'
printf '|---|:---:|---|\n'
printf '| **Trivy — vulnerabilities** (SCA) | %s | 🔴 %s critical · 🟠 %s high · 🟡 %s medium · ⚪ %s low · %s unknown |\n' \
  "$trivy_status" "$tv_crit" "$tv_high" "$tv_med" "$tv_low" "$tv_unkn"
printf '| **Trivy — IaC misconfig** | %s | %s failing |\n' "$trivy_status" "$tv_misconf"
printf '| **Trivy — secrets** | %s | %s detected |\n' "$trivy_status" "$tv_secrets"
printf '| **Trivy — licenses** | %s | %s policy finding(s) |\n' "$trivy_status" "$tv_lic"
printf '| **govulncheck** (reachability) | %s | %s reachable · %s imported-only |\n' \
  "$govuln_status" "$gv_reach" "$gv_imported"
printf '| **CodeQL** (code scanning, open) | — | %s open · 🔴 %s critical · 🟠 %s high |\n' \
  "$cq_total" "$cq_crit" "$cq_high"
printf '\n'
printf '> **Gate** (blocks merge): HIGH/CRITICAL **fixable** CVEs · reachable Go vulnerabilities · forbidden licenses · committed secrets. '
printf 'Counts above are the *full* picture (incl. medium/low and unfixed), so they can exceed what actually gates. '
printf 'CodeQL is informational here. ✅ = gate job passed, ❌ = failed, ⚪ = skipped.\n\n'

# --- dependency overview ---------------------------------------------------
printf '### 📦 Dependencies\n\n'
printf -- '- **SBOM components (CycloneDX):** %s\n' "$sbom_comp"
printf -- '- **Go modules in `go.mod`:** %s direct · %s indirect\n\n' "$mod_direct" "$mod_indirect"

lic_breakdown=$(jqf "$SBOM" '
  [.components[]?.licenses[]? | (.license.id // .license.name // .expression // "UNKNOWN")]
  | group_by(.) | map({k: .[0], n: length}) | sort_by(-.n) | .[:10][]
  | "| \(.k) | \(.n) |"' "")
if [ -n "$lic_breakdown" ]; then
  printf '<details><summary>License breakdown (top 10)</summary>\n\n'
  printf '| License | Components |\n|---|---:|\n%s\n\n</details>\n\n' "$lic_breakdown"
fi

# --- detail sections (only when there is something to show) ----------------
vuln_rows=$(jqf "$TRIVY" '
  [.Results[]?.Vulnerabilities[]?]
  | unique_by([.VulnerabilityID, .PkgName])
  | sort_by({"CRITICAL":0,"HIGH":1,"MEDIUM":2,"LOW":3,"UNKNOWN":4}[.Severity] // 5)
  | .[:50][]
  | "| \(.Severity) | \(.VulnerabilityID) | \(.PkgName) | \(.InstalledVersion) | \((.FixedVersion // "") | if . == "" then "—" else . end) |"' "")
if [ -n "$vuln_rows" ]; then
  printf '<details><summary>Vulnerabilities (Trivy, top 50)</summary>\n\n'
  printf '| Severity | ID | Package | Installed | Fixed |\n|---|---|---|---|---|\n%s\n\n</details>\n\n' "$vuln_rows"
fi

gv_rows=$(jqf "$GOVULN" '
  [.[] | select(.finding!=null) | .finding | select(.trace[0].function!=null)
   | {osv, module: .trace[0].module, fn: .trace[0].function}]
  | unique_by(.osv) | .[]
  | "| \(.osv) | \(.module // "—") | `\(.fn // "—")` |"' "" -s)
if [ -n "$gv_rows" ]; then
  printf '<details><summary>Reachable Go vulnerabilities (govulncheck)</summary>\n\n'
  printf '| Advisory | Module | Called symbol |\n|---|---|---|\n%s\n\n</details>\n\n' "$gv_rows"
fi

lic_rows=$(jqf "$TRIVY" '
  [.Results[]?.Licenses[]?] | .[]
  | "| \(.Severity) | \(.PkgName // .FilePath // "—") | \(.Name) | \(.Category // "—") |"' "")
if [ -n "$lic_rows" ]; then
  printf '<details><summary>License findings (Trivy)</summary>\n\n'
  printf '| Severity | Package/File | License | Category |\n|---|---|---|---|\n%s\n\n</details>\n\n' "$lic_rows"
fi

misconf_rows=$(jqf "$TRIVY" '
  [.Results[]? | .Target as $t | (.Misconfigurations[]? | select(.Status=="FAIL") | {t: $t, id: .ID, sev: .Severity, title: .Title})]
  | .[] | "| \(.sev) | \(.id) | \(.title) | \(.t) |"' "")
if [ -n "$misconf_rows" ]; then
  printf '<details><summary>IaC misconfigurations (Trivy)</summary>\n\n'
  printf '| Severity | ID | Title | Target |\n|---|---|---|---|\n%s\n\n</details>\n\n' "$misconf_rows"
fi

# Secrets: report rule + location only, NEVER the matched value, to avoid
# re-leaking a credential into the PR comment / artifact.
secret_rows=$(jqf "$TRIVY" '
  [.Results[]? | .Target as $t | (.Secrets[]? | {t: $t, rule: .RuleID, sev: .Severity, title: .Title, line: .StartLine})]
  | .[] | "| \(.sev) | \(.rule) | \(.title) | \(.t):\(.line) |"' "")
if [ -n "$secret_rows" ]; then
  printf '<details><summary>Secret findings (Trivy — values redacted)</summary>\n\n'
  printf '| Severity | Rule | Title | Location |\n|---|---|---|---|\n%s\n\n</details>\n\n' "$secret_rows"
fi

printf -- '---\n'
printf '📎 Full machine-readable reports are attached to this run as artifacts: '
printf '`security-summary`, `trivy-json`, `govulncheck-json`, `sbom-cyclonedx`.\n'

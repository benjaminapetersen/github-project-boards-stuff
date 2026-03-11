#!/usr/bin/env bash
# scripts/deploy-env.sh — Read the .env file, update the ConfigMap and Secret
# on-cluster. The GITHUB_TOKEN never touches a local file.
#
# Usage:
#   scripts/deploy-env.sh [ENV_FILE]
#
# ENV_FILE defaults to .env/kube-board.env
#
# Prerequisites:
#   - kubectl configured to talk to the target cluster
#   - The kube-board namespace must exist:
#       kubectl apply -f deploy/namespace.yaml
#
# What it does:
#   1. Sources the .env file to get all exported variables
#   2. Patches deploy/configmap.yaml (in-memory, not on disk) with the values
#      and applies it to the cluster
#   3. Creates/updates the kube-board-token Secret on-cluster using
#      kubectl create secret --dry-run=client | kubectl apply -f -
#      (the token is never written to a local file)

set -euo pipefail

NAMESPACE="kube-board"
ENV_FILE="${1:-.env/kube-board.env}"

if [[ ! -f "$ENV_FILE" ]]; then
  echo "Error: env file not found: $ENV_FILE" >&2
  echo "Usage: $0 [path/to/.env]" >&2
  exit 1
fi

# ---------- 1. Source the env file in a subshell-safe way ----------
# We need the exported values, but we don't want to pollute the current
# shell permanently. We'll source into the current script (it exits anyway).
set -a  # auto-export all variables
# shellcheck disable=SC1090
source "$ENV_FILE"
set +a

# ---------- 2. Validate required token ----------
if [[ -z "${GITHUB_TOKEN:-}" ]]; then
  echo "Error: GITHUB_TOKEN is not set in $ENV_FILE" >&2
  exit 1
fi

# ---------- 3. Ensure namespace exists ----------
kubectl apply -f deploy/namespace.yaml

# ---------- 4. Create/update Secret on-cluster ----------
# Uses --dry-run=client + kubectl apply so it's idempotent.
# The token never appears in a local file.
echo "Applying Secret kube-board-token to namespace $NAMESPACE..."
kubectl -n "$NAMESPACE" create secret generic kube-board-token \
  --from-literal=GITHUB_TOKEN="$GITHUB_TOKEN" \
  --dry-run=client -o yaml | kubectl apply -f -

# ---------- 5. Build and apply ConfigMap from env values ----------
# All keys from the configmap template, mapped to env var names.
# If the env var is unset, the value defaults to empty string.
declare -A CONFIG_KEYS=(
  [GITHUB_USERNAMES]="${GITHUB_USERNAMES:-}"
  [GITHUB_CORE_USERNAMES]="${GITHUB_CORE_USERNAMES:-}"
  [GITHUB_EXTENDED_USERNAMES]="${GITHUB_EXTENDED_USERNAMES:-}"
  [ENHANCEMENTS_REPO]="${ENHANCEMENTS_REPO:-kubernetes/enhancements}"
  [ENHANCEMENTS_LABELS]="${ENHANCEMENTS_LABELS:-sig/auth}"
  [GITHUB_KUBERNETES_MILESTONE]="${GITHUB_KUBERNETES_MILESTONE:-}"
  [GITHUB_KUBERNETES_RELEASE_SYNC_BOARD]="${GITHUB_KUBERNETES_RELEASE_SYNC_BOARD:-kubernetes/projects/241}"
  [GITHUB_KUBERNETES_RELEASE_SYNC_BOARD_FIELDS]="${GITHUB_KUBERNETES_RELEASE_SYNC_BOARD_FIELDS:-TrackingStatus,Stage,PRRStatus,EnhancementType,SIG,Milestone}"
  [ISSUES_REPO]="${ISSUES_REPO:-kubernetes/kubernetes}"
  [GITHUB_KUBERNETES_ISSUES_SIG_LABELS]="${GITHUB_KUBERNETES_ISSUES_SIG_LABELS:-}"
  [ISSUES_SEARCH_QUALIFIER]="${ISSUES_SEARCH_QUALIFIER:-involves}"
  [GITHUB_ADDITIONAL_ORGS]="${GITHUB_ADDITIONAL_ORGS:-}"
  [GITHUB_SEARCH_SINCE]="${GITHUB_SEARCH_SINCE:-}"
  [GITHUB_EXCLUDE_STATES]="${GITHUB_EXCLUDE_STATES:-closed}"
  [GITHUB_EXCLUDE_LABELS]="${GITHUB_EXCLUDE_LABELS:-}"
  [GITHUB_EXCLUDE_STATUSES]="${GITHUB_EXCLUDE_STATUSES:-}"
  [GITHUB_DEST_BOARD_OWNER]="${GITHUB_DEST_BOARD_OWNER:-}"
  [GITHUB_DEST_BOARD_NAME]="${GITHUB_DEST_BOARD_NAME:-}"
  [GITHUB_DEST_BOARD_PRIVACY]="${GITHUB_DEST_BOARD_PRIVACY:-private}"
  [GITHUB_DEST_BOARD_AUTHOR_FIELD_NAME]="${GITHUB_DEST_BOARD_AUTHOR_FIELD_NAME:-Item Author}"
  [GITHUB_LINK_REPOS]="${GITHUB_LINK_REPOS:-}"
  [GITHUB_DEST_BOARD_ADDITIONAL_VIEWS]="${GITHUB_DEST_BOARD_ADDITIONAL_VIEWS:-}"
  [GITHUB_SKIP_DEFAULT_VIEWS]="${GITHUB_SKIP_DEFAULT_VIEWS:-}"
  [GITHUB_DEST_BOARD_CUSTOM_FIELDS]="${GITHUB_DEST_BOARD_CUSTOM_FIELDS:-}"
  [GITHUB_AUTO_CUSTOM_FIELD_TO_REPO]="${GITHUB_AUTO_CUSTOM_FIELD_TO_REPO:-}"
  [GITHUB_CLOSED_ITEM_WINDOW]="${GITHUB_CLOSED_ITEM_WINDOW:-}"
)

echo "Applying ConfigMap kube-board-config to namespace $NAMESPACE..."

# Build the ConfigMap dynamically using kubectl create configmap --dry-run.
# This avoids editing the YAML template on disk.
CM_ARGS=()
for key in "${!CONFIG_KEYS[@]}"; do
  CM_ARGS+=("--from-literal=${key}=${CONFIG_KEYS[$key]}")
done

kubectl -n "$NAMESPACE" create configmap kube-board-config \
  "${CM_ARGS[@]}" \
  --dry-run=client -o yaml | kubectl apply -f -

# ---------- 6. Summary ----------
echo ""
echo "Done. Resources applied to namespace '$NAMESPACE':"
echo "  Secret/kube-board-token   (GITHUB_TOKEN from $ENV_FILE)"
echo "  ConfigMap/kube-board-config (all config vars from $ENV_FILE)"
echo ""
echo "Next steps:"
echo "  # Run the one-off job:"
echo "  kubectl apply -f deploy/job.yaml"
echo "  kubectl -n $NAMESPACE logs -f job/kube-board-run"
echo ""
echo "  # Or deploy the CronJob:"
echo "  kubectl apply -f deploy/cronjob.yaml"

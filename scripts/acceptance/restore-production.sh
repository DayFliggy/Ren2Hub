#!/usr/bin/env bash
set -euo pipefail
manifest=${1:?manifest required}
[[ -f "$manifest" ]] || exit 2
failed=0
for role in postgres redis app; do
  record=$(awk -F '\t' -v role="$role" '$1==role {print}' "$manifest")
  IFS=$'\t' read -r saved_role container_name container_id image_id was_running restart_policy <<< "$record"
  [[ "$saved_role" == "$role" && "$container_name" == "/ren2hub"* && "$container_id" =~ ^[a-f0-9]{64}$ && -n "$restart_policy" ]] || exit 2
  [[ "$(docker inspect -f '{{.Name}}' "$container_id")" == "$container_name" ]] || { printf 'RESTORE_FAILED role=%s name_changed\n' "$role"; failed=1; continue; }
  [[ "$(docker inspect -f '{{.HostConfig.RestartPolicy.Name}}' "$container_id")" == "$restart_policy" ]] || { printf 'RESTORE_FAILED role=%s restart_policy_changed\n' "$role"; failed=1; continue; }
  if [[ "$(docker inspect -f '{{.Image}}' "$container_id")" != "$image_id" ]]; then
    printf 'RESTORE_FAILED role=%s image_changed\n' "$role"
    failed=1
    continue
  fi
  [[ "$was_running" == true ]] || continue
  docker start "$container_id" > /dev/null || { failed=1; continue; }
  healthy=0
  for attempt in $(seq 1 90); do
    state=$(docker inspect -f '{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$container_id")
    if [[ "$state" == 'running healthy' ]]; then healthy=1; break; fi
    sleep 2
  done
  if (( healthy == 0 )); then
    printf 'RESTORE_FAILED role=%s state=%s\n' "$role" "$state"
    failed=1
    # Do not start the application when either of its original dependencies failed.
    [[ "$role" != app ]] && exit 1
  fi
done
if (( failed != 0 )); then exit 1; fi
version=$(curl --fail --silent http://127.0.0.1:3000/api/status | python3 -c 'import json,sys; v=json.load(sys.stdin); assert v["success"]; print(v["data"]["version"])')
expected=$(< "$(dirname "$manifest")/original-version.txt")
[[ "$version" == "$expected" ]] || { printf 'RESTORE_FAILED application_version_changed\n'; exit 1; }
html=$(curl --fail --silent http://127.0.0.1:3000/)
asset=$(printf '%s' "$html" | sed -n 's/.*src="\(\/assets\/[^" ]*\.js\)".*/\1/p' | head -n 1)
[[ -n "$asset" ]] || { printf 'RESTORE_FAILED missing_vue_entry\n'; exit 1; }
curl --fail --silent --output /dev/null "http://127.0.0.1:3000$asset"
printf 'RESTORE_OK version=%s original_container_ids_preserved\n' "$version"

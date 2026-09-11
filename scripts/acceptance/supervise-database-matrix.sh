#!/usr/bin/env bash
set -euo pipefail
run_id=${1:?run_id required}
image=${2:?acceptance image required}
report_dir=${3:?report directory required}
[[ "$run_id" =~ ^[a-z0-9-]{8,64}$ && "$report_dir" == /* ]] || exit 2
script_dir=$(cd -- "$(dirname -- "$0")" && pwd)
mkdir -p "$report_dir"
chmod 700 "$report_dir"
manifest="$report_dir/original-containers.tsv"
[[ ! -e "$manifest" ]] || { printf 'existing manifest; use its recovery script\n'; exit 2; }
free_mb=$(awk '/MemAvailable/ {print int($2/1024)}' /proc/meminfo)
disk_mb=$(df -Pm "$report_dir" | awk 'NR==2 {print $4}')
(( free_mb >= 1536 && disk_mb >= 10240 )) || { printf 'RESOURCE_GATE_FAILED memory=%s disk=%s\n' "$free_mb" "$disk_mb"; exit 3; }
docker image inspect "$image" > /dev/null
exec 9>/run/lock/ren2hub-routing-acceptance.lock
flock -n 9 || exit 3
if systemctl is-active --quiet ren2hub-backup.service; then printf 'backup active\n'; exit 3; fi
curl --fail --silent http://127.0.0.1:3000/api/status | python3 -c 'import json,sys; v=json.load(sys.stdin); assert v["success"]; print(v["data"]["version"])' > "$report_dir/original-version.txt"
printf 'role\tcontainer_name\tcontainer_id\timage_id\twas_running\trestart_policy\n' > "$manifest"
for pair in postgres:ren2hub-postgres redis:ren2hub-redis app:ren2hub; do
  role=${pair%%:*}
  container=${pair#*:}
  record=$(docker inspect -f '{{.Name}}\t{{.Id}}\t{{.Image}}\t{{.State.Running}}\t{{.HostConfig.RestartPolicy.Name}}' "$container")
  printf '%s\t%b\n' "$role" "$record" >> "$manifest"
done
chmod 600 "$manifest"
restore() {
  status=$?
  trap - EXIT INT TERM
  # Cancel only resources carrying this run's exact ownership label.
  for id in $(docker ps -aq --filter "label=ren2hub.acceptance.run=$run_id"); do
    if ! docker stop --time 15 "$id" > /dev/null; then
      printf 'TEST_STOP_FAILED container=%s\n' "$id" >> "$report_dir/cleanup.log"
      status=4
    fi
  done
  if ! bash "$script_dir/restore-production.sh" "$manifest" > "$report_dir/restore.log" 2>&1; then
    printf 'RESTORE_FAILED report=%s\n' "$report_dir/restore.log"
    exit 4
  fi
  cat "$report_dir/restore.log"
  exit "$status"
}
trap restore EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
stop_started_at=$(date --iso-8601=seconds)
for role in app postgres redis; do
  id=$(awk -F '\t' -v role="$role" '$1==role {print $3}' "$manifest")
  docker stop --time 180 "$id" > /dev/null
  docker inspect -f '{{.Id}} {{.State.Status}} {{.State.ExitCode}} {{.State.FinishedAt}}' "$id" >> "$report_dir/original-stop.log"
done
app_id=$(awk -F '\t' '$1=="app" {print $3}' "$manifest")
docker logs --since "$stop_started_at" "$app_id" > "$report_dir/accounting-flush-receipt.log" 2>&1
grep -F 'server drained and exited' "$report_dir/accounting-flush-receipt.log" > /dev/null || {
  printf 'ACCOUNTING_FLUSH_RECEIPT_MISSING\n'
  exit 4
}
free_mb=$(awk '/MemAvailable/ {print int($2/1024)}' /proc/meminfo)
disk_mb=$(df -Pm "$report_dir" | awk 'NR==2 {print $4}')
(( free_mb >= 1536 && disk_mb >= 10240 )) || {
  printf 'POST_STOP_RESOURCE_GATE_FAILED memory=%s disk=%s\n' "$free_mb" "$disk_mb"
  exit 3
}
printf 'POST_STOP_RESOURCE_GATE_OK memory=%s disk=%s cpu_limit=1.5\n' "$free_mb" "$disk_mb" >> "$report_dir/original-stop.log"
timeout --signal=TERM --kill-after=60s 1800 bash "$script_dir/run-database-matrix.sh" "$run_id" "$image" "$report_dir"

#!/usr/bin/env bash
set -euo pipefail

run_id=${1:?run_id required}
image=${2:?acceptance image required}
report_dir=${3:?absolute report directory required}
[[ "$run_id" =~ ^[a-z0-9-]{8,64}$ ]] || exit 2
[[ "$report_dir" == /* ]] || exit 2
mkdir -p "$report_dir"
[[ ! -e "$report_dir/results.tsv" ]] || { printf 'run report already exists\n'; exit 2; }
network="r2h-test-$run_id"
db_name="r2h-db-$run_id"
test_name="r2h-check-$run_id"
owned_label="ren2hub.acceptance.run=$run_id"

cleanup() {
  for name in "$test_name" "$db_name"; do
    if [[ "$(docker inspect -f '{{ index .Config.Labels "ren2hub.acceptance.run" }}' "$name" 2>/dev/null || true)" == "$run_id" ]]; then
      docker rm -f -v "$name" > /dev/null
    fi
  done
  if [[ "$(docker network inspect -f '{{ index .Labels "ren2hub.acceptance.run" }}' "$network" 2>/dev/null || true)" == "$run_id" ]]; then
    docker network rm "$network" > /dev/null
  fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
docker network create --internal --label "$owned_label" "$network" > /dev/null
printf 'database\tstatus\timage\n' > "$report_dir/results.tsv"
failed=0
for spec in sqlite postgres:9.6 postgres:15-bookworm mysql:5.7.8 mysql:8.0 clickhouse/clickhouse-server:24.8; do
  free_mb=$(awk '/MemAvailable/ {print int($2/1024)}' /proc/meminfo)
  if (( free_mb < 512 )); then printf 'ABORT memory=%s MiB\n' "$free_mb"; exit 3; fi
  name=${spec//[:.\/]/-}
  dsn="local"
  log_dsn=""
  test_pattern='^TestRoutingAcceptanceDatabase$'
  if [[ "$spec" != sqlite ]]; then
    if ! docker image inspect "$spec" > /dev/null 2>&1; then
      printf '%s\tNOT_RUN\t%s\n' "$spec" "$spec" >> "$report_dir/results.tsv"
      failed=1
      continue
    fi
    if [[ "$spec" == clickhouse/* ]]; then
      docker run -d --name "$db_name" --label "$owned_label" --network "$network" --network-alias database \
        --memory 1280m --memory-swap 1280m -e CLICKHOUSE_DB=acceptance -e CLICKHOUSE_USER=acceptance -e CLICKHOUSE_PASSWORD=synthetic-test-only -e CLICKHOUSE_DEFAULT_ACCESS_MANAGEMENT=1 \
        "$spec" > /dev/null
      log_dsn='clickhouse://acceptance:synthetic-test-only@database:9000/acceptance'
      test_pattern='^TestRoutingAcceptanceClickHouse$'
      probe=(clickhouse-client --user acceptance --password synthetic-test-only --query 'SELECT 1')
    elif [[ "$spec" == postgres:* ]]; then
      docker run -d --name "$db_name" --label "$owned_label" --network "$network" --network-alias database \
        --memory 768m --memory-swap 768m -e POSTGRES_DB=acceptance -e POSTGRES_HOST_AUTH_METHOD=trust -e POSTGRES_PASSWORD=synthetic-test-only \
        "$spec" > /dev/null
      dsn='postgres://postgres:synthetic-test-only@database:5432/acceptance?sslmode=disable'
      probe=(pg_isready -U postgres -d acceptance)
    else
      docker run -d --name "$db_name" --label "$owned_label" --network "$network" --network-alias database \
        --memory 1024m --memory-swap 1024m -e MYSQL_DATABASE=acceptance -e MYSQL_ALLOW_EMPTY_PASSWORD=yes -e MYSQL_ROOT_HOST=% \
        "$spec" --character-set-server=utf8mb4 --collation-server=utf8mb4_unicode_ci --innodb-buffer-pool-size=67108864 > /dev/null
      dsn='root@tcp(database:3306)/acceptance?charset=utf8mb4&parseTime=true'
      probe=(mysqladmin ping --silent)
    fi
    ready=0
    for attempt in $(seq 1 90); do
      if docker exec "$db_name" "${probe[@]}" > /dev/null 2>&1; then ready=1; break; fi
      sleep 2
    done
    if (( ready == 0 )); then
      printf '%s\tNOT_RUN\t%s\n' "$spec" "$spec" >> "$report_dir/results.tsv"
      docker logs "$db_name" > "$report_dir/$name-startup.log" 2>&1
      docker rm -f -v "$db_name" > /dev/null
      failed=1
      continue
    fi
  fi
  if docker run --rm --name "$test_name" --label "$owned_label" --network "$network" --memory 768m --memory-swap 768m \
    -e REN2HUB_ISOLATED_ACCEPTANCE=synthetic-empty-database -e REN2HUB_ACCEPTANCE_DSN="$dsn" -e REN2HUB_ACCEPTANCE_LOG_DSN="$log_dsn" \
    "$image" -test.run "$test_pattern" -test.timeout 60s -test.v > "$report_dir/$name.log" 2>&1; then
    printf '%s\tPASS\t%s\n' "$spec" "$spec" >> "$report_dir/results.tsv"
  else
    printf '%s\tFAIL\t%s\n' "$spec" "$spec" >> "$report_dir/results.tsv"
    failed=1
  fi
  if [[ "$spec" != sqlite ]]; then docker rm -f -v "$db_name" > /dev/null; fi
done
cat "$report_dir/results.tsv"
exit "$failed"

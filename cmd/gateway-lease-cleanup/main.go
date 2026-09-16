// gateway-lease-cleanup removes only retired user/channel admission keys.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
)

const legacyNamespace = "route:lease:v2:"

type cleanupReport struct {
	Types    map[string]int64 `json:"key_types"`
	Observed int64            `json:"observed"`
	Unlinked int64            `json:"unlinked"`
	DryRun   bool             `json:"dry_run"`
}

func cleanupLegacyAdmission(ctx context.Context, client *redis.Client, remove bool) (cleanupReport, error) {
	report := cleanupReport{Types: make(map[string]int64), DryRun: !remove}
	var cursor uint64
	for {
		keys, next, err := client.Scan(ctx, cursor, legacyNamespace+"*", 256).Result()
		if err != nil {
			return report, fmt.Errorf("scan legacy admission namespace: %w", err)
		}
		for start := 0; start < len(keys); start += 256 {
			batch := keys[start:min(start+256, len(keys))]
			existing := make([]string, 0, len(batch))
			for _, key := range batch {
				if !strings.HasPrefix(key, legacyNamespace) {
					return report, fmt.Errorf("scan returned a key outside the admission namespace")
				}
				kind, err := client.Type(ctx, key).Result()
				if err != nil {
					return report, fmt.Errorf("read legacy key type: %w", err)
				}
				if kind == "none" {
					continue
				}
				report.Types[kind]++
				report.Observed++
				existing = append(existing, key)
			}
			if remove && len(existing) > 0 {
				count, err := client.Unlink(ctx, existing...).Result()
				if err != nil {
					return report, fmt.Errorf("unlink legacy admission batch: %w", err)
				}
				report.Unlinked += count
			}
		}
		cursor = next
		if cursor == 0 {
			return report, nil
		}
	}
}

func main() {
	address := flag.String("addr", "127.0.0.1:6379", "Redis address")
	database := flag.Int("db", 0, "Redis database number")
	remove := flag.Bool("delete", false, "unlink legacy keys; default only reports types and counts")
	stopped := flag.Bool("old-process-stopped", false, "operator has verified the old gateway is drained, exited and has no writer")
	flag.Parse()
	if *remove && !*stopped {
		fmt.Fprintln(os.Stderr, "deletion requires --old-process-stopped after verifying old gateway exit")
		os.Exit(2)
	}
	client := redis.NewClient(&redis.Options{Addr: *address, DB: *database, Password: os.Getenv("REDIS_PASSWORD"), Username: os.Getenv("REDIS_USERNAME")})
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	report, err := cleanupLegacyAdmission(ctx, client, *remove)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	output, err := common.Marshal(report)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(string(output))
}

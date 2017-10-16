package cmp

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/outputs"
)

type Cmp struct {
	ApiUser     string
	ApiKey      string
	ResourceId  string
	CmpInstance string
	Timeout     internal.Duration
	Debug       bool

	client *http.Client
}

var sampleConfig = `
  # Cmp Api User and Key
  api_user = "my-api-user" # required.
  api_key = "my-api-key" # required.
  resource_id = "1234"

  # Cmp Instance URL
  cmp_instance = "https://yourcmpinstance" # required

  # Connection timeout.
  # timeout = "5s"

  # Print verbose debug messages to console
  debug = false
`

var translateMap = map[string]Translation{
	"cpu-usage.idle": {
		Name:       "cpu-usage",
		Unit:       "percent",
		Conversion: subtract_from_100_percent,
	},
	"cpu-usage.user": {
		Name: "cpu-usage-user",
		Unit: "percent",
	},
	"cpu-usage.system": {
		Name: "cpu-usage-system",
		Unit: "percent",
	},
	"mem-available.percent": {
		Name:       "memory-usage",
		Unit:       "percent",
		Conversion: subtract_from_100_percent,
	},
	"system-load1": {
		Name: "load-avg-1",
	},
	"system-load5": {
		Name: "load-avg-5",
	},
	"system-load15": {
		Name: "load-avg-15",
	},
	"disk-used.percent": {
		Name: "disk-usage",
		Unit: "percent",
	},
	"diskio-io.time": {
		Name: "disk-io-time-percent.cntr",
		Unit: "percent",
		// ms / 1000 for s then * 100 for percent
		Conversion: divide_by(10.0),
	},
	"diskio-reads": {
		Name: "disk-read-ops.cntr",
		Unit: "count",
	},
	"diskio-writes": {
		Name: "disk-write-ops.cntr",
		Unit: "count",
	},
	"diskio-read.time": {
		Name: "disk-read-time-percent.cntr",
		Unit: "percent",
		// ms / 1000 for s then * 100 for percent
		Conversion: divide_by(10.0),
	},
	"diskio-write.time": {
		Name: "disk-write-time-percent.cntr",
		Unit: "percent",
		// ms / 1000 for s then * 100 for percent
		Conversion: divide_by(10.0),
	},
	//     "system-uptime": {
	//         Name: "uptime",
	//     },
	"docker_container_cpu-usage.percent": {
		Name: "docker-cpu-usage",
		Unit: "percent",
	},
	"docker_container_mem-usage.percent": {
		Name: "docker-memory-usage",
		Unit: "percent",
	},
	"elasticsearch_cluster_health-status": {
		Name:       "es-status",
		Unit:       "",
		Conversion: es_cluster_health,
	},
	"elasticsearch_cluster_health-number.of.nodes": {
		Name: "es-nodes",
		Unit: "",
	},
	"elasticsearch_cluster_health-active.shards": {
		Name: "es-shards.active",
		Unit: "",
	},
	"elasticsearch_cluster_health-active.primary.shards": {
		Name: "es-shards.primary",
		Unit: "",
	},
	"elasticsearch_cluster_health-unassigned.shards": {
		Name: "es-shards.unassigned",
		Unit: "",
	},
	"elasticsearch_cluster_health-initializing.shards": {
		Name: "es-shards.initializing",
		Unit: "",
	},
	"elasticsearch_cluster_health-relocating.shards": {
		Name: "es-shards.relocating",
		Unit: "",
	},
	"elasticsearch_jvm-mem.heap.used.in.bytes": {
		Name: "es-memory-usage.heap.used",
		Unit: "B",
	},
	"elasticsearch_jvm-mem.heap.committed.in.bytes": {
		Name: "es-memory-usage.heap.committed",
		Unit: "B",
	},
	"elasticsearch_jvm-mem.non.heap.used.in.bytes": {
		Name: "es-memory-usage.nonheap.used",
		Unit: "B",
	},
	"elasticsearch_jvm-mem.non.heap.committed.in.bytes": {
		Name: "es-memory-usage.nonheap.committed",
		Unit: "B",
	},
	"elasticsearch_indices-search.query.total": {
		Name: "es-search-requests.query.cntr",
		Unit: "requests",
	},
	"elasticsearch_indices-search.fetch.total": {
		Name: "es-search-requests.fetch.cntr",
		Unit: "requests",
	},
	"elasticsearch_indices-search.query.current": {
		Name: "es-current-search-requests.query",
		Unit: "requests",
	},
	"elasticsearch_indices-search.fetch.current": {
		Name: "es-current-search-requests.fetch",
		Unit: "requests",
	},
	"elasticsearch_indices-search.query.time.in.millis": {
		Name:       "es-search-time.query.cntr",
		Unit:       "s",
		Conversion: divide_by(1000.0),
	},
	"elasticsearch_indices-search.fetch.time.in.millis": {
		Name:       "es-search-time.fetch.cntr",
		Unit:       "s",
		Conversion: divide_by(1000.0),
	},
	"elasticsearch_indices-get.total": {
		Name: "es-get-requests.get.cntr",
		Unit: "requests",
	},
	"elasticsearch_indices-get.exists.total": {
		Name: "es-get-requests.exists.cntr",
		Unit: "requests",
	},
	"elasticsearch_indices-get.missing.total": {
		Name: "es-get-requests.missing.cntr",
		Unit: "requests",
	},
	"elasticsearch_indices-get.time.in.millis": {
		Name:       "es-get-time.get",
		Unit:       "s",
		Conversion: divide_by(1000.0),
	},
	"elasticsearch_indices-get.exists.time.in.millis": {
		Name:       "es-get-time.exists",
		Unit:       "s",
		Conversion: divide_by(1000.0),
	},
	"elasticsearch_indices-get.missing.time.in.millis": {
		Name:       "es-get-time.missing",
		Unit:       "s",
		Conversion: divide_by(1000.0),
	},
	"elasticsearch_indices-indexing.index.total": {
		Name: "es-index-requests.index.cntr",
		Unit: "requests",
	},
	"elasticsearch_indices-indexing.index.current": {
		Name: "es-current-index-requests",
		Unit: "requests",
	},
	"elasticsearch_indices-indexing.delete.total": {
		Name: "es-index-requests.delete.cntr",
		Unit: "requests",
	},
	"elasticsearch_indices-indexing.index.time.in.millis": {
		Name:       "es-index-time.index",
		Unit:       "s",
		Conversion: divide_by(1000.0),
	},
	"elasticsearch_indices-indexing.delete.time.in.millis": {
		Name:       "es-index-time.delete",
		Unit:       "s",
		Conversion: divide_by(1000.0),
	},
	"elasticsearch_indices-flush.total.time.in.millis": {
		Name:       "es-index-time.flush.cntr",
		Unit:       "requests",
		Conversion: divide_by(1000.0),
	},
	"haproxy-active.servers": {
		Name: "haproxy-active-servers",
		Unit: "",
	},
	"haproxy-backup.servers": {
		Name: "haproxy-backup-servers",
		Unit: "",
	},
	"haproxy-bin": {
		Name: "haproxy-bytes-in",
		Unit: "B",
	},
	"haproxy-bout": {
		Name: "haproxy-bytes-out",
		Unit: "B",
	},
	"haproxy-check.duration": {
		Name:       "haproxy-check-duration",
		Unit:       "s",
		Conversion: divide_by(1000.0),
	},
	"haproxy-cli.abort": {
		Name: "haproxy-client-aborts",
		Unit: "count",
	},
	"haproxy-ctime": {
		Name:       "haproxy-connection-time",
		Unit:       "s",
		Conversion: divide_by(1000.0),
	},
	"haproxy-downtime": {
		Name: "haproxy-downtime",
		Unit: "s",
	},
	"haproxy-dreq": {
		Name: "haproxy-denied-requests",
		Unit: "count",
	},
	"haproxy-dresp": {
		Name: "haproxy-denied-responses",
		Unit: "count",
	},
	"haproxy-econ": {
		Name: "haproxy-error-connections",
		Unit: "count",
	},
	"haproxy-ereq": {
		Name: "haproxy-error-requests",
		Unit: "count",
	},
	"haproxy-eresp": {
		Name: "haproxy-error-responses",
		Unit: "count",
	},
	"haproxy-http.response.1xx": {
		Name: "haproxy-http-1xx",
		Unit: "responses",
	},
	"haproxy-http.response.2xx": {
		Name: "haproxy-http-2xx",
		Unit: "responses",
	},
	"haproxy-http.response.3xx": {
		Name: "haproxy-http-3xx",
		Unit: "responses",
	},
	"haproxy-http.response.4xx": {
		Name: "haproxy-http-4xx",
		Unit: "responses",
	},
	"haproxy-http.response.5xx": {
		Name: "haproxy-http-5xx",
		Unit: "responses",
	},
	"haproxy-lbtot": {
		Name: "haproxy-lbtot",
		Unit: "count",
	},
	"haproxy-qcur": {
		Name: "haproxy-queue-current",
		Unit: "requests",
	},
	"haproxy-qmax": {
		Name: "haproxy-queue-max",
		Unit: "requests",
	},
	"haproxy-qtime": {
		Name:       "haproxy-queue-time",
		Unit:       "s",
		Conversion: divide_by(1000.0),
	},
	"haproxy-rate": {
		Name: "haproxy-rate",
		Unit: "sessions/s",
	},
	"haproxy-rate.max": {
		Name: "haproxy-rate-max",
		Unit: "sessions/s",
	},
	"haproxy-req.rate": {
		Name: "haproxy-request-rate",
		Unit: "requests/s",
	},
	"haproxy-req.rate.max": {
		Name: "haproxy-request-rate-max",
		Unit: "requests/s",
	},
	"haproxy-req.tot": {
		Name: "haproxy-requests-total",
		Unit: "requests",
	},
	"haproxy-rtime": {
		Name:       "haproxy-response-time",
		Unit:       "s",
		Conversion: divide_by(1000.0),
	},
	"haproxy-scur": {
		Name: "haproxy-sessions-current",
		Unit: "sessions",
	},
	"haproxy-smax": {
		Name: "haproxy-sessions-max",
		Unit: "sessions",
	},
	"haproxy-srv.abort": {
		Name: "haproxy-server-aborts",
		Unit: "count",
	},
	"haproxy-stot": {
		Name: "haproxy-sessions-total",
		Unit: "sessions",
	},
	"haproxy-ttime": {
		Name:       "haproxy-total-time",
		Unit:       "s",
		Conversion: divide_by(1000.0),
	},
	"haproxy-wredis": {
		Name: "haproxy-warnings-redistributed",
		Unit: "count",
	},
	"haproxy-wretr": {
		Name: "haproxy-warnings-retried",
		Unit: "count",
	},
	"mongodb-open.connections": {
		Name: "mongodb-open-connections",
		Unit: "connections",
	},
	"mongodb-net.in.bytes": {
		Name: "mongodb-network-in",
		Unit: "B/s",
	},
	"mongodb-net.out.bytes": {
		Name: "mongodb-network-out",
		Unit: "B/s",
	},
	"mongodb-active.reads": {
		Name: "mongodb-active-reads",
		Unit: "",
	},
	"mongodb-active.writes": {
		Name: "mongodb-active-writes",
		Unit: "",
	},
	"mongodb-queued.reads": {
		Name: "mongodb-queued-reads",
		Unit: "",
	},
	"mongodb-queued.writes": {
		Name: "mongodb-queued-writes",
		Unit: "",
	},
	"mongodb-queries.per.sec": {
		Name: "mongodb-ops.queries",
		Unit: "operations/s",
	},
	"mongodb-inserts.per.sec": {
		Name: "mongodb-ops.inserts",
		Unit: "operations/s",
	},
	"mongodb-updates.per.sec": {
		Name: "mongodb-ops.updates",
		Unit: "operations/s",
	},
	"mongodb-deletes.per.sec": {
		Name: "mongodb-ops.deletes",
		Unit: "operations/s",
	},
	"mongodb-commands.per.sec": {
		Name: "mongodb-ops.commands",
		Unit: "operations/s",
	},
	"mongodb-getmores.per.sec": {
		Name: "mongodb-ops.getmores",
		Unit: "operations/s",
	},
	"mongodb-flushes.per.sec": {
		Name: "mongodb-ops.flushes",
		Unit: "operations/s",
	},
	"mongodb-resident.megabytes": {
		Name:       "mongodb-memory-resident",
		Unit:       "B",
		Conversion: divide_by(1000.0 * 1000.0),
	},
	"mongodb-vsize.megabytes": {
		Name:       "mongodb-memory-vsize",
		Unit:       "B",
		Conversion: divide_by(1000.0 * 1000.0),
	},
	"mongodb-percent.cache.dirty ": {
		Name: "mongodb-cache-dirty",
		Unit: "percent",
	},
	"mongodb-percent.cache.used": {
		Name: "mongodb-cache-used",
		Unit: "percent",
	},
	"postgresql-numbackends": {
		Name: "postgres-num-backends",
		Unit: "count",
	},
	"postgresql-xact.commit": {
		Name: "postgres-xact-commit.cntr",
		Unit: "count/s",
	},
	"postgresql-xact.rollback": {
		Name: "postgres-xact-rollback.cntr",
		Unit: "count/s",
	},
	"postgresql-blks.read": {
		Name: "postgres-blocks-read.cntr",
		Unit: "count/s",
	},
	"postgresql-blks.hit": {
		Name: "postgres-blocks-hit.cntr",
		Unit: "count/s",
	},
	"postgresql-tup.returned": {
		Name: "postgres-tuples-returned.cntr",
		Unit: "count/s",
	},
	"postgresql-tup.fetched": {
		Name: "postgres-tuples-fetched.cntr",
		Unit: "count/s",
	},
	"postgresql-tup.inserted": {
		Name: "postgres-tuples-inserted.cntr",
		Unit: "count/s",
	},
	"postgresql-tup.updated": {
		Name: "postgres-tuples-updated.cntr",
		Unit: "count/s",
	},
	"postgresql-tup.deleted": {
		Name: "postgres-tuples-deleted.cntr",
		Unit: "count/s",
	},
	"postgresql-conflicts": {
		Name: "postgres-conflicts.cntr",
		Unit: "count/s",
	},
	"postgresql-temp.files": {
		Name: "postgres-temp-files.cntr",
		Unit: "files/s",
	},
	"postgresql-temp.bytes": {
		Name: "postgres-temp-bytes.cntr",
		Unit: "B/s",
	},
	"postgresql-deadlocks": {
		Name: "postgres-deadlocks.cntr",
		Unit: "count/s",
	},
	"postgresql-blk.read.time": {
		Name: "postgres-block-read-time.cntr",
		Unit: "percent",
		// total milliseconds in, so divide by 10 to get
		// 100 x seconds, then differentate (.cntr) to get percentage
		Conversion: divide_by(10.0),
	},
	"postgresql-blk.write.time": {
		Name: "postgres-blk-write-time.cntr",
		Unit: "percent",
		// total milliseconds in, so divide by 10 to get
		// 100 x seconds, then differentate (.cntr) to get percentage
		Conversion: divide_by(10.0),
	},
	"Logins/sec | General Statistics-value": {
		Name: "mssql-logins",
		Unit: "count/s",
	},
	"Logouts/sec | General Statistics-value": {
		Name: "mssql-logouts",
		Unit: "count/s",
	},
	"Processes blocked | General Statistics-value": {
		Name: "mssql-blocked-processes",
		Unit: "count",
	},
	"User Connections | General Statistics-value": {
		Name: "mssql-user-connections",
		Unit: "count",
	},
	"Batch Requests/sec | SQL Statistics-value": {
		Name: "mssql-batch-requests",
		Unit: "requests/s",
	},
	"Lock Waits/sec | _Total | Locks-value": {
		Name: "mssql-lock-waits",
		Unit: "count/s",
	},
	"Latch Waits/sec | Latches-value": {
		Name: "mssql-latch-waits",
		Unit: "count/s",
	},
	"Lock Timeouts (timeout > 0)/sec | _Total | Locks-value": {
		Name: "mssql-lock-timeouts",
		Unit: "count/s",
	},
	"Number of Deadlocks/sec | _Total | Locks-value": {
		Name: "mssql-deadlocks",
		Unit: "count/s",
	},
	"Database Cache Memory (KB) | Memory Manager-value": {
		Name:       "mssql-memory-db-cache",
		Unit:       "B",
		Conversion: divide_by(1024.0),
	},
	"Log Pool Memory (KB) | Memory Manager-value": {
		Name:       "mssql-memory-log-pool",
		Unit:       "B",
		Conversion: divide_by(1024.0),
	},
	"Optimizer Memory (KB) | Memory Manager-value": {
		Name:       "mssql-memory-optimizer",
		Unit:       "B",
		Conversion: divide_by(1024.0),
	},
	"SQL Cache Memory (KB) | Memory Manager-value": {
		Name:       "mssql-memory-sql-cache",
		Unit:       "B",
		Conversion: divide_by(1024.0),
	},
	"Transactions/sec | _Total | Databases-value": {
		Name: "mssql-transactions",
		Unit: "count/s",
	},
	"Write Transactions/sec | _Total | Databases-value": {
		Name: "mssql-write-transactions",
		Unit: "count/s",
	},
	"SQL Compilations/sec | SQL Statistics-value": {
		Name: "mssql-sql-compilations",
		Unit: "count/s",
	},
	"SQL Re-Compilations/sec | SQL Statistics-value": {
		Name: "mssql-sql-recompilations",
		Unit: "count/s",
	},
	"Log Flush Wait Time | _Total | Databases-value": {
		Name: "mssql-log-flush-wait-time",
		Unit: "s",
	},
	"Log Flushes/sec | _Total | Databases-value": {
		Name: "mssql-log-flushes",
		Unit: "count/s",
	},
	"nginx-waiting": {
		Name: "nginx-waiting",
		Unit: "connections",
	},
	"nginx-writing": {
		Name: "nginx-writing",
		Unit: "requests",
	},
	"nginx-reading": {
		Name: "nginx-reading",
		Unit: "requests",
	},
	"nginx-handled": {
		Name: "nginx-handled.cntr",
		Unit: "connections",
	},
	"nginx-active": {
		Name: "nginx-active",
		Unit: "connections",
	},
	"nginx-accepts": {
		Name: "nginx-accepts.cntr",
		Unit: "connections",
	},
	"nginx-requests": {
		Name: "nginx-requests.cntr",
		Unit: "requests",
	},
	"uwsgi_summary-memory-vsize": {
		Name: "uwsgi-memory-vsize",
		Unit: "B",
	},
	"uwsgi_summary-memory-resident": {
		Name: "uwsgi-memory-resident",
		Unit: "B",
	},
	"uwsgi_summary-request-time": {
		Name: "uwsgi-request-time",
		Unit: "ms",
	},
	"uwsgi_summary-requests": {
		Name: "uwsgi-requests.cntr",
		Unit: "requests",
	},
	"uwsgi_summary-workers": {
		Name: "uwsgi-workers",
		Unit: "",
	},
	"uwsgi_summary-active-workers": {
		Name: "uwsgi-active-workers",
		Unit: "",
	},
	"uwsgi_summary-exceptions": {
		Name: "uwsgi-exceptions.cntr",
		Unit: "exceptions",
	},
	"vault_audit_log_request-mean": {
		Name: "vault-audit-log-requests.cntr",
		Unit: "count",
	},
	"vault_audit_log_response-mean": {
		Name: "vault-audit-log-responses.cntr",
		Unit: "count",
	},
	"vault_barrier_delete-mean": {
		Name: "vault-barrier-deletes.cntr",
		Unit: "count",
	},
	"vault_barrier_get-mean": {
		Name: "vault-barrier-get-ops.cntr",
		Unit: "count",
	},
	"vault_barrier_put-mean": {
		Name: "vault-barrier-put-ops.cntr",
		Unit: "count",
	},
	"vault_barrier_list-value": {
		Name: "vault-barrier-list-ops.cntr",
		Unit: "count",
	},
	"vault_core_check_token-mean": {
		Name: "vault-token-checks.cntr",
		Unit: "count",
	},
	"vault_core_fetch_acl_and_token-mean": {
		Name: "vault-acl-and-token-fetches.cntr",
		Unit: "count",
	},
	"vault_core_handle_request-mean": {
		Name: "vault-requests.cntr",
		Unit: "count",
	},
	"vault_core_handle_login_request-mean": {
		Name: "vault-login-requests.cntr",
		Unit: "count",
	},
	"vault_core_leadership_setup_failed-mean": {
		Name: "vault-leadership-setup-failures.cntr",
		Unit: "count",
	},
	"vault_core_leadership_lost-mean": {
		Name: "vault-leadership-losses.cntr",
		Unit: "count",
	},
	"vault_core_post_unseal-value": {
		Name: "vault-post-unseal-ops",
		Unit: "count",
	},
	"vault_core_pre_seal-value": {
		Name: "vault-pre-seal-ops",
		Unit: "count",
	},
	"vault_core_seal-with-request-value": {
		Name: "vault-requested-seals",
		Unit: "count",
	},
	"vault_core_seal-value": {
		Name: "vault-seals",
		Unit: "count",
	},
	"vault_core_seal-internal-value": {
		Name: "vault-internal-seals",
		Unit: "count",
	},
	"vault_core_step_down-mean": {
		Name: "vault-step-downs.cntr",
		Unit: "count",
	},
	"vault_core_unseal-mean": {
		Name: "vault-unseals.cntr",
		Unit: "count",
	},
	"vault_runtime_alloc_bytes-value": {
		Name: "vault-allocated-bytes",
		Unit: "B",
	},
	"vault_runtime_free_count-value": {
		Name: "vault-free-ops",
		Unit: "count",
	},
	"vault_runtime_heap_objects-value": {
		Name: "vault-heap-objects",
		Unit: "count",
	},
	"vault_runtime_malloc_count-value": {
		Name: "vault-malloc-ops",
		Unit: "count",
	},
	"vault_runtime_num_goroutines-value": {
		Name: "vault-goroutines",
		Unit: "count",
	},
	"vault_runtime_sys_bytes-value": {
		Name: "vault-sys-bytes",
		Unit: "B",
	},
	"vault_runtime_gc_pause_ns-mean": {
		Name:       "vault-gc-pause-time-avg",
		Unit:       "s",
		Conversion: divide_by(1000 * 1000 * 1000),
	},
	"vault_runtime_total_gc_pause_ns-value": {
		Name:       "vault-gc-pause-time.cntr",
		Unit:       "s",
		Conversion: divide_by(1000 * 1000 * 1000),
	},
	"vault_runtime_total_gc_runs-value": {
		Name: "vault-gc-runs.cntr",
		Unit: "count",
	},
	"vault_expire_num_leases-value": {
		Name: "vault-expired-leases",
		Unit: "count",
	},
	"vault_expire_revoke": {
		Name: "vault-revoke-ops.cntr",
		Unit: "count",
	},
	"vault_expire_revoke-force": {
		Name: "vault-revokes-force.cntr",
		Unit: "count",
	},
	"vault_expire_revoke-prefix": {
		Name: "vault-revokes-by-prefix.cntr",
		Unit: "count",
	},
	"vault_expire_revoke-by-token": {
		Name: "vault-revokes-by-token.cntr",
		Unit: "count",
	},
	"vault_expire_renew": {
		Name: "vault-renew-ops.cntr",
		Unit: "count",
	},
	"vault_expire_renew-token": {
		Name: "vault-renew-token-ops",
		Unit: "count",
	},
	"vault_policy_get_policy": {
		Name: "vault-policy-get-ops.cntr",
		Unit: "count",
	},
	"vault_policy_list_policies": {
		Name: "vault-policy-list-ops.cntr",
		Unit: "count",
	},
	"vault_policy_delete_policy": {
		Name: "vault-policy-delete-ops.cntr",
		Unit: "count",
	},
	"vault_policy_set_policy": {
		Name: "vault-policy-set-ops.cntr",
		Unit: "count",
	},
	"vault_token_create": {
		Name: "vault-token-create-ops.cntr",
		Unit: "count",
	},
	"vault_token_createAccessor": {
		Name: "vault-token-identifier-ops.cntr",
		Unit: "count",
	},
	"vault_token_lookup": {
		Name: "vault-token-lookups.cntr",
		Unit: "count",
	},
	"vault_token_revoke": {
		Name: "vault-token-revokes.cntr",
		Unit: "count",
	},
	"vault_token_revoke-tree": {
		Name: "vault-token-tree-revokes.cntr",
		Unit: "count",
	},
	"vault_token_store": {
		Name: "vault-token-store-ops.cntr",
		Unit: "count",
	},
	"vault_rollback_attempt_auth-token--mean": {
		Name: "vault-rollback-attempts-auth-token",
		Unit: "count",
	},
	"vault_rollback_attempt_cubbyhole--mean": {
		Name: "vault-rollback-attempts-cubbyhole",
		Unit: "count",
	},
	"vault_rollback_attempt_secret--mean": {
		Name: "vault-rollback-attempts-secret",
		Unit: "count",
	},
	"vault_rollback_attempt_sys--mean": {
		Name: "vault-rollback-attempts-sys",
		Unit: "count",
	},
	"vault_rollback_attempt_pki--mean": {
		Name: "vault-rollback-attempts-pki",
		Unit: "count",
	},
	"vault_route_rollback_auth-token--mean": {
		Name: "vault-route-rollbacks-auth-token",
		Unit: "count",
	},
	"vault_route_rollback_cubbyhole--mean": {
		Name: "vault-route-rollbacks-cubbyhole",
		Unit: "count",
	},
	"vault_route_rollback_secret--mean": {
		Name: "vault-route-rollbacks-secret",
		Unit: "count",
	},
	"vault_route_rollback_sys--mean": {
		Name: "vault-route-rollbacks-sys",
		Unit: "count",
	},
	"vault_route_rollback_pki--mean": {
		Name: "vault-route-rollbacks-pki",
		Unit: "count",
	},
	"vault_etcd_put-value": {
		Name: "vault-etcd-put-ops",
		Unit: "count",
	},
	"vault_etcd_get-value": {
		Name: "vault-etcd-get-ops",
		Unit: "count",
	},
	"vault_etcd_delete-value": {
		Name: "vault-etcd-delete-ops",
		Unit: "count",
	},
	"vault_etcd_list-value": {
		Name: "vault-etcd-list-ops",
		Unit: "count",
	},
}

type Translation struct {
	Name       string
	Unit       string
	Conversion func(interface{}) interface{}
}

func subtract_from_100_percent(value interface{}) interface{} {
	return (100.0 - value.(float64))
}

func divide_by(divisor float64) func(value interface{}) interface{} {
	return func(value interface{}) interface{} {
		switch v := value.(type) {
		case int64:
			return float64(v) / divisor
		case float64:
			return v / divisor
		default:
			return 0.0
		}
	}
}

func es_cluster_health(status interface{}) interface{} {
	switch status.(string) {
	case "green":
		return 0.0
	case "yellow":
		return 1.0
	case "red":
		return 2.0
	default:
		return 3.0
	}
}

type CmpData struct {
	MonitoringSystem string      `json:"monitoring_system"`
	ResourceId       string      `json:"resource_id"`
	Metrics          []CmpMetric `json:"metrics"`
}

type CmpMetric struct {
	Metric string `json:"metric"`
	Unit   string `json:"unit"`
	Value  string `json:"value"`
	Time   string `json:"time"`
}

func (data *CmpData) AddMetric(item CmpMetric) []CmpMetric {
	data.Metrics = append(data.Metrics, item)
	return data.Metrics
}

func (a *Cmp) Connect() error {
	if a.ApiUser == "" || a.ApiKey == "" || a.CmpInstance == "" || a.ResourceId == "" {
		return fmt.Errorf("api_user, api_key, resource_id and cmp_instance are required fields for cmp output")
	}
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	a.client = &http.Client{
		Transport: tr,
		Timeout:   a.Timeout.Duration,
	}
	return nil
}

func (a *Cmp) Write(metrics []telegraf.Metric) error {
	if len(metrics) == 0 {
		return nil
	}
	cmp_data := &CmpData{
		MonitoringSystem: "telegraf",
		ResourceId:       a.ResourceId,
	}

	for _, m := range metrics {

		if a.Debug {
			log.Printf("METRIC: %+v", m)
		}

		suffix := ""
		cpu := m.Tags()["cpu"]
		path := m.Tags()["path"]
		haproxy_service := m.Tags()["proxy"] + "_" + m.Tags()["sv"]
		container_name := m.Tags()["com.docker.compose.service"]
		disk_name := m.Tags()["name"]
		db := m.Tags()["db"]

		if len(cpu) > 0 && cpu != "cpu-total" {
			suffix = cpu[3:]
		} else if len(path) > 0 {
			suffix = path
		} else if len(container_name) > 0 {
			suffix = container_name
		} else if m.Name() == "haproxy" && len(haproxy_service) > 0 {
			suffix = haproxy_service
		} else if m.Name() == "diskio" && len(disk_name) > 0 {
			suffix = disk_name
		} else if m.Name() == "postgresql" && len(db) > 0 {
			suffix = db
		}

		timestamp := m.Time().UTC().Format("2006-01-02T15:04:05.999999Z")
		for k, v := range m.Fields() {
			metric_name := m.Name() + "-" + strings.Replace(k, "_", ".", -1)
			translation, found := translateMap[metric_name]
			if found {
				cmp_name := translation.Name
				if len(suffix) > 0 {
					if strings.HasSuffix(cmp_name, ".cntr") {
						cmp_name = strings.TrimSuffix(cmp_name, ".cntr")
						cmp_name += "." + suffix + ".cntr"
					} else {
						cmp_name += "." + suffix
					}

				}

				conversion := translation.Conversion
				if conversion != nil {
					v = conversion(v)
				}

				if a.Debug {
					log.Printf("SEND: %s: %s %v", timestamp, cmp_name, v)
				}
				cmp_data.AddMetric(CmpMetric{
					Metric: cmp_name,
					Unit:   translation.Unit,
					Value:  fmt.Sprintf("%v", v),
					Time:   timestamp,
				})
			} else if a.Debug {
				log.Printf("Not Matched: %s %v", metric_name, v)
			}
		}
	}

	cmp_bytes, err := json.Marshal(cmp_data)
	if err != nil {
		return fmt.Errorf("unable to marshal TimeSeries, %s\n", err.Error())
	}
	req, err := http.NewRequest("POST", a.authenticatedUrl(), bytes.NewBuffer(cmp_bytes))
	if err != nil {
		return fmt.Errorf("unable to create http.Request, %s\n", err.Error())
	}
	req.Header.Add("Content-Type", "application/json")
	req.SetBasicAuth(a.ApiUser, a.ApiKey)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("error POSTing metrics, %s\n", err.Error())
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 209 {
		return fmt.Errorf("received bad status code, %d\n", resp.StatusCode)
	}

	return nil
}

func (a *Cmp) SampleConfig() string {
	return sampleConfig
}

func (a *Cmp) Description() string {
	return "Configuration for Cmp Server to send metrics to."
}

func (a *Cmp) authenticatedUrl() string {

	return fmt.Sprintf("%s/metrics", a.CmpInstance)
}

func (a *Cmp) Close() error {
	return nil
}

func init() {
	outputs.Add("cmp", func() telegraf.Output {
		return &Cmp{}
	})
}

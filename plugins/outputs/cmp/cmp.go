package cmp

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/outputs"
)

// CMP represents our plugin config
type CMP struct {
	APIURL     string            `toml:"api_url"`
	APIUser    string            `toml:"api_user"`
	APIKey     string            `toml:"api_key"`
	ResourceID string            `toml:"resource_id"`
	Timeout    internal.Duration `toml:"timeout"`
	UserAgent  string            `toml:"user_agent"`

	client *http.Client
}

var sampleConfig = `
  ## CMP API URL and credentials are required
  api_url = "https://dev.cmp.nflex.io/cmp/basic/api"
  api_user = "api-user"
  api_key = "api-key"

  ## CMP Resource UUID is also required
  resource_id = "00000000-0000-0000-0000-000000000001"

  ## Request settings
  timeout = "5s"
  user_agent = ""
`

var translateMap = map[string]Translation{
	"cpu-usage.idle": {
		Name:       "cpu-usage",
		Unit:       "percent",
		Conversion: subtractFrom100Percent,
	},
	"cpu-usage.user": {
		Name: "cpu-usage-user",
		Unit: "percent",
	},
	"cpu-usage.system": {
		Name: "cpu-usage-system",
		Unit: "percent",
	},
	"cpu-usage.iowait": {
		Name: "cpu-usage-iowait",
		Unit: "percent",
	},
	"mem-available.percent": {
		Name:       "memory-usage",
		Unit:       "percent",
		Conversion: subtractFrom100Percent,
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
		Name:    "disk-io-time-percent",
		Unit:    "percent",
		Counter: true,
		// ms / 1000 for s then * 100 for percent
		Conversion: divideBy(10.0),
	},
	"diskio-reads": {
		Name:    "disk-read-ops",
		Counter: true,
		Unit:    "count",
	},
	"diskio-writes": {
		Name:    "disk-write-ops",
		Counter: true,
		Unit:    "count",
	},
	"diskio-read.time": {
		Name:    "disk-read-time-percent",
		Counter: true,
		Unit:    "percent",
		// ms / 1000 for s then * 100 for percent
		Conversion: divideBy(10.0),
	},
	"diskio-write.time": {
		Name:    "disk-write-time-percent",
		Counter: true,
		Unit:    "percent",
		// ms / 1000 for s then * 100 for percent
		Conversion: divideBy(10.0),
	},
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
		Conversion: esClusterHealth,
	},
	"elasticsearch_cluster_health-number.of.nodes": {
		Name: "es-nodes",
		Unit: "",
	},
	"elasticsearch_cluster_health-active.shards": {
		Name:           "es-shards",
		Specialisation: "active",
		Unit:           "",
	},
	"elasticsearch_cluster_health-active.primary.shards": {
		Name:           "es-shards",
		Specialisation: "primary",
		Unit:           "",
	},
	"elasticsearch_cluster_health-unassigned.shards": {
		Name:           "es-shards",
		Specialisation: "unassigned",
		Unit:           "",
	},
	"elasticsearch_cluster_health-initializing.shards": {
		Name:           "es-shards",
		Specialisation: "initializing",
		Unit:           "",
	},
	"elasticsearch_cluster_health-relocating.shards": {
		Name:           "es-shards",
		Specialisation: "relocating",
		Unit:           "",
	},
	"elasticsearch_jvm-mem.heap.used.in.bytes": {
		Name:           "es-memory-usage",
		Specialisation: "heap.used",
		Unit:           "B",
	},
	"elasticsearch_jvm-mem.heap.committed.in.bytes": {
		Name:           "es-memory-usage",
		Specialisation: "heap.committed",
		Unit:           "B",
	},
	"elasticsearch_jvm-mem.non.heap.used.in.bytes": {
		Name:           "es-memory-usage",
		Specialisation: "nonheap.used",
		Unit:           "B",
	},
	"elasticsearch_jvm-mem.non.heap.committed.in.bytes": {
		Name:           "es-memory-usage",
		Specialisation: "nonheap.committed",
		Unit:           "B",
	},
	"elasticsearch_indices-search.query.total": {
		Name:           "es-search-requests",
		Specialisation: "query",
		Counter:        true,
		Unit:           "requests",
	},
	"elasticsearch_indices-search.fetch.total": {
		Name:           "es-search-requests",
		Specialisation: "fetch",
		Counter:        true,
		Unit:           "requests",
	},
	"elasticsearch_indices-search.query.current": {
		Name:           "es-current-search-requests",
		Specialisation: "query",
		Unit:           "requests",
	},
	"elasticsearch_indices-search.fetch.current": {
		Name:           "es-current-search-requests",
		Specialisation: "fetch",
		Unit:           "requests",
	},
	"elasticsearch_indices-search.query.time.in.millis": {
		Name:           "es-search-time",
		Specialisation: "query",
		Counter:        true,
		Unit:           "s",
		Conversion:     divideBy(1000.0),
	},
	"elasticsearch_indices-search.fetch.time.in.millis": {
		Name:           "es-search-time",
		Specialisation: "fetch",
		Counter:        true,
		Unit:           "s",
		Conversion:     divideBy(1000.0),
	},
	"elasticsearch_indices-get.total": {
		Name:           "es-get-requests",
		Specialisation: "get",
		Counter:        true,
		Unit:           "requests",
	},
	"elasticsearch_indices-get.exists.total": {
		Name:           "es-get-requests",
		Specialisation: "exists",
		Counter:        true,
		Unit:           "requests",
	},
	"elasticsearch_indices-get.missing.total": {
		Name:           "es-get-requests",
		Specialisation: "missing",
		Counter:        true,
		Unit:           "requests",
	},
	"elasticsearch_indices-get.time.in.millis": {
		Name:           "es-get-time",
		Specialisation: "get",
		Unit:           "s",
		Conversion:     divideBy(1000.0),
	},
	"elasticsearch_indices-get.exists.time.in.millis": {
		Name:           "es-get-time",
		Specialisation: "exists",
		Unit:           "s",
		Conversion:     divideBy(1000.0),
	},
	"elasticsearch_indices-get.missing.time.in.millis": {
		Name:           "es-get-time",
		Specialisation: "missing",
		Unit:           "s",
		Conversion:     divideBy(1000.0),
	},
	"elasticsearch_indices-indexing.index.total": {
		Name:           "es-index-requests",
		Specialisation: "index",
		Counter:        true,
		Unit:           "requests",
	},
	"elasticsearch_indices-indexing.index.current": {
		Name: "es-current-index-requests",
		Unit: "requests",
	},
	"elasticsearch_indices-indexing.delete.total": {
		Name:           "es-index-requests",
		Specialisation: "delete",
		Counter:        true,
		Unit:           "requests",
	},
	"elasticsearch_indices-indexing.index.time.in.millis": {
		Name:           "es-index-time",
		Specialisation: "index",
		Counter:        true,
		Unit:           "s",
		Conversion:     divideBy(1000.0),
	},
	"elasticsearch_indices-indexing.delete.time.in.millis": {
		Name:           "es-index-time",
		Specialisation: "delete",
		Unit:           "s",
		Conversion:     divideBy(1000.0),
	},
	"elasticsearch_indices-flush.total.time.in.millis": {
		Name:           "es-index-time",
		Specialisation: "flush",
		Counter:        true,
		Unit:           "requests",
		Conversion:     divideBy(1000.0),
	},
	"etcd_server_has_leader-gauge": {
		Name: "etcd-has-leader",
		Unit: "count",
	},
	"etcd_server_leader_changes_seen_total-counter": {
		Name:    "etcd-leader-changes-seen",
		Counter: true,
		Unit:    "count",
	},
	"etcd_server_proposals_committed_total-gauge": {
		Name: "etcd-proposals-committed-total",
		Unit: "count",
	},
	"etcd_server_proposals_applied_total-gauge": {
		Name: "etcd-proposals-applied-total",
		Unit: "count",
	},
	"etcd_server_proposals_pending-gauge": {
		Name: "etcd-proposals-pending",
		Unit: "count",
	},
	"etcd_server_proposals_failed_total-counter": {
		Name:    "etcd-proposals-failed",
		Counter: true,
		Unit:    "count",
	},
	"etcd_network_peer_sent_bytes_total-counter": {
		Name:    "etcd-peer-sent-bytes",
		Counter: true,
		Unit:    "B",
	},
	"etcd_network_peer_received_bytes_total-counter": {
		Name:    "etcd-peer-received-bytes",
		Counter: true,
		Unit:    "B",
	},
	"etcd_network_peer_sent_failures_total-counter": {
		Name:    "etcd-peer-sent-failures",
		Counter: true,
		Unit:    "count",
	},
	"etcd_network_peer_received_failures_total-counter": {
		Name:    "etcd-peer-received-failures",
		Counter: true,
		Unit:    "count",
	},
	"etcd_network_client_grpc_sent_bytes_total-counter": {
		Name:    "etcd-grpc-client-sent-bytes",
		Counter: true,
		Unit:    "B",
	},
	"etcd_network_client_grpc_received_bytes_total-counter": {
		Name:    "etcd-grpc-client-received-bytes",
		Counter: true,
		Unit:    "B",
	},
	"process_open_fds-gauge": {
		Name: "etcd-open-file-descriptors",
		Unit: "count",
	},
	"process_max_fds-gauge": {
		Name: "etcd-max-file-descriptors",
		Unit: "count",
	},
	"grpc_server_started_total-counter": {
		Name:    "etcd-server-started",
		Counter: true,
		Unit:    "count",
	},
	"etcd_debugging_mvcc_db_total_size_in_bytes-gauge": {
		Name: "etcd-mvcc-db-size",
		Unit: "B",
	},
	"etcd_debugging_mvcc_delete_total-counter": {
		Name:    "etcd-mvcc-deletes",
		Counter: true,
		Unit:    "count",
	},
	"etcd_debugging_mvcc_keys_total-gauge": {
		Name: "etcd-mvcc-keys",
		Unit: "count",
	},
	"etcd_debugging_server_lease_expired_total-counter": {
		Name:    "etcd-server-lease-expired",
		Counter: true,
		Unit:    "count",
	},
	"process_resident_memory_bytes-gauge": {
		Name: "etcd-resident-memory",
		Unit: "B",
	},
	"haproxy-active.servers": {
		Name: "haproxy-active-servers",
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
		Conversion: divideBy(1000.0),
	},
	"haproxy-rate": {
		Name: "haproxy-rate",
		Unit: "sessions/s",
	},
	"haproxy-req.tot": {
		Name: "haproxy-requests-total",
		Unit: "requests",
	},
	"haproxy-scur": {
		Name: "haproxy-sessions-current",
		Unit: "sessions",
	},
	"haproxy-smax": {
		Name: "haproxy-sessions-max",
		Unit: "sessions",
	},
	"haproxy-stot": {
		Name: "haproxy-sessions-total",
		Unit: "sessions",
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
		Name:           "mongodb-ops",
		Specialisation: "queries",
		Unit:           "operations/s",
	},
	"mongodb-inserts.per.sec": {
		Name:           "mongodb-ops",
		Specialisation: "inserts",
		Unit:           "operations/s",
	},
	"mongodb-updates.per.sec": {
		Name:           "mongodb-ops",
		Specialisation: "updated",
		Unit:           "operations/s",
	},
	"mongodb-deletes.per.sec": {
		Name:           "mongodb-ops",
		Specialisation: "deletes",
		Unit:           "operations/s",
	},
	"mongodb-commands.per.sec": {
		Name:           "mongodb-ops",
		Specialisation: "commands",
		Unit:           "operations/s",
	},
	"mongodb-getmores.per.sec": {
		Name:           "mongodb-ops",
		Specialisation: "getmores",
		Unit:           "operations/s",
	},
	"mongodb-flushes.per.sec": {
		Name:           "mongodb-ops",
		Specialisation: "flushes",
		Unit:           "operations/s",
	},
	"mongodb-resident.megabytes": {
		Name:       "mongodb-memory-resident",
		Unit:       "B",
		Conversion: divideBy(1000.0 * 1000.0),
	},
	"mongodb-vsize.megabytes": {
		Name:       "mongodb-memory-vsize",
		Unit:       "B",
		Conversion: divideBy(1000.0 * 1000.0),
	},
	"mongodb-percent.cache.dirty ": {
		Name: "mongodb-cache-dirty",
		Unit: "percent",
	},
	"mongodb-percent.cache.used": {
		Name: "mongodb-cache-used",
		Unit: "percent",
	},
	"mongodb_db_stats-index.size": {
		Name: "mongodb-db-index-size",
		Unit: "B",
	},
	"mongodb_db_stats-data.size": {
		Name: "mongodb-db-data-size",
		Unit: "B",
	},
	"mongodb_db_stats-objects": {
		Name: "mongodb-db-objects",
		Unit: "count",
	},
	"mongodb_db_stats-ok": {
		Name: "mongodb-db-ok",
		Unit: "count",
	},
	"mongodb_db_stats-storage.size": {
		Name: "mongodb-db-storage-size",
		Unit: "B",
	},
	"mongodb_db_stats-avg.obj.size": {
		Name: "mongodb-db-obj-size-avg",
		Unit: "B",
	},
	"mongodb_db_stats-indexes": {
		Name: "mongodb-db-indexes",
		Unit: "count",
	},
	"mongodb_db_stats-collections": {
		Name: "mongodb-db-collections",
		Unit: "count",
	},
	"mongodb_db_stats-num.extents": {
		Name: "mongodb-db-num-extents",
		Unit: "count",
	},
	"postgresql-numbackends": {
		Name: "postgres-num-backends",
		Unit: "count",
	},
	"postgresql-xact.commit": {
		Name:    "postgres-xact-commit",
		Counter: true,
		Unit:    "count/s",
	},
	"postgresql-xact.rollback": {
		Name:    "postgres-xact-rollback",
		Counter: true,
		Unit:    "count/s",
	},
	"postgresql-blks.read": {
		Name:    "postgres-blocks-read",
		Counter: true,
		Unit:    "count/s",
	},
	"postgresql-blks.hit": {
		Name:    "postgres-blocks-hit",
		Counter: true,
		Unit:    "count/s",
	},
	"postgresql-tup.returned": {
		Name:    "postgres-tuples-returned",
		Counter: true,
		Unit:    "count/s",
	},
	"postgresql-tup.fetched": {
		Name:    "postgres-tuples-fetched",
		Counter: true,
		Unit:    "count/s",
	},
	"postgresql-tup.inserted": {
		Name:    "postgres-tuples-inserted",
		Counter: true,
		Unit:    "count/s",
	},
	"postgresql-tup.updated": {
		Name:    "postgres-tuples-updated",
		Counter: true,
		Unit:    "count/s",
	},
	"postgresql-tup.deleted": {
		Name:    "postgres-tuples-deleted",
		Counter: true,
		Unit:    "count/s",
	},
	"postgresql-conflicts": {
		Name:    "postgres-conflicts",
		Counter: true,
		Unit:    "count/s",
	},
	"postgresql-temp.files": {
		Name:    "postgres-temp-files",
		Counter: true,
		Unit:    "files/s",
	},
	"postgresql-temp.bytes": {
		Name:    "postgres-temp-bytes",
		Counter: true,
		Unit:    "B/s",
	},
	"postgresql-deadlocks": {
		Name:    "postgres-deadlocks",
		Counter: true,
		Unit:    "count/s",
	},
	"postgresql-blk.read.time": {
		Name:    "postgres-block-read-time",
		Counter: true,
		Unit:    "percent",
		// total milliseconds in, so divide by 10 to get
		// 100 x seconds, then differentate (.cntr) to get percentage
		Conversion: divideBy(10.0),
	},
	"postgresql-blk.write.time": {
		Name:    "postgres-blk-write-time",
		Counter: true,
		Unit:    "percent",
		// total milliseconds in, so divide by 10 to get
		// 100 x seconds, then differentate (.cntr) to get percentage
		Conversion: divideBy(10.0),
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
		Conversion: divideBy(1024.0),
	},
	"Log Pool Memory (KB) | Memory Manager-value": {
		Name:       "mssql-memory-log-pool",
		Unit:       "B",
		Conversion: divideBy(1024.0),
	},
	"Optimizer Memory (KB) | Memory Manager-value": {
		Name:       "mssql-memory-optimizer",
		Unit:       "B",
		Conversion: divideBy(1024.0),
	},
	"SQL Cache Memory (KB) | Memory Manager-value": {
		Name:       "mssql-memory-sql-cache",
		Unit:       "B",
		Conversion: divideBy(1024.0),
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
		Name:    "nginx-handled",
		Counter: true,
		Unit:    "connections",
	},
	"nginx-active": {
		Name: "nginx-active",
		Unit: "connections",
	},
	"nginx-accepts": {
		Name:    "nginx-accepts",
		Counter: true,
		Unit:    "connections",
	},
	"nginx-requests": {
		Name:    "nginx-requests",
		Counter: true,
		Unit:    "requests",
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
		Name:    "uwsgi-requests",
		Counter: true,
		Unit:    "requests",
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
		Name:    "uwsgi-exceptions",
		Counter: true,
		Unit:    "exceptions",
	},
	"vault_audit_log_request-mean": {
		Name: "vault-audit-log-requests",
		Unit: "count",
	},
	"vault_audit_log_response-mean": {
		Name: "vault-audit-log-responses",
		Unit: "count",
	},
	"vault_barrier_delete-mean": {
		Name: "vault-barrier-deletes",
		Unit: "count",
	},
	"vault_barrier_get-mean": {
		Name: "vault-barrier-get-ops",
		Unit: "count",
	},
	"vault_barrier_put-mean": {
		Name: "vault-barrier-put-ops",
		Unit: "count",
	},
	"vault_barrier_list-value": {
		Name: "vault-barrier-list-ops",
		Unit: "count",
	},
	"vault_core_check_token-mean": {
		Name:    "vault-token-checks",
		Counter: true,
		Unit:    "count",
	},
	"vault_core_fetch_acl_and_token-mean": {
		Name: "vault-acl-and-token-fetches",
		Unit: "count",
	},
	"vault_core_handle_request-mean": {
		Name: "vault-requests",
		Unit: "count",
	},
	"vault_core_handle_login_request-mean": {
		Name:    "vault-login-requests",
		Counter: true,
		Unit:    "count",
	},
	"vault_core_leadership_setup_failed-mean": {
		Name:    "vault-leadership-setup-failures",
		Counter: true,
		Unit:    "count",
	},
	"vault_core_leadership_lost-mean": {
		Name:    "vault-leadership-losses",
		Counter: true,
		Unit:    "count",
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
		Name:    "vault-step-downs",
		Counter: true,
		Unit:    "count",
	},
	"vault_core_unseal-mean": {
		Name:    "vault-unseals",
		Counter: true,
		Unit:    "count",
	},
	"vault_runtime_alloc_bytes-value": {
		Name: "vault-allocated-bytes",
		Unit: "B",
	},
	"vault_runtime_free_count-value": {
		Name:    "vault-free-ops",
		Counter: true,
		Unit:    "count",
	},
	"vault_runtime_heap_objects-value": {
		Name: "vault-heap-objects",
		Unit: "count",
	},
	"vault_runtime_malloc_count-value": {
		Name:    "vault-malloc-ops",
		Counter: true,
		Unit:    "count",
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
		Conversion: divideBy(1000 * 1000 * 1000),
	},
	"vault_runtime_total_gc_pause_ns-value": {
		Name:       "vault-gc-pause-time",
		Counter:    true,
		Unit:       "s",
		Conversion: divideBy(1000 * 1000 * 1000),
	},
	"vault_runtime_total_gc_runs-value": {
		Name:    "vault-gc-runs",
		Counter: true,
		Unit:    "count",
	},
	"vault_expire_num_leases-value": {
		Name:    "vault-expired-leases",
		Counter: true,
		Unit:    "count",
	},
	"vault_expire_revoke": {
		Name:    "vault-revoke-ops",
		Counter: true,
		Unit:    "count",
	},
	"vault_expire_revoke-force": {
		Name:    "vault-revokes-force",
		Counter: true,
		Unit:    "count",
	},
	"vault_expire_revoke-prefix": {
		Name:    "vault-revokes-by-prefix",
		Counter: true,
		Unit:    "count",
	},
	"vault_expire_revoke-by-token": {
		Name:    "vault-revokes-by-token",
		Counter: true,
		Unit:    "count",
	},
	"vault_expire_renew": {
		Name:    "vault-renew-ops",
		Counter: true,
		Unit:    "count",
	},
	"vault_expire_renew-token": {
		Name: "vault-renew-token-ops",
		Unit: "count",
	},
	"vault_policy_get_policy": {
		Name:    "vault-policy-get-ops",
		Counter: true,
		Unit:    "count",
	},
	"vault_policy_list_policies": {
		Name:    "vault-policy-list-ops",
		Counter: true,
		Unit:    "count",
	},
	"vault_policy_delete_policy": {
		Name:    "vault-policy-delete-ops",
		Counter: true,
		Unit:    "count",
	},
	"vault_policy_set_policy": {
		Name:    "vault-policy-set-ops",
		Counter: true,
		Unit:    "count",
	},
	"vault_token_create": {
		Name:    "vault-token-create-ops",
		Counter: true,
		Unit:    "count",
	},
	"vault_token_createAccessor": {
		Name:    "vault-token-identifier-ops",
		Counter: true,
		Unit:    "count",
	},
	"vault_token_lookup": {
		Name:    "vault-token-lookups",
		Counter: true,
		Unit:    "count",
	},
	"vault_token_revoke": {
		Name:    "vault-token-revokes",
		Counter: true,
		Unit:    "count",
	},
	"vault_token_revoke-tree": {
		Name:    "vault-token-tree-revokes",
		Counter: true,
		Unit:    "count",
	},
	"vault_token_store": {
		Name:    "vault-token-store-ops",
		Counter: true,
		Unit:    "count",
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
	"redis-blocked.clients": {
		Name: "redis-blocked-clients",
		Unit: "count",
	},
	"redis-client.biggest.input.buf": {
		Name: "redis-client-biggest-input-buf",
		Unit: "count",
	},
	"redis-clients": {
		Name: "redis-clients",
		Unit: "count",
	},
	"redis-cluster.enabled": {
		Name: "redis-cluster-enabled",
		Unit: "count",
	},
	"redis-connected.slaves": {
		Name: "redis-connected-slaves",
		Unit: "count",
	},
	"redis-evicted.keys": {
		Name:    "redis-evicted-keys",
		Counter: true,
		Unit:    "count",
	},
	"redis-expired.keys": {
		Name:    "redis-expired-keys",
		Counter: true,
		Unit:    "count",
	},
	"redis-instantaneous.ops.per.sec": {
		Name: "redis-ops-per-sec",
		Unit: "count",
	},
	"redis-keyspace.hitrate": {
		Name:    "redis-keyspace-hitrate",
		Counter: true,
		Unit:    "count",
	},
	"redis-keyspace.hits": {
		Name:    "redis-keyspace-hits",
		Counter: true,
		Unit:    "count",
	},
	"redis-keyspace.misses": {
		Name:    "redis-keyspace-misses",
		Counter: true,
		Unit:    "count",
	},
	"redis-master.repl.offset": {
		Name: "redis-master-repl-offset",
		Unit: "count",
	},
	"redis-pubsub.channels": {
		Name:    "redis-pubsub-channels",
		Counter: true,
		Unit:    "count",
	},
	"redis-pubsub.patterns": {
		Name:    "redis-pubsub-patterns",
		Counter: true,
		Unit:    "count",
	},
	"redis-rejected.connections": {
		Name: "redis-rejected-connections",
		Unit: "count",
	},
	"redis-repl.backlog.active": {
		Name: "redis-repl-backlog-active",
		Unit: "count",
	},
	"redis-repl.backlog.size": {
		Name: "redis-repl-backlog-size",
		Unit: "B",
	},
	"redis-sync.partial.err": {
		Name: "redis-sync-partial-err",
		Unit: "count",
	},
	"redis-sync.partial.ok": {
		Name:    "redis-sync-partial-ok",
		Counter: true,
		Unit:    "count",
	},
	"redis-total.commands.processed": {
		Name:    "redis-total-commands-processed",
		Counter: true,
		Unit:    "count",
	},
	"redis-total.connections.received": {
		Name:    "redis-total-connections-received",
		Counter: true,
		Unit:    "count",
	},
	"redis-total.net.input.bytes": {
		Name:    "redis-total-net-input",
		Counter: true,
		Unit:    "B",
	},
	"redis-total.net.output.bytes": {
		Name:    "redis-total-output",
		Counter: true,
		Unit:    "B",
	},
	"redis-used.cpu.sys": {
		Name:    "redis-used-cpu-sys",
		Counter: true,
		Unit:    "percent",
	},
	"redis-used.cpu.user": {
		Name:    "redis-used-cpu-user",
		Counter: true,
		Unit:    "percent",
	},
	"redis-used.memory": {
		Name: "redis-used-memory",
		Unit: "B",
	},
	"redis-used.memory.lua": {
		Name: "redis-used-memory-lua",
		Unit: "B",
	},
	"redis-used.memory.peak": {
		Name: "redis-peak-used-memory",
		Unit: "B",
	},
	"redis-used.memory.rss": {
		Name: "redis-used-memory-rss",
		Unit: "B",
	},
	"zookeeper-outstanding.requests": {
		Name: "zookeeper-outstanding-requests",
		Unit: "count",
	},
	"zookeeper-open.file.descriptor.count": {
		Name:    "zookeeper-open-file-descriptor",
		Counter: true,
		Unit:    "count",
	},
	"zookeeper-packets.sent": {
		Name:    "zookeeper-packets-sent",
		Counter: true,
		Unit:    "count",
	},
	"zookeeper-max.latency": {
		Name: "zookeeper-max-latency",
		Unit: "count",
	},
	"zookeeper-packets.received": {
		Name:    "zookeeper-packets-received",
		Counter: true,
		Unit:    "count",
	},
	"zookeeper-approximate.data.size": {
		Name: "zookeeper-approximate-data-size",
		Unit: "B",
	},
	"zookeeper-avg.latency": {
		Name: "zookeeper-avg-latency",
		Unit: "count",
	},
	"zookeeper-max.file.descriptor.count": {
		Name:    "zookeeper-max-file-descriptor",
		Counter: true,
		Unit:    "count",
	},
	"zookeeper-ephemerals.count": {
		Name:    "zookeeper-ephemerals",
		Counter: true,
		Unit:    "count",
	},
	"zookeeper-num.alive.connections": {
		Name: "zookeeper-alive-connections",
		Unit: "count",
	},
	"zookeeper-znode.count": {
		Name:    "zookeeper-znodes",
		Counter: true,
		Unit:    "count",
	},
	"zookeeper-watch.count": {
		Name:    "zookeeper-watch",
		Counter: true,
		Unit:    "count",
	},
	"kafka.controller-KafkaController": {
		Name: "kafka-controller",
		Unit: "count",
	},
	"kafka.network-RequestMetrics.Count.Produce.RequestsPerSec": {
		Name:    "kafka-produce-requests",
		Counter: true,
		Unit:    "count/s",
	},
	"kafka.network-RequestMetrics.Count.FetchConsumer.RequestsPerSec": {
		Name:    "kafka-fetch-consumer-requests",
		Counter: true,
		Unit:    "count/s",
	},
	"kafka.network-RequestMetrics.Count.FetchFollower.RequestsPerSec": {
		Name: "kafka-fetch-follower-requests",
		Unit: "count/s",
	},
	"kafka.network-RequestMetrics.Count.Produce.TotalTimeMs": {
		Name:       "kafka-produce-time-total",
		Counter:    true,
		Unit:       "s",
		Conversion: divideBy(1000),
	},
	"kafka.network-RequestMetrics.Count.FetchConsumer.TotalTimeMs": {
		Name:       "kafka-fetch-consumer-time-total",
		Unit:       "s",
		Conversion: divideBy(1000),
	},
	"kafka.network-RequestMetrics.Count.FetchFollower.TotalTimeMs": {
		Name:       "kafka-fetch-follower-time-total",
		Unit:       "s",
		Conversion: divideBy(1000),
	},
	"kafka.network-RequestMetrics.Min.Produce.TotalTimeMs": {
		Name:       "kafka-produce-time-total-min",
		Unit:       "s",
		Conversion: divideBy(1000),
	},
	"kafka.network-RequestMetrics.Max.Produce.TotalTimeMs": {
		Name:       "kafka-produce-time-total-max",
		Unit:       "s",
		Conversion: divideBy(1000),
	},
	"kafka.network-RequestMetrics.Min.FetchConsumer.TotalTimeMs": {
		Name:       "kafka-fetch-consumer-time-total-min",
		Unit:       "s",
		Conversion: divideBy(1000),
	},
	"kafka.network-RequestMetrics.Max.FetchConsumer.TotalTimeMs": {
		Name:       "kafka-fetch-consumer-time-total-max",
		Unit:       "s",
		Conversion: divideBy(1000),
	},
	"kafka.network-RequestMetrics.Min.FetchFollower.TotalTimeMs": {
		Name:       "kafka-fetch-follower-time-total-min",
		Unit:       "s",
		Conversion: divideBy(1000),
	},
	"kafka.network-RequestMetrics.Max.FetchFollower.TotalTimeMs": {
		Name:       "kafka-fetch-follower-time-total-max",
		Unit:       "s",
		Conversion: divideBy(1000),
	},
	"kafka.server-Fetch.queue-size": {
		Name: "kafka-fetch-queue-size",
		Unit: "count",
	},
	"kafka.server-DelayedFetchMetrics.Count.follower": {
		Name:    "kafka-delayed-fetch-follower",
		Counter: true,
		Unit:    "count",
	},
	"kafka.server-DelayedFetchMetrics.Count.consumer": {
		Name:    "kafka-delayed-fetch-consumer",
		Counter: true,
		Unit:    "count",
	},
	"kafka.server-DelayedOperationPurgatory": {
		Name: "kafka-delayed-operation-purgatory",
		Unit: "count",
	},
	"kafka.server-Fetch.byte-rate": {
		Name: "kafka-fetch-byte-rate",
		Unit: "count",
	},
	"kafka.server-Fetch.throttle-time": {
		Name: "kafka-fetch-throttle-time",
		Unit: "count",
	},
	"kafka.server-FetcherLagMetrics": {
		Name: "kafka-fetcher-lag",
		Unit: "count",
	},
	"kafka.server-FetcherStats.Count.BytesPerSec": {
		Name:    "kafka-fetcher-bytes",
		Counter: true,
		Unit:    "B/s",
	},
	"kafka.server-FetcherStats.Count.RequestsPerSec": {
		Name:    "kafka-fetcher-requests",
		Counter: true,
		Unit:    "count/s",
	},
	"kafka.server-KafkaRequestHandlerPool.Count": {
		Name:    "kafka-request-handler-pool",
		Counter: true,
		Unit:    "count",
	},
	"kafka.server-LeaderReplication.byte-rate": {
		Name: "kafka-leader-replication-rate",
		Unit: "B",
	},
	"kafka.server-Produce.byte-rate": {
		Name: "kafka-produce-byte-rate",
		Unit: "count",
	},
	"kafka.server-Produce.queue-size": {
		Name: "kafka-produce-queue-size",
		Unit: "count",
	},
	"kafka.server-Produce.throttle-time": {
		Name: "kafka-produce-throttle-time",
		Unit: "count",
	},
	"kafka.server-replica-fetcher-metrics.connection-close-rate": {
		Name: "kafka-replica-fetcher-connection-close-rate",
		Unit: "count",
	},
	"kafka.server-replica-fetcher-metrics.connection-count": {
		Name: "kafka-replica-fetcher-connection-count",
		Unit: "count",
	},
	"kafka.server-replica-fetcher-metrics.connection-creation-rate": {
		Name: "kafka-replica-fetcher-connection-creation-rate",
		Unit: "count",
	},
	"kafka.server-replica-fetcher-metrics.incoming-byte-rate": {
		Name: "kafka-replica-fetcher-incoming-byte-rate",
		Unit: "count",
	},
	"kafka.server-replica-fetcher-metrics.io-ratio": {
		Name: "kafka-replica-fetcher-io-rate",
		Unit: "count",
	},
	"kafka.server-replica-fetcher-metrics.io-time-ns-avg": {
		Name:       "kafka-replica-fetcher-io-time",
		Unit:       "s",
		Conversion: divideBy(1000 * 1000 * 1000),
	},
	"kafka.server-replica-fetcher-metrics.io-wait-ratio": {
		Name: "kafka-replica-fetcher-io-wait-ratio",
		Unit: "count",
	},
	"kafka.server-replica-fetcher-metrics.io-wait-time-ns-avg": {
		Name:       "kafka-replica-fetcher-io-wait-time",
		Unit:       "s",
		Conversion: divideBy(1000 * 1000 * 1000),
	},
	"kafka.server-replica-fetcher-metrics.network-io-rate": {
		Name: "kafka-replica-fetcher-network-io-rate",
		Unit: "count",
	},
	"kafka.server-replica-fetcher-metrics.outgoing-byte-rate": {
		Name: "kafka-replica-fetcher-outgoing-byte-rate",
		Unit: "count",
	},
	"kafka.server-replica-fetcher-metrics.request-rate": {
		Name: "kafka-replica-fetcher-request-rate",
		Unit: "coutn",
	},
	"kafka.server-replica-fetcher-metrics.request-size-avg": {
		Name: "kafka-replica-fetcher-request-size-avg",
		Unit: "B",
	},
	"kafka.server-replica-fetcher-metrics.request-size-max": {
		Name: "kafka-replica-fetcher-request-size-max",
		Unit: "B",
	},
	"kafka.server-replica-fetcher-metrics.response-rate": {
		Name: "kafka-replica-fetcher-response-rate",
		Unit: "count",
	},
	"kafka.server-replica-fetcher-metrics.select-rate": {
		Name: "kafka-replica-fetcher-select-rate",
		Unit: "count",
	},
	"kafka.server-ReplicaFetcherManager": {
		Name: "kafka-fetcher-replica-manager",
		Unit: "count",
	},
	"kafka.server-SessionExpireListener.Count": {
		Name:    "kafka-session-expiry-listener",
		Counter: true,
		Unit:    "count",
	},
	"kafka.server-socket-server-metrics.connection-close-rate": {
		Name: "kafka-socket-connection-close-rate",
		Unit: "count",
	},
	"kafka.server-socket-server-metrics.connection-count": {
		Name: "kafka-socket-connection-count",
		Unit: "count",
	},
	"kafka.server-socket-server-metrics.connection-creation-rate": {
		Name: "kafka-socket-connection-creation-rate",
		Unit: "count",
	},
	"kafka.server-socket-server-metrics.incoming-byte-rate": {
		Name: "kafka-socket-incoming-byte-rate",
		Unit: "count",
	},
	"kafka.server-socket-server-metrics.io-ratio": {
		Name: "kafka-socket-io-ratio",
		Unit: "count",
	},
	"kafka.server-socket-server-metrics.io-time-ns-avg": {
		Name:       "kafka-socket-avg-io-time",
		Unit:       "s",
		Conversion: divideBy(1000 * 1000 * 1000),
	},
	"kafka.server-socket-server-metrics.io-wait-ratio": {
		Name: "kafka-socket-io-wait",
		Unit: "count",
	},
	"kafka.server-socket-server-metrics.io-wait-time-ns-avg": {
		Name:       "kafka-socket-io-wait-time",
		Unit:       "s",
		Conversion: divideBy(1000 * 1000 * 1000),
	},
	"kafka.server-socket-server-metrics.network-io-rate": {
		Name: "kafka-socket-network-io-rate",
		Unit: "count",
	},
	"kafka.server-socket-server-metrics.outgoing-byte-rate": {
		Name: "kafka-socket-outgoing-byte-rate",
		Unit: "count",
	},
	"kafka.server-socket-server-metrics.request-rate": {
		Name: "kafka-socket-request-rate",
		Unit: "count",
	},
	"kafka.server-socket-server-metrics.request-size-avg": {
		Name: "kafka-socket-request-size-avg",
		Unit: "B",
	},
	"kafka.server-socket-server-metrics.request-size-max": {
		Name: "kafka-socket-request-size-max",
		Unit: "B",
	},
	"kafka.server-socket-server-metrics.response-rate": {
		Name: "kafka-socket-response-rate",
		Unit: "count",
	},
	"kafka.server-socket-server-metrics.select-rate": {
		Name: "kafka-socket-select-rate",
		Unit: "count",
	},
	"kafka.server-BrokerTopicMetrics.Count.BytesInPerSec": {
		Name:    "kafka-bytes-in",
		Counter: true,
		Unit:    "B/s",
	},
	"kafka.server-BrokerTopicMetrics.Count.BytesOutPerSec": {
		Name:    "kafka-bytes-out",
		Counter: true,
		Unit:    "B/s",
	},
	"kafka.server-BrokerTopicMetrics.Count.BytesRejectedPerSec": {
		Name: "kafka-bytes-rejected",
		Unit: "B/s",
	},
	"kafka.server-BrokerTopicMetrics.Count.FailedFetchRequestsPerSec": {
		Name: "kafka-failed-fetch-requests",
		Unit: "count/s",
	},
	"kafka.server-BrokerTopicMetrics.Count.FailedProduceRequestsPerSec": {
		Name:    "kafka-failed-produce-requests",
		Counter: true,
		Unit:    "count/s",
	},
	"kafka.server-BrokerTopicMetrics.Count.MessagesInPerSec": {
		Name:    "kafka-messages-in",
		Counter: true,
		Unit:    "count/s",
	},
	"kafka.server-BrokerTopicMetrics.Count.TotalFetchRequestsPerSec": {
		Name:    "kafka-fetch-requests",
		Counter: true,
		Unit:    "count/s",
	},
	"kafka.server-BrokerTopicMetrics.Count.TotalProduceRequestsPerSec": {
		Name:    "kafka-produce-requests",
		Counter: true,
		Unit:    "count/s",
	},
	"kafka.server-ReplicaManager.Count": {
		Name: "kafka-replica-manager-count",
		Unit: "count",
	},
	"kafka.server-KafkaServer": {
		Name: "kafka-servers",
		Unit: "count",
	},
	"kafka.server-ReplicaManager": {
		Name: "kafka-servers",
		Unit: "count",
	},
	"kafka.server-kafka-metrics-count": {
		Name: "kafka-metrics-count",
		Unit: "count",
	},
	"kafka.controller-ControllerStats.Count": {
		Name: "kafka-controller-stats",
		Unit: "count",
	},
	"kafka.controller-ControllerStats.Min": {
		Name: "kafka-controller-stats-min",
		Unit: "count",
	},
	"kafka.controller-ControllerStats.Max": {
		Name: "kafka-controller-stats-max",
		Unit: "count",
	},
	"minio_network_sent_bytes_total-counter": {
		Name: "minio-network-sent-total",
		Unit: "B",
	},
	"minio_network_received_bytes_total-counter": {
		Name: "minio-network-received-total",
		Unit: "B",
	},
	"minio_disk_storage_bytes-gauge": {
		Name: "minio-disk-storage-available",
		Unit: "B",
	},
	"minio_disk_storage_free_bytes-gauge": {
		Name: "minio-disk-storage-free",
		Unit: "B",
	},
	"minio_offline_disks-gauge": {
		Name: "minio-offline-disks",
		Unit: "count",
	},
	"minio_total_disks-gauge": {
		Name: "minio-total-disks",
		Unit: "count",
	},
	"minio_http_requests_duration_seconds-sum": {
		Name: "minio-http-requests-duration-aggregate",
		Unit: "s",
	},
	"minio_http_requests_duration_seconds-count": {
		Name: "minio-http-requests-count",
		Unit: "count",
	},
	"influxdb_memstats-sys": {
		Name: "influxdb-memstats-sys",
		Unit: "B",
	},
	"influxdb_memstats-total.alloc": {
		Name: "influxdb-memstats-total-allocated",
		Unit: "B",
	},
	"influxdb_runtime-Alloc": {
		Name: "influxdb-runtime-alloc",
		Unit: "count",
	},
	"influxdb_runtime-Frees": {
		Name: "influxdb-runtime-frees",
		Unit: "count",
	},
	"influxdb_runtime-HeapAlloc": {
		Name: "influxdb-runtime-heal-alloc",
		Unit: "count",
	},
	"influxdb_runtime-HeapIdle": {
		Name: "influxdb-runtime-heal-idle",
		Unit: "count",
	},
	"influxdb_runtime-HeapInUse": {
		Name: "influxdb-runtime-heap-inuse",
		Unit: "count",
	},
	"influxdb_runtime-HeapObjects": {
		Name: "influxdb-runtime-heap-objects",
		Unit: "count",
	},
	"influxdb_runtime-HeapReleased": {
		Name: "influxdb-runtime-heap-released",
		Unit: "count",
	},
	"influxdb_runtime-HeapSys": {
		Name: "influxdb-runtime-heap-sys",
		Unit: "count",
	},
	"influxdb_runtime-Lookups": {
		Name: "influxdb-runtime-lookups",
		Unit: "count",
	},
	"influxdb_runtime-Mallocs": {
		Name: "influxdb-runtime-mallocs",
		Unit: "count",
	},
	"influxdb_runtime-PauseTotalNs": {
		Name:       "influxdb-runtime-pause-total",
		Unit:       "s",
		Conversion: divideBy(1000 * 1000 * 1000),
	},
	"influxdb_runtime-Sys": {
		Name: "influxdb-runtime-sys",
		Unit: "count",
	},
	"influxdb_runtime-TotalAlloc": {
		Name: "influxdb-runtime-totalalloc",
		Unit: "count",
	},
	"influxdb_queryExecutor-queryDurationNs": {
		Name:       "influxdb-query-duration",
		Unit:       "s",
		Conversion: divideBy(1000 * 1000 * 1000),
	},
	"influxdb_queryExecutor-queriesActive": {
		Name: "influxdb-queries-active",
		Unit: "count",
	},
	"influxdb_queryExecutor-queriesExecuted": {
		Name: "influxdb-queries-executed",
		Unit: "count",
	},
	"influxdb_queryExecutor-queriesFinished": {
		Name: "influxdb-queries-finished",
		Unit: "count",
	},
	"influxdb_queryExecutor-recoveredPanics": {
		Name: "influxdb-query-recovered-panics",
		Unit: "count",
	},
	"influxdb_database-numMeasurements": {
		Name: "influxdb-database-measurements",
		Unit: "count",
	},
	"influxdb_database-numSeries": {
		Name: "influxdb-database-series",
		Unit: "count",
	},
	"influxdb_httpd-serverError": {
		Name:    "influxdb-httpd-server-errors",
		Counter: true,
		Unit:    "count",
	},
	"influxdb_httpd-clientError": {
		Name:    "influxdb-httpd-client-errors",
		Counter: true,
		Unit:    "count",
	},
	"influxdb_httpd-req": {
		Name:    "influxdb-httpd-requests",
		Counter: true,
		Unit:    "count",
	},
	"influxdb_httpd-queryReq": {
		Name:    "influxdb-httpd-query-requests",
		Counter: true,
		Unit:    "count",
	},
	"influxdb_httpd-writeReq": {
		Name:    "influxdb-httpd-write-requests",
		Counter: true,
		Unit:    "count",
	},
	"influxdb_httpd-pingReq": {
		Name:    "influxdb-httpd-ping-requests",
		Counter: true,
		Unit:    "count",
	},
	"influxdb_httpd-pointsReq": {
		Name:    "influxdb-httpd-point-requests",
		Counter: true,
		Unit:    "count",
	},
}

// Translation bears the convertion info from the source to the CMP metric
type Translation struct {
	Name           string
	Specialisation string
	Unit           string
	Counter        bool
	Conversion     func(interface{}) interface{}
}

func subtractFrom100Percent(value interface{}) interface{} {
	return (100.0 - value.(float64))
}

func divideBy(divisor float64) func(value interface{}) interface{} {
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

func esClusterHealth(status interface{}) interface{} {
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

// PostMetrics is the payload sent to the CMP metrics API
type PostMetrics struct {
	MonitoringSystem string      `json:"monitoring_system"`
	ResourceID       string      `json:"resource_id"`
	Metrics          []DataPoint `json:"metrics"`
}

// DataPoint represents a CMP metric data point
type DataPoint struct {
	Name           string `json:"name"`
	Specialisation string `json:"specialisation,omitempty"`
	Unit           string `json:"unit"`
	Value          string `json:"value"`
	Time           string `json:"time"`
	Counter        bool   `json:"counter"`
}

// AddMetric appends a metric data point to the list of metrics
func (data *PostMetrics) AddMetric(item DataPoint) []DataPoint {
	data.Metrics = append(data.Metrics, item)
	return data.Metrics
}

// Connect makes a connection to CMP
func (a *CMP) Connect() error {
	if a.APIUser == "" || a.APIKey == "" || a.APIURL == "" || a.ResourceID == "" {
		return fmt.Errorf(
			"api_url, api_user, api_key and resource_id" +
				"are required fields for cmp output",
		)
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

// Write sends the metrics to CMP
func (a *CMP) Write(metrics []telegraf.Metric) error {
	if len(metrics) == 0 {
		return nil
	}
	payload := &PostMetrics{
		MonitoringSystem: "telegraf",
		ResourceID:       a.ResourceID,
	}

	for _, m := range metrics {
		log.Printf("D! [CMP] Process %+v", m)

		suffix := ""
		cpu := m.Tags()["cpu"]
		path := m.Tags()["path"]
		haproxyService := m.Tags()["proxy"] + "_" + m.Tags()["sv"]
		containerName := m.Tags()["com.docker.compose.service"]
		diskName := m.Tags()["name"]
		db := m.Tags()["db"]
		kafkaTopic := m.Tags()["topic"]
		kafkaBrokerHost := m.Tags()["brokerHost"]
		mongoDBName := m.Tags()["db_name"]

		if len(cpu) > 0 && cpu != "cpu-total" {
			suffix = cpu[3:]
		} else if len(path) > 0 {
			suffix = path
		} else if len(containerName) > 0 {
			suffix = containerName
		} else if m.Name() == "haproxy" && len(haproxyService) > 0 {
			suffix = haproxyService
		} else if m.Name() == "diskio" && len(diskName) > 0 {
			suffix = diskName
		} else if m.Name() == "postgresql" && len(db) > 0 {
			suffix = db
		} else if strings.HasPrefix(m.Name(), "mongodb_") && len(mongoDBName) > 0 {
			suffix = mongoDBName
		} else if strings.HasPrefix(m.Name(), "kafka.") && len(kafkaTopic) > 0 {
			suffix = kafkaTopic
		} else if strings.HasPrefix(m.Name(), "kafka.") && len(kafkaBrokerHost) > 0 {
			suffix = kafkaBrokerHost
		}

		timestamp := m.Time().UTC().Format("2006-01-02T15:04:05.999999Z")
		for k, v := range m.Fields() {
			if k == "DelayedFetchMetrics.Count" {
				k = fmt.Sprintf("%s.%s", k, m.Tags()["fetcherType"])
			} else if k == "BrokerTopicMetrics.Count" || k == "FetcherStats.Count" {
				k = fmt.Sprintf("%s.%s", k, m.Tags()["name"])
			} else if strings.HasPrefix(k, "RequestMetrics.") {
				k = fmt.Sprintf("%s.%s.%s", k, m.Tags()["request"], m.Tags()["name"])
			}
			metricName := m.Name() + "-" + strings.Replace(k, "_", ".", -1)
			translation, found := translateMap[metricName]
			if !found {
				log.Printf("D! [CMP] Skip %s", metricName)
				continue
			}

			if translation.Conversion != nil {
				v = translation.Conversion(v)
			}

			specialisations := []string{}
			if translation.Specialisation != "" {
				specialisations = append(specialisations, translation.Specialisation)
			}
			if suffix != "" {
				specialisations = append(specialisations, suffix)
			}

			p := DataPoint{
				Counter:        translation.Counter,
				Name:           translation.Name,
				Specialisation: strings.Join(specialisations, "."),
				Unit:           translation.Unit,
				Value:          fmt.Sprintf("%v", v),
				Time:           timestamp,
			}
			log.Printf(
				"D! [CMP] Create %s[%s] = %s(%s) %s",
				p.Name,
				p.Specialisation,
				p.Value,
				p.Unit,
				p.Time,
			)
			payload.AddMetric(p)
		}
	}

	cmpBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("unable to JSON-serialize the data points: %s", err.Error())
	}
	req, err := http.NewRequest(
		"POST",
		a.authenticatedURL(),
		bytes.NewBuffer(cmpBytes),
	)
	if err != nil {
		return fmt.Errorf("unable to prepare the HTTP request %s", err.Error())
	}

	if a.UserAgent == "" {
		a.UserAgent = "telegraf/unknown"
	}
	req.Header.Add("User-Agent", a.UserAgent)
	req.Header.Add("Content-Type", "application/json")
	req.SetBasicAuth(a.APIUser, a.APIKey)

	log.Printf(
		"I! [CMP] Sending %d data points generated from %d metrics to the API",
		len(payload.Metrics),
		len(metrics),
	)
	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("API call failed: %s", err.Error())
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Printf("E! [CMP] failed to parse CMP response body: %s", err)
		}
		return fmt.Errorf("received a non-200 response: %s %s", resp.Status, body)
	}

	return nil
}

// SampleConfig returns a sample plugin config
func (a *CMP) SampleConfig() string {
	return sampleConfig
}

// Description returns the plugin description
func (a *CMP) Description() string {
	return "Configuration for CMP Server to send metrics to."
}

func (a *CMP) authenticatedURL() string {
	return fmt.Sprintf("%s/metrics", a.APIURL)
}

// Close closes the connection
func (a *CMP) Close() error {
	a.client = nil
	return nil
}

func init() {
	outputs.Add("cmp", func() telegraf.Output {
		return &CMP{}
	})
}

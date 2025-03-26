package models

import (
	"time"
)

// LogEntry represents a log entry in ClickHouse
type LogEntry struct {
	Timestamp          time.Time         `ch:"timestamp"`
	TraceID            string            `ch:"trace_id"`
	SpanID             string            `ch:"span_id"`
	ParentSpanID       string            `ch:"parent_span_id"`
	ServiceName        string            `ch:"service_name"`
	Level              string            `ch:"level"`
	Message            string            `ch:"message"`
	Component          string            `ch:"component"`
	Tenant             string            `ch:"tenant"`
	UserID             string            `ch:"user_id"`
	Method             string            `ch:"method"`
	Path               string            `ch:"path"`
	StatusCode         int32             `ch:"status_code"`
	DurationMs         int64             `ch:"duration_ms"`
	Attributes         map[string]string `ch:"attributes"`
	ResourceAttributes map[string]string `ch:"resource_attributes"`
	ErrorType          string            `ch:"error_type"`
	ErrorMessage       string            `ch:"error_message"`
	StackTrace         string            `ch:"stack_trace"`

	// Derived fields for partitioning and optimizations
	YearMonth uint32    `ch:"year_month,materialized:toYYYYMM(timestamp)"`
	Date      time.Time `ch:"date,materialized:toDate(timestamp)"`
}

// TableName returns the ClickHouse table name
func (LogEntry) TableName() string {
	return "logs"
}

// CreateTableSQL returns the SQL statement to create the logs table
func (LogEntry) CreateTableSQL() string {
	return `CREATE TABLE IF NOT EXISTS logs (
		timestamp DateTime64(3),
		trace_id String,
		span_id String,
		parent_span_id String,
		service_name String,
		level String,
		message String,
		component String,
		tenant String,
		user_id String,
		method String,
		path String,
		status_code Int32,
		duration_ms Int64,
		attributes Map(String, String),
		resource_attributes Map(String, String),
		error_type String,
		error_message String,
		stack_trace String,
		
		year_month UInt32 MATERIALIZED toYYYYMM(timestamp),
		date Date MATERIALIZED toDate(timestamp),

		INDEX idx_trace_id trace_id TYPE minmax GRANULARITY 1,
		INDEX idx_timestamp timestamp TYPE minmax GRANULARITY 1,
		INDEX idx_tenant tenant TYPE minmax GRANULARITY 1,
		INDEX idx_user user_id TYPE minmax GRANULARITY 1
	) ENGINE = MergeTree()
	PARTITION BY year_month
	ORDER BY (timestamp, trace_id)
	SETTINGS index_granularity = 8192`
}

// TraceEntry represents a trace entry in ClickHouse
type TraceEntry struct {
	Timestamp          time.Time         `ch:"timestamp"`
	TraceID            string            `ch:"trace_id"`
	SpanID             string            `ch:"span_id"`
	ParentSpanID       string            `ch:"parent_span_id"`
	ServiceName        string            `ch:"service_name"`
	Name               string            `ch:"name"`
	Kind               string            `ch:"kind"`
	StatusCode         string            `ch:"status_code"`
	StatusMessage      string            `ch:"status_message"`
	StartTime          time.Time         `ch:"start_time"`
	EndTime            time.Time         `ch:"end_time"`
	DurationMs         int64             `ch:"duration_ms"`
	Attributes         map[string]string `ch:"attributes"`
	ResourceAttributes map[string]string `ch:"resource_attributes"`
	Tenant             string            `ch:"tenant"`
	UserID             string            `ch:"user_id"`

	// Derived fields for partitioning and optimizations
	YearMonth uint32    `ch:"year_month,materialized:toYYYYMM(timestamp)"`
	Date      time.Time `ch:"date,materialized:toDate(timestamp)"`
}

// TableName returns the ClickHouse table name
func (TraceEntry) TableName() string {
	return "traces"
}

// CreateTableSQL returns the SQL statement to create the traces table
func (TraceEntry) CreateTableSQL() string {
	return `CREATE TABLE IF NOT EXISTS traces (
		timestamp DateTime64(3),
		trace_id String,
		span_id String,
		parent_span_id String,
		service_name String,
		name String,
		kind String,
		status_code String,
		status_message String,
		start_time DateTime64(3),
		end_time DateTime64(3),
		duration_ms Int64,
		attributes Map(String, String),
		resource_attributes Map(String, String),
		tenant String,
		user_id String,

		year_month UInt32 MATERIALIZED toYYYYMM(timestamp),
		date Date MATERIALIZED toDate(timestamp),

		INDEX idx_trace_id trace_id TYPE minmax GRANULARITY 1,
		INDEX idx_timestamp timestamp TYPE minmax GRANULARITY 1,
		INDEX idx_tenant tenant TYPE minmax GRANULARITY 1,
		INDEX idx_user user_id TYPE minmax GRANULARITY 1
	) ENGINE = MergeTree()
	PARTITION BY year_month
	ORDER BY (timestamp, trace_id)
	SETTINGS index_granularity = 8192`
}

package repository

import (
	"context"
	"fmt"

	"github.com/opentracing/opentracing-go"
	"github.com/uptrace/go-clickhouse/ch"

	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/tracing"
)

type emailStore struct {
	ch *ch.DB
}

func NewEmailStore(ch *ch.DB) interfaces.EmailStore {
	return &emailStore{
		ch: ch,
	}
}

// InitSchema ensures the emails table exists in ClickHouse
func (r *emailStore) InitSchema(ctx context.Context) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "EmailStore.InitSchema")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	// Use the raw SQL from the model
	sql := models.EmailStore{}.CreateTableSQL()

	// Execute the raw SQL
	_, err := r.ch.Exec(sql)
	if err != nil {
		tracing.TraceErr(span, err)
		return err
	}

	return nil
}

// SaveEmail stores an email in ClickHouse
func (r *emailStore) SaveEmail(ctx context.Context, email *models.EmailStore) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "EmailStore.SaveEmail")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	// Validate input
	if email == nil {
		return fmt.Errorf("cannot save nil email")
	}

	span.SetTag("email.id", email.ID)
	span.SetTag("email.message_id", email.MessageID)

	// Explicitly specify the table name to avoid naming conflicts
	_, err := r.ch.NewInsert().
		Model(email).
		ModelTableExpr("emails"). // Explicitly set the table name
		ExcludeColumn("year_month", "day_of_week", "hour", "body_text_size", "body_html_size", "_shard_key").
		Exec(ctx)
	if err != nil {
		tracing.TraceErr(span, err)
		return fmt.Errorf("failed to save email to ClickHouse: %w", err)
	}

	return nil
}

func (r *emailStore) EmailExists(ctx context.Context, messageId string) (bool, error) {
	var count uint64
	err := r.ch.QueryRow(
		"SELECT count() FROM emails WHERE message_id = ?",
		messageId,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check email existence: %w", err)
	}

	return count > 0, nil
}

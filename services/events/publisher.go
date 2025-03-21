package events

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"github.com/rabbitmq/amqp091-go"

	"github.com/customeros/mailstack/dto"
	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/logger"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/tracing"
	"github.com/customeros/mailstack/internal/utils"
)

const (
	// Exchange names
	ExchangeMailstackDirect = "mailstack-direct"
	ExchangeCustomerOS      = "customeros"
	ExchangeNotifications   = "notifications"
	ExchangeDeadLetter      = "dead-letter"

	// queues
	QueueNotifications = "notifications"
	QueueMailstack     = "events-mailstack"
	QueueSendEmail     = "send-email"
	QueueReceiveEmail  = "receive-email"
	DLQMailstack       = QueueMailstack + "-dlq"
	DLQNotifications   = QueueNotifications + "-dlq"
	DLQSendEmail       = QueueSendEmail + "-dlq"
	DLQReceiveEmail    = QueueReceiveEmail + "-dlq"

	// routing keys
	RoutingKeyDeadLetter   = "dead-letter"
	RoutingKeySendEmail    = "mailstack-send-email"
	RoutingKeyReceiveEmail = "mailstack-receive-email"

	// Default configurations
	DefaultMessageTTL          = 240 * time.Hour // after TTL message moves to DLQ
	DefaultMaxRetries          = 3
	DefaultPublishTimeout      = 5 * time.Second
	DefaultReconnectBackoff    = time.Second
	DefaultMaxReconnectBackoff = 30 * time.Second
)

type PublisherConfig struct {
	MessageTTL          time.Duration
	MaxRetries          int
	PublishTimeout      time.Duration
	ReconnectBackoff    time.Duration
	MaxReconnectBackoff time.Duration
}

type RabbitMQPublisher struct {
	connection      *amqp091.Connection
	connectionMutex sync.Mutex
	publishChannel  *amqp091.Channel
	publishMutex    sync.Mutex
	url             string
	logger          logger.Logger
	confirms        chan amqp091.Confirmation
	config          PublisherConfig
}

func NewRabbitMQPublisher(rabbitmqURL string, logger logger.Logger, config *PublisherConfig) (*RabbitMQPublisher, error) {
	if config == nil {
		config = &PublisherConfig{
			MessageTTL:          DefaultMessageTTL,
			MaxRetries:          DefaultMaxRetries,
			PublishTimeout:      DefaultPublishTimeout,
			ReconnectBackoff:    DefaultReconnectBackoff,
			MaxReconnectBackoff: DefaultMaxReconnectBackoff,
		}
	}

	publisher := &RabbitMQPublisher{
		url:    rabbitmqURL,
		logger: logger,
		config: *config,
	}

	err := publisher.connect()
	if err != nil {
		return nil, err
	}

	return publisher, nil
}

func (r *RabbitMQPublisher) PublishRecieveEmailEvent(ctx context.Context, message dto.EmailReceived) error {
	switch message.Source {
	case enum.EmailImportIMAP:
		id := fmt.Sprintf("%s-%s-%s", message.MailboxID, message.Folder, message.ImapUID)
		return r.publishEventOnExchange(ctx, id, enum.EMAIL, message, ExchangeMailstackDirect, RoutingKeyReceiveEmail)
	default:
		return errors.New("not implemented yet")
	}
}

func (r *RabbitMQPublisher) PublishSendEmailEvent(ctx context.Context, email *models.Email) error {
	return r.publishEventOnExchange(ctx, email.ID, enum.EMAIL, dto.SendEmail{Email: email}, ExchangeMailstackDirect, RoutingKeySendEmail)
}

func (r *RabbitMQPublisher) PublishFanoutEvent(ctx context.Context, entityId string, entityType enum.EntityType, message interface{}) error {
	err := utils.ValidateTenant(ctx)
	if err != nil {
		return err
	}
	return r.publishEventOnExchange(ctx, entityId, entityType, message, ExchangeCustomerOS, "")
}

func (r *RabbitMQPublisher) PublishNotification(ctx context.Context, tenant string, entityId string, entityType enum.EntityType, details *utils.EventCompletedDetails) {
	r.PublishNotificationBulk(ctx, tenant, []string{entityId}, entityType, details)
}

func (r *RabbitMQPublisher) PublishNotificationBulk(ctx context.Context, tenant string, entityIds []string, entityType enum.EntityType, details *utils.EventCompletedDetails) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "RabbitMQPublisher.PublishEventCompletedBulk")
	defer span.Finish()
	tracing.TagTenant(span, tenant)
	if len(entityIds) == 1 {
		tracing.TagEntity(span, entityIds[0])
	}
	span.LogKV("entityType", entityType, "entityIds", entityIds)

	event := dto.EventCompleted{
		Tenant:     tenant,
		EntityType: entityType,
		EntityIds:  entityIds,
		Create:     false,
		Update:     false,
		Delete:     false,
	}

	if details != nil {
		event.Create = details.Create
		event.Update = details.Update
		event.Delete = details.Delete
	}

	err := r.publishMessageOnExchange(ctx, event, ExchangeNotifications, "")
	if err != nil {
		tracing.TraceErr(span, err)
		r.logger.Errorf("Failed to publish event completed notification: %v", err)
	}
	span.LogKV("result.published", true)
}

func (r *RabbitMQPublisher) setupPublishChannel() error {
	channel, err := r.connection.Channel()
	if err != nil {
		return errors.Wrap(err, "Failed to open publish channel")
	}

	// Enable publisher confirms
	err = channel.Confirm(false)
	if err != nil {
		channel.Close()
		return errors.Wrap(err, "Failed to enable publisher confirms")
	}

	r.confirms = channel.NotifyPublish(make(chan amqp091.Confirmation, 1))
	r.publishChannel = channel
	return nil
}

func (r *RabbitMQPublisher) handleReconnection() {
	backoff := r.config.ReconnectBackoff

	for {
		notifyClose := r.connection.NotifyClose(make(chan *amqp091.Error))
		err := <-notifyClose
		r.logger.Warnf("RabbitMQ connection closed: %v, attempting to reconnect", err)

		for {
			err := r.connect()
			if err == nil {
				r.logger.Info("Successfully reconnected to RabbitMQ")
				break
			}

			r.logger.Errorf("Failed to reconnect: %v, retrying in %v", err, backoff)
			time.Sleep(backoff)

			// Exponential backoff with max limit
			backoff *= 2
			if backoff > r.config.MaxReconnectBackoff {
				backoff = r.config.MaxReconnectBackoff
			}
		}

		// Reset backoff after successful reconnection
		backoff = r.config.ReconnectBackoff
	}
}

func (r *RabbitMQPublisher) setupExchangesAndQueues() error {
	channel, err := r.connection.Channel()
	if err != nil {
		return errors.Wrap(err, "Failed to open channel for exchange/queue setup")
	}
	defer channel.Close()

	err = r.declareExchanges(channel)
	if err != nil {
		return err
	}

	err = r.declareAndBindQueues(channel)
	if err != nil {
		return err
	}

	return nil
}

func (r *RabbitMQPublisher) declareExchanges(channel *amqp091.Channel) error {
	// Dead Letter Exchange (direct)
	err := channel.ExchangeDeclare(
		ExchangeDeadLetter,
		"direct",
		true,  // durable
		false, // auto-deleted
		false, // internal
		false, // no-wait
		nil,   // arguments
	)
	if err != nil {
		return errors.Wrap(err, "Failed to declare dead letter exchange")
	}

	// Notifications exchange (fanout)
	err = channel.ExchangeDeclare(
		ExchangeNotifications,
		"fanout",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return errors.Wrap(err, "Failed to declare notifications exchange")
	}

	// CustomerOS exchange (fanout)
	err = channel.ExchangeDeclare(
		ExchangeCustomerOS,
		"fanout",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return errors.Wrap(err, "Failed to declare customeros exchange")
	}

	// Mailstack direct exchange
	err = channel.ExchangeDeclare(
		ExchangeMailstackDirect,
		"direct",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return errors.Wrap(err, "Failed to declare customeros-direct exchange")
	}

	return nil
}

func (r *RabbitMQPublisher) declareAndBindQueues(channel *amqp091.Channel) error {
	// Notifications queue with DLQ
	err := r.declareQueueWithDLQ(channel, QueueNotifications, DLQNotifications)
	if err != nil {
		return err
	}
	err = channel.QueueBind(
		QueueNotifications,
		"",
		ExchangeNotifications,
		false,
		nil,
	)
	if err != nil {
		return errors.Wrapf(err, "Failed to bind queue %s to exchange %s", QueueNotifications, ExchangeNotifications)
	}

	// CustomerOS fanout queues with DLQs
	customerosQueues := []struct {
		queueName string
		dlqName   string
	}{
		{QueueMailstack, DLQMailstack},
	}

	for _, q := range customerosQueues {
		err := r.declareQueueWithDLQ(channel, q.queueName, q.dlqName)
		if err != nil {
			return err
		}
		err = channel.QueueBind(
			q.queueName,
			"",
			ExchangeCustomerOS,
			false,
			nil,
		)
		if err != nil {
			return errors.Wrapf(err, "Failed to bind queue %s to exchange %s", q.queueName, ExchangeCustomerOS)
		}
	}

	// Mailstack SendEmail direct queue with DLQ
	err = r.declareQueueWithDLQ(channel, QueueSendEmail, DLQSendEmail)
	if err != nil {
		return err
	}
	err = channel.QueueBind(
		QueueSendEmail,
		RoutingKeySendEmail,
		ExchangeMailstackDirect,
		false,
		nil,
	)
	if err != nil {
		return errors.Wrapf(err, "Failed to bind queue %s to exchange %s", QueueSendEmail, ExchangeMailstackDirect)
	}

	// Mailstack ReceiveEmail direct queue with DLQ
	err = r.declareQueueWithDLQ(channel, QueueReceiveEmail, DLQReceiveEmail)
	if err != nil {
		return err
	}
	err = channel.QueueBind(
		QueueReceiveEmail,
		RoutingKeyReceiveEmail,
		ExchangeMailstackDirect,
		false,
		nil,
	)
	if err != nil {
		return errors.Wrapf(err, "Failed to bind queue %s to exchange %s", QueueSendEmail, ExchangeMailstackDirect)
	}

	return nil
}

func (r *RabbitMQPublisher) declareQueueWithDLQ(channel *amqp091.Channel, queueName string, dlqName string) error {
	// First declare the DLQ
	_, err := channel.QueueDeclare(
		dlqName,
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return errors.Wrapf(err, "Failed to declare DLQ %s", dlqName)
	}

	// Bind DLQ to dead letter exchange
	err = channel.QueueBind(
		dlqName,
		RoutingKeyDeadLetter,
		ExchangeDeadLetter,
		false,
		nil,
	)
	if err != nil {
		return errors.Wrapf(err, "Failed to bind DLQ %s to exchange", dlqName)
	}

	// Declare main queue with DLQ configuration
	args := make(map[string]interface{})
	args["x-dead-letter-exchange"] = ExchangeDeadLetter
	args["x-dead-letter-routing-key"] = RoutingKeyDeadLetter
	args["x-message-ttl"] = int64(r.config.MessageTTL.Milliseconds())

	_, err = channel.QueueDeclare(
		queueName,
		true,
		false,
		false,
		false,
		args,
	)
	if err != nil {
		return errors.Wrapf(err, "Failed to declare queue %s", queueName)
	}

	return nil
}

func (r *RabbitMQPublisher) connect() error {
	r.connectionMutex.Lock()
	defer r.connectionMutex.Unlock()

	var err error
	r.connection, err = amqp091.Dial(r.url)
	if err != nil {
		return errors.Wrap(err, "Failed to connect to RabbitMQ")
	}

	err = r.setupExchangesAndQueues()
	if err != nil {
		return errors.Wrap(err, "Failed to setup exchanges and queues")
	}

	err = r.setupPublishChannel()
	if err != nil {
		return errors.Wrap(err, "Failed to setup publish channel")
	}

	go r.handleReconnection()

	return nil
}

func (r *RabbitMQPublisher) ensureConnectionAndChannel() error {
	if r.connection == nil || r.connection.IsClosed() {
		if err := r.connect(); err != nil {
			return errors.Wrap(err, "Failed to establish connection")
		}
	}

	if r.publishChannel == nil || r.publishChannel.IsClosed() {
		if err := r.setupPublishChannel(); err != nil {
			return errors.Wrap(err, "Failed to establish channel")
		}
	}

	return nil
}

func (r *RabbitMQPublisher) publishEventOnExchange(ctx context.Context, entityId string, entityType enum.EntityType, message interface{}, exchange, routingKey string) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "RabbitMQPublisher.PublishEventOnExchange")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	tracingData := tracing.ExtractTextMapCarrier((span).Context())

	messageType := reflect.TypeOf(message)
	if messageType.Kind() == reflect.Ptr {
		messageType = messageType.Elem()
	}

	eventMessage := dto.Event{
		Event: dto.EventDetails{
			Id:         utils.GenerateNanoIDWithPrefix("event", 21),
			EntityId:   entityId,
			EntityType: entityType,
			Tenant:     utils.GetTenantFromContext(ctx),
			EventType:  messageType.Name(),
			Data:       message,
		},
		Metadata: dto.EventMetadata{
			UberTraceId: tracingData["uber-trace-id"],
			UserId:      utils.GetUserIdFromContext(ctx),
			UserEmail:   utils.GetUserEmailFromContext(ctx),
			Timestamp:   utils.Now().Format(time.RFC3339),
		},
	}

	return r.publishMessageOnExchange(ctx, eventMessage, exchange, routingKey)
}

func (r *RabbitMQPublisher) publishMessageOnExchange(ctx context.Context, message interface{}, exchange, routingKey string) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "RabbitMQPublisher.PublishMessageOnExchange")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)

	tracing.LogObjectAsJson(span, "message", message)

	for attempt := 0; attempt < r.config.MaxRetries; attempt++ {
		err := r.publishWithConfirm(ctx, message, exchange, routingKey)
		if err == nil {
			return nil
		}

		r.logger.Warnf("Publish attempt %d failed: %v", attempt+1, err)
		if attempt < r.config.MaxRetries-1 {
			time.Sleep(time.Millisecond * 100 * time.Duration(attempt+1))
		}
	}

	return errors.New("Failed to publish message after all retries")
}

func (r *RabbitMQPublisher) publishWithConfirm(ctx context.Context, message interface{}, exchange, routingKey string) error {
	r.publishMutex.Lock()
	defer r.publishMutex.Unlock()

	// Check context cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Ensure connection and channel are healthy
	if err := r.ensureConnectionAndChannel(); err != nil {
		return err
	}

	jsonBody, err := json.Marshal(message)
	if err != nil {
		return errors.Wrap(err, "Failed to marshal message")
	}

	actualRoutingKey := routingKey
	if exchange == ExchangeCustomerOS || exchange == ExchangeNotifications {
		actualRoutingKey = ""
	}

	err = r.publishChannel.Publish(
		exchange,
		actualRoutingKey,
		true,  // mandatory - ensure message is routed
		false, // immediate
		amqp091.Publishing{
			DeliveryMode: amqp091.Persistent,
			ContentType:  "application/json",
			Body:         jsonBody,
			Timestamp:    time.Now(),
		})
	if err != nil {
		return errors.Wrap(err, "Failed to publish message")
	}

	// Wait for confirmation with timeout
	select {
	case confirm := <-r.confirms:
		if !confirm.Ack {
			return errors.New("Message was not confirmed by server")
		}
	case <-time.After(r.config.PublishTimeout):
		return errors.New("Publish confirmation timeout")
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

// Close gracefully shuts down the publisher
func (r *RabbitMQPublisher) Close() error {
	r.connectionMutex.Lock()
	defer r.connectionMutex.Unlock()

	var err error
	if r.publishChannel != nil {
		err = r.publishChannel.Close()
		if err != nil {
			r.logger.Errorf("Error closing publish channel: %v", err)
		}
	}

	if r.connection != nil {
		if closeErr := r.connection.Close(); closeErr != nil {
			r.logger.Errorf("Error closing connection: %v", closeErr)
			if err == nil {
				err = closeErr
			}
		}
	}

	return err
}

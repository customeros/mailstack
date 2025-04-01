package email_content

import (
	"fmt"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/enum"
	nats_internal "github.com/customeros/mailstack/internal/nats"
)

type emailContentService struct {
	natsConn *nats_internal.NATSConnections
}

func NewEmailContentService(natsConn *nats_internal.NATSConnections) interfaces.EmailContentService {
	return &emailContentService{
		natsConn: natsConn,
	}
}

const (
	// queue group
	QUEUE_GROUP = "email-content-service"

	// consumer config
	CONSUMER_NAME         = "email-content-consumer"
	ACK_WAIT              = 30 * time.Second
	MAX_DELIVERY_ATTEMPTS = 5
	MAX_ACK_PENDING       = 100
	FETCH_BATCH_SIZE      = 50
	MAX_FETCH_WAIT        = 500 * time.Millisecond
	ERR_BACKOFF           = 100 * time.Millisecond
)

// Start begins listening for raw email events and processing them
func (s *emailContentService) Start(ctx context.Context) error {
	// Create durable consumer for processing emails
	_, err := s.natsConn.JS.AddConsumer(nats_internal.EMAIL_STREAM, &nats.ConsumerConfig{
		Durable:       CONSUMER_NAME,
		AckPolicy:     nats.AckExplicitPolicy,
		AckWait:       ACK_WAIT,
		MaxDeliver:    MAX_DELIVERY_ATTEMPTS,
		FilterSubject: enum.EventEmailInboundClassifiedOK.String(),
		MaxAckPending: MAX_ACK_PENDING,
	})
	if err != nil {
		return fmt.Errorf("failed to create consumer: %w", err)
	}

	// Create pull subscription
	sub, err := s.natsConn.JS.PullSubscribe(
		enum.EventEmailInboundReceivedIMAP.String(),
		QUEUE_GROUP,
		nats.Bind(nats_internal.EMAIL_STREAM, CONSUMER_NAME),
	)
	if err != nil {
		return fmt.Errorf("failed to create subscription: %w", err)
	}

	// Start processing
	go s.processEmails(ctx, sub)

	return nil
}

// processEmails continuously processes raw email events
func (s *emailStorageService) processRawEmailEvents(ctx context.Context, sub *nats.Subscription) {
	log.Println("Email Storage Service started")

	for {
		select {
		case <-ctx.Done():
			log.Println("Email Storage Service shutting down")
			return
		default:
			// Fetch messages batch
			msgs, err := sub.Fetch(FETCH_BATCH_SIZE, nats.MaxWait(MAX_FETCH_WAIT))
			if err != nil {
				if err == nats.ErrTimeout {
					// No messages available, this is normal
					continue
				}
				log.Printf("Fetch error: %v", err)
				time.Sleep(ERR_BACKOFF) // Small backoff on error
				continue
			}

			for _, msg := range msgs {
				// Parse the raw message
				var rawEmail dto.EmailReceivedIMAP
				if err := json.Unmarshal(msg.Data, &rawEmail); err != nil {
					log.Printf("Failed to unmarshal inbound.email.received.imap: %v", err)
					msg.Ack() // Ack malformed messages to avoid redelivery
					continue
				}

				// Process the email
				err := s.HandleIMAPEmail(ctx, rawEmail)

				if err != nil {
					log.Printf("Processing error: %v", err)
					metadata, _ := msg.Metadata()

					// Check if we should retry
					if metadata.NumDelivered <= uint64(MAX_DELIVERY_ATTEMPTS) {
						// Negative acknowledgment triggers redelivery
						msg.Nak()
					} else {
						// Max retries reached, acknowledge but publish to dead letter
						msg.Ack()
						s.publishError(ctx, msg.Data, err)
					}
				} else {
					// Successfully processed
					msg.Ack()
				}
			}
		}
	}
}

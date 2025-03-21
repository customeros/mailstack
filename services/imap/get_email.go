package imap

import (
	"context"
	"fmt"

	"github.com/emersion/go-imap"
	"github.com/opentracing/opentracing-go"

	"github.com/customeros/mailstack/internal/tracing"
)

func (s *IMAPService) GetMessageByUID(ctx context.Context, mailboxID, folderName string, uid uint32) (*imap.Message, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "IMAPService.GetMessageByUID")
	defer span.Finish()
	tracing.SetDefaultServiceSpanTags(ctx, span)
	span.SetTag("mailbox_id", mailboxID)
	span.SetTag("folder", folderName)
	span.SetTag("uid", uid)

	// Get the client for this mailbox
	client, err := s.getConnectedClient(ctx, mailboxID)
	if err != nil {
		tracing.TraceErr(span, err)
		return nil, err
	}

	// Select the folder
	_, err = client.Select(folderName, true) // Read-only mode
	if err != nil {
		tracing.TraceErr(span, err)
		return nil, err
	}

	// Create search criteria for the message by UID
	criteria := imap.NewSearchCriteria()
	criteria.Uid = new(imap.SeqSet)
	criteria.Uid.AddNum(uid)

	// Search for the message
	uids, err := client.Search(criteria)
	if err != nil {
		tracing.TraceErr(span, err)
		return nil, err
	}

	if len(uids) == 0 {
		err = fmt.Errorf("message with UID %d not found", uid)
		tracing.TraceErr(span, err)
		return nil, err
	}

	// Fetch the full message
	seqSet := new(imap.SeqSet)
	seqSet.AddNum(uids[0])

	// Define items to fetch - include everything needed by the processor
	items := []imap.FetchItem{
		imap.FetchEnvelope,
		imap.FetchBodyStructure,
		imap.FetchFlags,
		imap.FetchUid,
		imap.FetchRFC822, // Fetch the full message content
	}

	messages := make(chan *imap.Message, 1)
	err = client.Fetch(seqSet, items, messages)
	if err != nil {
		tracing.TraceErr(span, err)
		return nil, err
	}

	// Get the message from the channel
	msg, ok := <-messages
	if !ok {
		err = fmt.Errorf("failed to fetch message with UID %d", uid)
		tracing.TraceErr(span, err)
		return nil, err
	}

	return msg, nil
}

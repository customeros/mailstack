package enum

type NATSBucket string

const (
	NATSBucketEmailAttachment NATSBucket = "EMAIL_ATTACHMENTS"
)

func (e NATSBucket) String() string {
	return string(e)
}

package utils

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// Convert a time.Time to a protobuf Timestamp
func TimeToProto(t time.Time) *timestamppb.Timestamp {
	return timestamppb.New(t)
}

// Convert a pointer to time.Time to a protobuf Timestamp
func TimePointerToProto(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}

package helpers

import (
	"github.com/customeros/mailstack/internal/utils"
	"github.com/customeros/mailstack/proto/pb"
)

func AllRecipients(req *pb.EmailClassificationRequest) []string {
	result := make([]string, 0)

	for _, r := range req.To {
		if !utils.IsStringInSlice(r.Email, result) {
			result = append(result, r.Email)
		}
	}

	for _, r := range req.Cc {
		if !utils.IsStringInSlice(r.Email, result) {
			result = append(result, r.Email)
		}
	}

	for _, r := range req.Bcc {
		if !utils.IsStringInSlice(r.Email, result) {
			result = append(result, r.Email)
		}
	}

	return result
}

func emailsAsSlice(emails []*pb.EmailAddress) []string {
	result := make([]string, 0)

	for _, r := range emails {
		if !utils.IsStringInSlice(r.Email, result) {
			result = append(result, r.Email)
		}
	}
	return result
}

package dto

import (
	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/utils"
)

type EmailClassificationRequest struct {
	EmailID            string         `json:"emailId"`
	Subject            string         `json:"subject"`
	From               EmailAddress   `json:"from"`
	To                 []EmailAddress `json:"to"`
	Cc                 []EmailAddress `json:"cc"`
	Bcc                []EmailAddress `json:"bcc"`
	ReplyTo            EmailAddress   `json:"replyTo"`
	ReturnPath         string         `json:"returnPath"`
	Unsubscribe        string         `json:"unsubscribe"`
	Precedence         string         `json:"precedence"`
	Sender             string         `json:"sender"`
	XAutoReply         string         `json:"xAutoReply"`
	XAutoResponse      string         `json:"xAutoResponse"`
	XLoop              string         `json:"xLoop"`
	XFailedRecipients  string         `json:"xFailedRecipients"`
	ContentDescription string         `json:"contentDescription"`
	FeedbackID         string         `json:"feedbackId"`
	ForwardedFor       string         `json:"forwardedFor"`
	DKIM               string         `json:"dkim"`
	SPF                string         `json:"spf"`
	DMARC              string         `json:"dmarc"`
	ListUnsubscribe    string         `json:"listUnsubscribe"`
	AutoSubmitted      string         `json:"autoSubmitted"`
}

type EmailClassificationResponse struct {
	EmailID        string                   `json:"emailID"`
	Classification enum.EmailClassification `json:"classification"`
	Details        string                   `json:"details"`
	ErrorMessage   string                   `json:"errorMessage"`
}

type EmailAddress struct {
	Name   string `json:"name"`
	Email  string `json:"email"`
	User   string `json:"user"`
	Domain string `json:"domain"`
}

func (e EmailClassificationRequest) EventType() enum.EmailEvent {
	return enum.EventEmailInboundClassify
}

func (e *EmailClassificationRequest) ToAddresses() []string {
	result := make([]string, 0)

	for _, r := range e.To {
		if !utils.IsStringInSlice(r.Email, result) {
			result = append(result, r.Email)
		}
	}
	return result
}

func (e *EmailClassificationRequest) CcAddresses() []string {
	result := make([]string, 0)

	for _, r := range e.Cc {
		if !utils.IsStringInSlice(r.Email, result) {
			result = append(result, r.Email)
		}
	}
	return result
}

func (e *EmailClassificationRequest) BccAddresses() []string {
	result := make([]string, 0)

	for _, r := range e.Bcc {
		if !utils.IsStringInSlice(r.Email, result) {
			result = append(result, r.Email)
		}
	}
	return result
}

func (e *EmailClassificationRequest) AllRecipients() []string {
	result := make([]string, 0)

	for _, email := range e.ToAddresses() {
		if !utils.IsStringInSlice(email, result) {
			result = append(result, email)
		}
	}
	for _, email := range e.CcAddresses() {
		if !utils.IsStringInSlice(email, result) {
			result = append(result, email)
		}
	}
	for _, email := range e.BccAddresses() {
		if !utils.IsStringInSlice(email, result) {
			result = append(result, email)
		}
	}
	return result
}

package dto

type AskAIForEmailRequest struct {
	EmailFrom        string `json:"emailFrom"`
	FromEmailAddress string `json:"fromEmailAddress"`
	ToEmailAddress   string `json:"toEmailAddress"`
	EmailBodyText    string `json:"emailBodyText"`
	EmailBodyHTML    string `json:"emailBodyHtml"`
}

type AskAIForEmailResponse struct {
	EmailData EmailResponse `json:"emailData"`
}

type EmailResponse struct {
	MessageBody  string         `json:"messageBody"`
	HasSignature bool           `json:"hasSignature"`
	Signature    EmailSignature `json:"signature,omitempty"`
}

type EmailSignature struct {
	ContactInfo EmailSignatureContactInfo `json:"contactInfo"`
	CompanyInfo EmailSignatureCompanyInfo `json:"companyInfo"`
}

type EmailSignatureContactInfo struct {
	Name         string `json:"name"`
	JobTitle     string `json:"jobTitle"`
	Company      string `json:"company"`
	Email        string `json:"email"`
	Phone        string `json:"phone"`
	Mobile       string `json:"mobile"`
	LinkedIn     string `json:"linkedin"`
	GitHub       string `json:"github"`
	CalendarLink string `json:"calendarLink"`
}

// EmailSignatureCompanyInfo contains company information
type EmailSignatureCompanyInfo struct {
	Website   string                `json:"website"`
	LinkedIn  string                `json:"linkedin"`
	Twitter   string                `json:"twitter"`
	Youtube   string                `json:"youtube"`
	Instagram string                `json:"instagram"`
	GitHub    string                `json:"github"`
	Address   EmailSignatureAddress `json:"address"`
}

// EmailSignatureAddress contains address information
type EmailSignatureAddress struct {
	Street     string `json:"street"`
	City       string `json:"city"`
	Region     string `json:"region"`
	PostalCode string `json:"postalCode"`
	Country    string `json:"country"`
}

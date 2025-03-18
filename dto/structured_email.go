package dto

type StructuredEmailRequest struct {
	FromName         string `json:"emailFrom"`
	FromEmailAddress string `json:"fromEmailAddress"`
	ToName           string `json:"emailTo"`
	ToEmailAddress   string `json:"toEmailAddress"`
	EmailBodyText    string `json:"emailBodyText"`
	EmailBodyHTML    string `json:"emailBodyHTML"`
}

type StructuredEmailResponse struct {
	EmailData EmailData `json:"emailData"`
	RequestID string    `json:"requestId"`
	Status    string    `json:"status"`
}

// EmailData contains the parsed email information
type EmailData struct {
	HasSignature bool           `json:"hasSignature"`
	MessageBody  string         `json:"messageBody"`
	Signature    EmailSignature `json:"signature,omitempty"`
}

// EmailSignature represents the complete email signature
type EmailSignature struct {
	CompanyInfo EmailSignatureCompanyInfo `json:"companyInfo"`
	ContactInfo EmailSignatureContactInfo `json:"contactInfo"`
}

// EmailSignatureCompanyInfo contains company information
type EmailSignatureCompanyInfo struct {
	Address   EmailSignatureAddress `json:"address"`
	Domain    string                `json:"domain"`
	GitHub    string                `json:"github"`
	Instagram string                `json:"instagram"`
	LinkedIn  string                `json:"linkedin"`
	Twitter   string                `json:"twitter"`
	Website   string                `json:"website"`
	Youtube   string                `json:"youtube"`
}

// EmailSignatureContactInfo contains the contact information of a person
type EmailSignatureContactInfo struct {
	CalendarLink string `json:"calendarLink"`
	Company      string `json:"company"`
	Email        string `json:"email"`
	GitHub       string `json:"github"`
	JobTitle     string `json:"jobTitle"`
	LinkedIn     string `json:"linkedin"`
	Mobile       string `json:"mobile"`
	Name         string `json:"name"`
	Phone        string `json:"phone"`
}

// EmailSignatureAddress contains address information
type EmailSignatureAddress struct {
	City       string `json:"city"`
	Country    string `json:"country"`
	PostalCode string `json:"postalCode"`
	Region     string `json:"region"`
	Street     string `json:"street"`
}

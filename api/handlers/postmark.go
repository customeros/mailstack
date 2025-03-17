package handlers

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime/debug"
	"strings"

	"github.com/customeros/mailsherpa/mailvalidate"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/tracing"
	"github.com/customeros/mailstack/services"
	dmarcstats "github.com/customeros/mailwatcher/dmarkstats"
	"github.com/gin-gonic/gin"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
)

type PostmarkHandler struct {
	repos *repository.Repositories
	svc   *services.Services
}

func NewPostmarkHandler(repos *repository.Repositories, svc *services.Services) *PostmarkHandler {
	return &PostmarkHandler{
		svc:   svc,
		repos: repos,
	}
}

func (h *PostmarkHandler) PostmarkDMARCMonitor() gin.HandlerFunc {
	return func(c *gin.Context) {
		span, ctx := opentracing.StartSpanFromContext(c.Request.Context(), "PostmarkHandler.PostmarkDMARCMonitor")
		defer span.Finish()
		tracing.SetDefaultRestSpanTags(ctx, span)

		// Parse email data
		emailData, err := h.parseInboundEmail(c)
		if err != nil {
			tracing.LogObjectAsJson(span, "body", c.Request.Body)
			tracing.TraceErr(span, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Return accepted response immediately
		c.JSON(http.StatusAccepted, gin.H{"message": "Accepted"})

		// Process email asynchronously
		go func() {
			defer func() {
				if r := recover(); r != nil {
					stack := debug.Stack()
					err := fmt.Errorf("panic recovered in email processing: %v\n%s", r, stack)
					tracing.TraceErr(span, err)
				}
			}()

			var err error
			if emailData.IsMonitorEmail() {
				err = h.processDmarcMonitoringReport(c, &emailData)
				if err != nil {
					tracing.TraceErr(span, errors.Wrap(err, "failed to process DMARC report"))
				}
			}
		}()
	}

}

func (h *PostmarkHandler) parseInboundEmail(c *gin.Context) (postmarkInboundEmailData, error) {
	var emailData postmarkInboundEmailData
	err := c.BindJSON(&emailData)
	if err != nil {
		return emailData, err
	}
	return emailData, nil
}

func (h *PostmarkHandler) processDmarcMonitoringReport(ctx context.Context, emailData *postmarkInboundEmailData) error {
	span, ctx := tracing.StartTracerSpan(ctx, "PostmarkHandler.processDmarcMonitoringReport")
	defer span.Finish()
	tracing.SetDefaultRestSpanTags(ctx, span)

	// Get attachment, unzip, feed file to dmark analyzer service
	attachment := emailData.Attachments[0]
	if attachment.ContentType != "application/zip" && attachment.ContentType != "application/gzip" {
		return fmt.Errorf("attachment %s is not a zip file", attachment.Name)
	}

	provider := emailData.DMARCReportProvider()

	reports, err := decodeAndReadDMARCReportFile(attachment.Content, attachment.ContentType)
	if err != nil {
		return fmt.Errorf("cannot parse dmarc report %s from attachment: %v", attachment.Name, err)
	}
	for _, report := range reports {
		dbReport := h.buildDMARCReport(ctx, report, provider)
		// todo - if tenant is empty, don't send report to database
		// leaving this in for now to verify everything is working as expected
		h.repos.DomainRepository.CreateDMARCReport(ctx, dbReport.Tenant, &dbReport)
	}
	return nil
}

func (h *PostmarkHandler) buildDMARCReport(ctx context.Context, report dmarcstats.Report, provider string) models.DMARCMonitoring {
	span, ctx := tracing.StartTracerSpan(ctx, "PostmarkHandler.buildDMARCReport")
	defer span.Finish()
	tracing.SetDefaultRestSpanTags(ctx, span)

	tenant, err := h.svc.DomainService.GetTenantForMailstackDomain(ctx, report.Domain)
	if err != nil {
		tracing.TraceErr(span, fmt.Errorf("unable to get tenant for domain %s: %v", report.Domain, err))
	}

	jsonReport, _ := json.Marshal(report)
	return models.DMARCMonitoring{
		Tenant:        tenant,
		EmailProvider: provider,
		Domain:        report.Domain,
		ReportStart:   report.ReportPeriod.Start,
		ReportEnd:     report.ReportPeriod.End,
		MessageCount:  report.TotalMessages,
		SPFPass:       report.AuthResults.SPFPassCount,
		DKIMPass:      report.AuthResults.DKIMPassCount,
		DMARCPass:     report.AuthResults.DMARCPassCount,
		Data:          string(jsonReport),
	}
}

func decodeAndReadDMARCReportFile(attachment, contentType string) ([]dmarcstats.Report, error) {
	var reports []dmarcstats.Report

	// Decode base64 string to bytes
	decoded, err := base64.StdEncoding.DecodeString(attachment)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64: %w", err)
	}

	switch contentType {
	case "application/zip":
		reports, err = handleZipFile(decoded)
	case "application/gzip":
		reports, err = handleGzipFile(decoded)
	default:
		return nil, fmt.Errorf("unsupported content type: %s", contentType)
	}

	if err != nil {
		return nil, err
	}

	return reports, nil
}

func handleZipFile(decoded []byte) ([]dmarcstats.Report, error) {
	var reports []dmarcstats.Report

	zipReader, err := zip.NewReader(bytes.NewReader(decoded), int64(len(decoded)))
	if err != nil {
		return nil, fmt.Errorf("failed to create zip reader: %w", err)
	}

	for _, file := range zipReader.File {
		report, err := processZipFile(file)
		if err != nil {
			return nil, err
		}
		reports = append(reports, *report)
	}

	return reports, nil
}

func handleGzipFile(decoded []byte) ([]dmarcstats.Report, error) {
	var reports []dmarcstats.Report

	gzReader, err := gzip.NewReader(bytes.NewReader(decoded))
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzReader.Close()

	report, err := dmarcstats.AnalyzeDMARCReport(gzReader)
	if err != nil {
		return nil, fmt.Errorf("failed to analyze DMARC report from gzip: %w", err)
	}

	reports = append(reports, *report)
	return reports, nil
}

func processZipFile(file *zip.File) (*dmarcstats.Report, error) {
	rc, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open zip file %s: %w", file.Name, err)
	}
	defer rc.Close()

	report, err := dmarcstats.AnalyzeDMARCReport(rc)
	if err != nil {
		return nil, fmt.Errorf("failed to analyze DMARC report for file %s: %w", file.Name, err)
	}

	return report, nil
}

type postmarkInboundEmailData struct {
	FromName      string `json:"FromName"`
	MessageStream string `json:"MessageStream"`
	From          string `json:"From"`
	FromFull      struct {
		Email       string `json:"Email"`
		Name        string `json:"Name"`
		MailboxHash string `json:"MailboxHash"`
	} `json:"FromFull"`
	To     string `json:"To"`
	ToFull []struct {
		Email       string `json:"Email"`
		Name        string `json:"Name"`
		MailboxHash string `json:"MailboxHash"`
	} `json:"ToFull"`
	Cc     string `json:"Cc"`
	CcFull []*struct {
		Email       string `json:"Email"`
		Name        string `json:"Name"`
		MailboxHash string `json:"MailboxHash"`
	} `json:"CcFull"`
	Bcc     string `json:"Bcc"`
	BccFull []*struct {
		Email       string `json:"Email"`
		Name        string `json:"Name"`
		MailboxHash string `json:"MailboxHash"`
	} `json:"BccFull"`
	OriginalRecipient string `json:"OriginalRecipient"`
	Subject           string `json:"Subject"`
	MessageID         string `json:"MessageID"`
	ReplyTo           string `json:"ReplyTo"`
	MailboxHash       string `json:"MailboxHash"`
	Date              string `json:"Date"`
	TextBody          string `json:"TextBody"`
	HtmlBody          string `json:"HtmlBody"`
	StrippedTextReply string `json:"StrippedTextReply"`
	Tag               string `json:"Tag"`
	Headers           []struct {
		Name  string `json:"Name"`
		Value string `json:"Value"`
	} `json:"Headers"`
	Attachments []struct {
		Name          string `json:"Name"`
		Content       string `json:"Content"`
		ContentType   string `json:"ContentType"`
		ContentLength int    `json:"ContentLength"`
	} `json:"Attachments"`
}

func (p *postmarkInboundEmailData) IsMonitorEmail() bool {
	for _, address := range p.BccFull {
		validation := mailvalidate.ValidateEmailSyntax(address.Email)
		if validation.IsValid && strings.EqualFold(validation.User, "monitor") {
			return true
		}
	}
	return false
}

func (p *postmarkInboundEmailData) DMARCReportProvider() string {
	filename := p.Attachments[0].Name
	parts := strings.Split(filename, "!")
	if len(parts) > 0 {
		switch {
		case parts[0] == "enterprise.protection.outlook.com":
			return "outlook.com"
		case parts[0] == "aol.com":
			return "yahoo.com"
		default:
			return parts[0]
		}
	}
	return ""
}

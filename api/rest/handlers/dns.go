package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/telemetry"
	"github.com/customeros/mailstack/internal/utils"
	"github.com/customeros/mailstack/services"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

type DNSHandler struct {
	domainService     interfaces.DomainService
	cloudflareService interfaces.CloudflareService
}

func NewDNSHandler(s *services.Services) *DNSHandler {
	return &DNSHandler{
		domainService:     s.DomainService,
		cloudflareService: s.CloudflareService,
	}
}

type DNSRecord struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

type DNSResponse struct {
	Records []DNSRecord `json:"dnsRecords"`
}

type DNSRecordResponse struct {
	Record DNSRecord `json:"dnsRecord"`
}

func (h *DNSHandler) AddDNSRecord() gin.HandlerFunc {
	return func(c *gin.Context) {
		spans, ctx := telemetry.StartRestSpan(c.Request.Context(), "DNSHandler.AddDNSRecord")
		defer spans.Finish()

		tenant := utils.GetTenantFromContext(ctx)

		// validate domain belongs to tenant
		domain := c.Param("domain")
		domainModel, err := h.domainService.GetDomain(ctx, domain)
		if err != nil {
			spans.TraceError(err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if domainModel == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "domain not found"})
			return
		}
		if domainModel.Tenant != tenant {
			c.JSON(http.StatusNotFound, gin.H{"error": "domain not found"})
			return
		}

		domainExists, zoneId, err := h.cloudflareService.CheckDomainExists(ctx, domain)
		if err != nil {
			spans.TraceError(err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if !domainExists {
			message := "domain not found"
			spans.TraceError(errors.New(message))
			c.JSON(http.StatusNotFound, gin.H{"error": message})
			return
		}

		// get dns record payload
		record, err := h.getDNSRequestPayload(c)
		if err != nil {
			spans.TraceError(err)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		err = h.cloudflareService.AddDNSRecord(ctx, zoneId, record.Type, record.Name, record.Content, 1, false, nil)
		if err != nil {
			spans.TraceError(err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, DNSRecordResponse{
			Record: record,
		})
	}
}

func (h *DNSHandler) getDNSRequestPayload(c *gin.Context) (DNSRecord, error) {
	spans, _ := telemetry.StartRestSpan(c.Request.Context(), "DNSHandler.getDNSRequestPayload")
	defer spans.Finish()

	var req DNSRecord
	err := c.ShouldBindJSON(&req)
	if err != nil {
		spans.TraceError(err)
		return req, err
	}

	return req, nil
}

func (h *DNSHandler) DeleteDNSRecord() gin.HandlerFunc {
	return func(c *gin.Context) {
		spans, ctx := telemetry.StartRestSpan(c.Request.Context(), "DNSHandler.DeleteDNSRecord")
		defer spans.Finish()

		tenant := utils.GetTenantFromContext(ctx)

		domain := c.Param("domain")
		domainModel, err := h.domainService.GetDomain(ctx, domain)
		if err != nil {
			spans.TraceError(err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if domainModel == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "domain not found"})
			return
		}
		if domainModel.Tenant != tenant {
			c.JSON(http.StatusNotFound, gin.H{"error": "domain not found"})
			return
		}

		domainExists, zoneId, err := h.cloudflareService.CheckDomainExists(ctx, domain)
		if err != nil {
			spans.TraceError(err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if !domainExists {
			message := "domain not found"
			spans.TraceError(errors.New(message))
			c.JSON(http.StatusNotFound, gin.H{"error": message})
			return
		}

		// delete dns record
		dnsRecordId := c.Param("id")
		dnsRecordId = strings.TrimPrefix(dnsRecordId, "dns_")
		err = h.cloudflareService.DeleteDNSRecord(ctx, zoneId, dnsRecordId)
		if err != nil {
			spans.TraceError(err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "DNS record deleted"})
	}
}

func (h *DNSHandler) GetDNSRecords() gin.HandlerFunc {
	return func(c *gin.Context) {
		spans, ctx := telemetry.StartRestSpan(c.Request.Context(), "DNSHandler.GetDNSRecords")
		defer spans.Finish()

		tenant := utils.GetTenantFromContext(ctx)

		domain := c.Param("domain")
		domainModel, err := h.domainService.GetDomain(ctx, domain)
		if err != nil {
			spans.TraceError(err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if domainModel == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "domain not found"})
			return
		}
		if domainModel.Tenant != tenant {
			c.JSON(http.StatusNotFound, gin.H{"error": "domain not found"})
			return
		}

		// get dns records
		dnsRecords, err := h.cloudflareService.GetDNSRecords(ctx, domain)
		if err != nil {
			spans.TraceError(err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if dnsRecords == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Unable to loacate DNS records for domain"})
			return
		}

		var records []DNSRecord

		for _, record := range *dnsRecords {
			records = append(records, DNSRecord{
				ID:      fmt.Sprintf("dns_%s", record.ID),
				Type:    record.Type,
				Name:    record.Name,
				Content: record.Content,
			})
		}

		c.JSON(http.StatusOK, DNSResponse{
			Records: records,
		})

	}
}

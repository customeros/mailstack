package handlers

import (
	"net/http"
	"strings"

	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/tracing"
	"github.com/customeros/mailstack/internal/utils"
	"github.com/customeros/mailstack/services"
	"github.com/gin-gonic/gin"
	"github.com/opentracing/opentracing-go"
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
		span, ctx := opentracing.StartSpanFromContext(c.Request.Context(), "DNSHandler.AddDNSRecord")
		defer span.Finish()
		tracing.SetDefaultRestSpanTags(ctx, span)

		tenant := utils.GetTenantFromContext(ctx)

		// validate domain belongs to tenant
		domain := c.Param("domain")
		domainModel, err := h.domainService.GetDomain(ctx, domain)
		if err != nil {
			tracing.TraceErr(span, err)
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
			tracing.TraceErr(span, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if !domainExists {
			message := "domain not found"
			tracing.TraceErr(span, errors.New(message))
			c.JSON(http.StatusNotFound, gin.H{"error": message})
			return
		}

		// get dns record payload
		record, err := h.getDNSRequestPayload(c)
		if err != nil {
			tracing.TraceErr(span, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		err = h.cloudflareService.AddDNSRecord(ctx, zoneId, record.Type, record.Name, record.Content, 1, false, nil)
		if err != nil {
			tracing.TraceErr(span, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, DNSRecordResponse{
			Record: record,
		})
	}
}

func (h *DNSHandler) getDNSRequestPayload(c *gin.Context) (DNSRecord, error) {
	span, ctx := opentracing.StartSpanFromContext(c.Request.Context(), "DNSHandler.getDNSRequestPayload")
	defer span.Finish()
	tracing.SetDefaultRestSpanTags(ctx, span)

	var req DNSRecord
	err := c.ShouldBindJSON(&req)
	if err != nil {
		tracing.TraceErr(span, err)
		return req, err
	}

	return req, nil
}

func (h *DNSHandler) DeleteDNSRecord() gin.HandlerFunc {
	return func(c *gin.Context) {
		span, ctx := opentracing.StartSpanFromContext(c.Request.Context(), "DNSHandler.DeleteDNSRecord")
		defer span.Finish()
		tracing.SetDefaultRestSpanTags(ctx, span)

		tenant := utils.GetTenantFromContext(ctx)

		domain := c.Param("domain")
		domainModel, err := h.domainService.GetDomain(ctx, domain)
		if err != nil {
			tracing.TraceErr(span, err)
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
			tracing.TraceErr(span, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if !domainExists {
			message := "domain not found"
			tracing.TraceErr(span, errors.New(message))
			c.JSON(http.StatusNotFound, gin.H{"error": message})
			return
		}

		// delete dns record
		dnsRecordId := c.Param("id")
		dnsRecordId = strings.TrimPrefix(dnsRecordId, "dns_")
		err = h.cloudflareService.DeleteDNSRecord(ctx, zoneId, dnsRecordId)
		if err != nil {
			tracing.TraceErr(span, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "DNS record deleted"})
	}
}

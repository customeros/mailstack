package handlers

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"

	"github.com/customeros/mailstack/internal/config"
	er "github.com/customeros/mailstack/internal/errors"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/tracing"
	"github.com/customeros/mailstack/internal/utils"
	"github.com/customeros/mailstack/services"
)

type RegisterNewDomainRequest struct {
	Domain  string `json:"domain"`
	Website string `json:"website"`
}

type PurchaseDomainRequest struct {
	Domain string `json:"domain"`
}

type ConfigureDomainRequest struct {
	Domain  string `json:"domain"`
	Website string `json:"website"`
}

type DomainResponse struct {
	Domain DomainRecord `json:"domain"`
}

type DomainsResponse struct {
	Domains []DomainRecord `json:"domains"`
}

type DomainRecord struct {
	Domain      string   `json:"domain"`
	Nameservers []string `json:"nameservers"`
	CreatedDate string   `json:"createdDate"`
	ExpiredDate string   `json:"expiredDate"`
}

type DomainAvailabilityResponse struct {
	IsAvailable bool `json:"isAvailable"`
	IsPremium   bool `json:"isPremium"`
}

type DomainHandler struct {
	repos *repository.Repositories
	cfg   *config.Config
	svc   *services.Services
}

func NewDomainHandler(r *repository.Repositories, cfg *config.Config, s *services.Services) *DomainHandler {
	return &DomainHandler{
		repos: r,
		cfg:   cfg,
		svc:   s,
	}
}

// RegisterNewDomain registers a new domain for the tenant
func (h *DomainHandler) RegisterNewDomain() gin.HandlerFunc {
	return func(c *gin.Context) {
		span, ctx := opentracing.StartSpanFromContext(c.Request.Context(), "DomainHandler.RegisterNewDomain")
		defer span.Finish()
		tracing.SetDefaultRestSpanTags(ctx, span)

		tenant := utils.GetTenantFromContext(ctx)

		// Parse and validate request body
		var req RegisterNewDomainRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			tracing.TraceErr(span, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		domain := req.Domain
		website := req.Website

		// Check for missing domain
		if req.Domain == "" {
			message := "Missing required field: domain"
			tracing.TraceErr(span, errors.New(message))
			c.JSON(http.StatusBadRequest, gin.H{"error": message})
			return
		} else if req.Website == "" {
			message := "Missing required field: website"
			tracing.TraceErr(span, errors.New(message))
			c.JSON(http.StatusBadRequest, gin.H{"error": message})
			return
		}

		// check if domain tld is supported
		// Extract the TLD from the domain (e.g., "com" from "example.com")
		tld := strings.Split(domain, ".")[1]
		if !utils.IsStringInSlice(tld, h.cfg.DomainConfig.SupportedTlds) {
			message := "Domain TLD not supported"
			tracing.TraceErr(span, errors.New(message))
			c.JSON(http.StatusNotAcceptable, gin.H{"error": message})
			return
		}
		// check if domain is available
		isAvailable, isPremium, err := h.svc.NamecheapService.CheckDomainAvailability(ctx, domain)
		if err != nil {
			tracing.TraceErr(span, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if !isAvailable {
			message := "Domain is not available"
			tracing.TraceErr(span, errors.New(message))
			c.JSON(http.StatusNotAcceptable, gin.H{"error": message})
			return
		}
		if isPremium {
			message := "Domain is premium"
			tracing.TraceErr(span, errors.New(message))
			c.JSON(http.StatusNotAcceptable, gin.H{"error": message})
			return
		}
		// check if domain price is exceeded
		domainPrice, err := h.svc.NamecheapService.GetDomainPrice(ctx, domain)
		if err != nil {
			tracing.TraceErr(span, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if domainPrice > h.cfg.NamecheapConfig.MaxPrice {
			message := "Domain price is exceeded"
			tracing.TraceErr(span, errors.New(message))
			c.JSON(http.StatusNotAcceptable, gin.H{"error": message})
			return
		}
		// register domain
		err = h.svc.NamecheapService.PurchaseDomain(ctx, tenant, domain)
		if err != nil {
			tracing.TraceErr(span, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// configure domain
		domainRecord, err := h.configureDomain(ctx, domain, website)
		if errors.Is(err, er.ErrConnectionTimeout) {
			message := "Connection timeout, please retry"
			tracing.TraceErr(span, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": message})
			return
		} else if errors.Is(err, er.ErrDomainConfigurationFailed) {
			message := "Domain configuration failed"
			tracing.TraceErr(span, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": message})
			return
		} else if err != nil {
			tracing.TraceErr(span, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Success
		c.JSON(http.StatusCreated, DomainResponse{Domain: domainRecord})
	}
}

func (h *DomainHandler) configureDomain(ctx context.Context, domain, website string) (DomainRecord, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "DomainHandler.configureDomain")
	defer span.Finish()
	tracing.SetDefaultRestSpanTags(ctx, span)

	tenant := utils.GetTenantFromContext(ctx)

	domainResponse := DomainRecord{}
	domainResponse.Domain = domain

	var err error

	domainBelongsToTenant, err := h.repos.DomainRepository.CheckDomainOwnership(ctx, tenant, domain)
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "Error checking domain"))
		return domainResponse, err
	}
	if !domainBelongsToTenant {
		return domainResponse, er.ErrDomainNotFound
	}

	err = h.svc.DomainService.ConfigureDomain(ctx, domain, website)
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "Error configuring domain"))
		return domainResponse, er.ErrDomainConfigurationFailed
	}

	// get domain details
	domainInfo, err := h.svc.NamecheapService.GetDomainInfo(ctx, tenant, domain)
	if err != nil {
		tracing.TraceErr(span, errors.Wrap(err, "Error getting domain info"))
		return domainResponse, err
	}
	domainResponse.CreatedDate = domainInfo.CreatedDate
	domainResponse.ExpiredDate = domainInfo.ExpiredDate
	domainResponse.Nameservers = domainInfo.Nameservers
	domainResponse.Domain = domainInfo.DomainName

	return domainResponse, nil
}

func (h *DomainHandler) GetDomains() gin.HandlerFunc {
	return func(c *gin.Context) {
		span, ctx := opentracing.StartSpanFromContext(c.Request.Context(), "DomainHandler.GetDomains")
		defer span.Finish()
		tracing.SetDefaultRestSpanTags(ctx, span)

		tenant := utils.GetTenantFromContext(ctx)

		// get all active domains from postgres
		activeDomainRecords, err := h.repos.DomainRepository.GetActiveDomains(ctx, tenant)
		if err != nil {
			tracing.TraceErr(span, errors.Wrap(err, "Error retrieving domains"))
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		response := DomainsResponse{
			Domains: make([]DomainRecord, 0, len(activeDomainRecords)),
		}

		for _, domainRecord := range activeDomainRecords {
			domain, err := h.svc.NamecheapService.GetDomainInfo(ctx, tenant, domainRecord.Domain)
			if err != nil {
				message := "Unable to retreive domain info"
				tracing.TraceErr(span, err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": message})
				return
			}
			response.Domains = append(response.Domains, DomainRecord{
				Domain:      domain.DomainName,
				CreatedDate: domain.CreatedDate,
				ExpiredDate: domain.ExpiredDate,
				Nameservers: domain.Nameservers,
			})
		}

		c.JSON(http.StatusOK, response)
	}
}

func (h *DomainHandler) GetRecommendations() gin.HandlerFunc {
	return func(c *gin.Context) {
		span, ctx := opentracing.StartSpanFromContext(c.Request.Context(), "DomainHandler.GetRecommendations")
		defer span.Finish()
		tracing.SetDefaultRestSpanTags(ctx, span)

		// get root domain
		baseName, exists := c.GetQuery("baseName")
		if !exists {
			message := "Must provide baseName"
			tracing.TraceErr(span, errors.New(message))
			c.JSON(http.StatusBadRequest, gin.H{"error": message})
			return
		}

		// get domain recommendations
		recommendations := h.svc.MailboxService.RecommendOutboundDomains(ctx, baseName, 500)

		var response struct {
			Recommendations []string `json:"recommendations"`
		}
		response.Recommendations = recommendations

		tracing.LogObjectAsJson(span, "result", response)
		c.JSON(http.StatusOK, response)
	}
}

func (h *DomainHandler) ConfigureDomain() gin.HandlerFunc {
	return func(c *gin.Context) {
		span, ctx := opentracing.StartSpanFromContext(c.Request.Context(), "DomainHandler.ConfigureDomain")
		defer span.Finish()
		tracing.SetDefaultRestSpanTags(ctx, span)

		var req ConfigureDomainRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			tracing.TraceErr(span, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if req.Domain == "" {
			message := "Missing required field: domain"
			tracing.TraceErr(span, errors.New(message))
			c.JSON(http.StatusBadRequest, gin.H{"error": message})
			return
		} else if req.Website == "" {
			message := "Missing required field: website"
			tracing.TraceErr(span, errors.New(message))
			c.JSON(http.StatusBadRequest, gin.H{"error": message})
			return
		}

		domainResponse, err := h.configureDomain(ctx, req.Domain, req.Website)
		if err != nil {
			tracing.TraceErr(span, err)
			if errors.Is(err, er.ErrDomainNotFound) {
				message := "Domain not found"
				c.JSON(http.StatusNotFound, gin.H{"error": message})
				return
			} else if errors.Is(err, er.ErrDomainConfigurationFailed) {
				message := "Domain configuration failed"
				c.JSON(http.StatusInternalServerError, gin.H{"error": message})
				return
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}

		c.JSON(http.StatusOK, domainResponse)
	}
}

func (h *DomainHandler) CheckAvailability() gin.HandlerFunc {
	return func(c *gin.Context) {
		span, ctx := opentracing.StartSpanFromContext(c.Request.Context(), "DomainHandler.CheckAvailability")
		defer span.Finish()
		tracing.SetDefaultRestSpanTags(ctx, span)

		domain := c.Param("domain")
		if domain == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "domain parameter is required"})
			return
		}

		isAvailable, isPremium, err := h.svc.NamecheapService.CheckDomainAvailability(ctx, domain)
		if err != nil {
			tracing.TraceErr(span, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, DomainAvailabilityResponse{
			IsAvailable: isAvailable,
			IsPremium:   isPremium,
		})
	}
}

func (h *DomainHandler) PurchaseDomain() gin.HandlerFunc {
	return func(c *gin.Context) {
		span, ctx := opentracing.StartSpanFromContext(c.Request.Context(), "DomainHandler.PurchaseDomain")
		defer span.Finish()
		tracing.SetDefaultRestSpanTags(ctx, span)

		tenant := utils.GetTenantFromContext(ctx)

		// Parse and validate request body
		var req PurchaseDomainRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			tracing.TraceErr(span, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Check for missing domain
		if req.Domain == "" {
			message := "Missing required field: domain"
			tracing.TraceErr(span, errors.New(message))
			c.JSON(http.StatusBadRequest, gin.H{"error": message})
			return
		}

		// purchase domain
		err := h.svc.NamecheapService.PurchaseDomain(ctx, tenant, req.Domain)
		if err != nil {
			tracing.TraceErr(span, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// get domain details
		domainInfo, err := h.svc.NamecheapService.GetDomainInfo(ctx, tenant, req.Domain)
		if err != nil {
			tracing.TraceErr(span, errors.New("Domain purchased but failed to retrieve details"))
			c.JSON(http.StatusOK, DomainResponse{Domain: DomainRecord{Domain: req.Domain}})
			return
		}

		c.JSON(http.StatusCreated, DomainResponse{
			Domain: DomainRecord{
				Domain:      domainInfo.DomainName,
				CreatedDate: domainInfo.CreatedDate,
				ExpiredDate: domainInfo.ExpiredDate,
				Nameservers: domainInfo.Nameservers,
			},
		})
	}
}

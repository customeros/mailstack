package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/opentracing/opentracing-go"

	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/tracing"
	"github.com/customeros/mailstack/internal/utils"
)

type RegisterNewDomainRequest struct {
	Domain  string `json:"domain" binding:"required"`
	Website string `json:"website" binding:"required"`
}

type DomainResponse struct {
	Domain DomainRecord `json:"domain"`
}

type DomainRecord struct {
	Domain  string `json:"domain"`
	Website string `json:"website"`
}

type DomainHandler struct {
	domainRepository repository.DomainRepository
}

func NewDomainHandler(repos *repository.Repositories) *DomainHandler {
	return &DomainHandler{
		domainRepository: repos.DomainRepository,
	}
}

// RegisterNewDomain registers a new domain for the tenant
func (h *DomainHandler) RegisterNewDomain() gin.HandlerFunc {
	return func(c *gin.Context) {
		span, ctx := opentracing.StartSpanFromContext(c.Request.Context(), "RegisterNewDomain")
		defer span.Finish()
		tracing.SetDefaultRestSpanTags(ctx, span)

		tenant := utils.GetTenantFromContext(ctx)
		// if tenant missing return auth error
		if tenant == "" {
			tracing.TraceErr(span, errors.New("Missing tenant in context"))
			c.JSON(http.StatusNotFound, gin.H{"error": "Missing tenant in context"})
			return
		}

		// Parse and validate request body
		var req RegisterNewDomainRequest
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
		} else if req.Website == "" {
			message := "Missing required field: website"
			tracing.TraceErr(span, errors.New(message))
			c.JSON(http.StatusBadRequest, gin.H{"error": message})
			return
		}

		// Check if domain already exists
		existingDomain, err := h.domainRepository.GetByDomain(ctx, req.Domain)
		if err == nil && existingDomain != nil {
			message := "Domain already registered"
			tracing.TraceErr(span, errors.New(message))
			c.JSON(http.StatusConflict, gin.H{"error": message})
			return
		}

		// Create domain record
		domain := &models.Domain{
			Tenant:  tenant,
			Domain:  req.Domain,
			Website: req.Website,
		}

		err = h.domainRepository.Create(ctx, domain)
		if err != nil {
			message := "Failed to register domain"
			tracing.TraceErr(span, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": message})
			return
		}

		// Return success response
		c.JSON(http.StatusOK, DomainResponse{
			Domain: DomainRecord{
				Domain:  domain.Domain,
				Website: domain.Website,
			},
		})
	}
}

package utils

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	er "github.com/customeros/mailstack/internal/errors"
)

// TenantHeaders is a list of possible header names for tenant identification
var TenantHeaders = []string{
	"X-TENANT",
	"x-tenant",
	"X-Tenant",
	"tenant",
	"Tenant",
	"TENANT",
	"tenantname",
	"TenantName",
	"tenantName",
	"TENANTNAME",
}

type CustomContext struct {
	AppSource  string
	Tenant     string
	AuthUserId string
	UserId     string
	UserEmail  string
}

var customContextKey = "CUSTOM_CONTEXT"

func WithContext(customContext *CustomContext, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestWithCtx := r.WithContext(context.WithValue(r.Context(), customContextKey, customContext))
		next.ServeHTTP(w, requestWithCtx)
	})
}

func WithCustomContext(ctx context.Context, customContext *CustomContext) context.Context {
	return context.WithValue(ctx, customContextKey, customContext)
}

func WithCustomContextFromGinRequest(c *gin.Context, appSource string) context.Context {
	// Get tenant from headers with case-insensitive checks
	tenant := ""
	for _, header := range TenantHeaders {
		if value := c.GetHeader(header); value != "" {
			tenant = value
			break
		}
	}

	customContext := &CustomContext{
		AppSource:  appSource,
		AuthUserId: c.GetString("AuthenticatedUserId"),
		Tenant:     tenant,
		UserId:     c.GetString("UserId"),
		UserEmail:  c.GetString("UserEmail"),
	}
	return WithCustomContext(c.Request.Context(), customContext)
}

func GetContext(ctx context.Context) *CustomContext {
	customContext, ok := ctx.Value(customContextKey).(*CustomContext)
	if !ok {
		return new(CustomContext)
	}
	return customContext
}

func GetAppSourceFromContext(ctx context.Context) string {
	return GetContext(ctx).AppSource
}

func GetTenantFromContext(ctx context.Context) string {
	return GetContext(ctx).Tenant
}

func GetAuthUserIdFromContext(ctx context.Context) string {
	return GetContext(ctx).AuthUserId
}

func GetUserIdFromContext(ctx context.Context) string {
	return GetContext(ctx).UserId
}

func GetUserEmailFromContext(ctx context.Context) string {
	return GetContext(ctx).UserEmail
}

func SetAppSourceInContext(ctx context.Context, appSource string) context.Context {
	customContext := GetContext(ctx)
	customContext.AppSource = appSource
	return WithCustomContext(ctx, customContext)
}

func SetUserIdInContext(ctx context.Context, userId string) context.Context {
	customContext := GetContext(ctx)
	customContext.UserId = userId
	return WithCustomContext(ctx, customContext)
}

func SetTenantInContext(ctx context.Context, tenant string) context.Context {
	customContext := GetContext(ctx)
	customContext.Tenant = tenant
	return WithCustomContext(ctx, customContext)
}

func ValidateTenant(ctx context.Context) error {
	if GetTenantFromContext(ctx) == "" {
		return er.ErrTenantMissing
	}
	return nil
}

// WithTenantContext creates a new context with the specified tenant
func WithTenantContext(ctx context.Context, tenant string) context.Context {
	return WithCustomContext(ctx, &CustomContext{
		Tenant: tenant,
	})
}

package httpapi

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
)

type Identity struct {
	UserID         string
	OrganizationID string
	MembershipID   string
	EmployeeID     string
	Role           string
	Permissions    map[string]bool
}

type identityContextKey struct{}

func identityFrom(c *gin.Context) Identity {
	value, _ := c.Get("identity")
	identity, _ := value.(Identity)
	return identity
}

func IdentityFromContext(ctx context.Context) (Identity, bool) {
	identity, ok := ctx.Value(identityContextKey{}).(Identity)
	return identity, ok
}

func permissionRequired(permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		identity := identityFrom(c)
		if identity.Role != "hr_admin" && !identity.Permissions[permission] {
			problem(c, http.StatusForbidden, "permission_denied", "Akses ditolak", "Anda tidak memiliki izin "+permission, nil)
			c.Abort()
			return
		}
		c.Next()
	}
}

func (s *Server) authRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := bearerToken(c.GetHeader("Authorization"))
		if raw == "" {
			problem(c, http.StatusUnauthorized, "authentication_required", "Autentikasi diperlukan", "Access token tidak ditemukan.", nil)
			c.Abort()
			return
		}
		claims, err := s.tokens.Parse(raw, "access")
		if err != nil {
			problem(c, http.StatusUnauthorized, "invalid_token", "Sesi tidak valid", "Silakan masuk kembali.", nil)
			c.Abort()
			return
		}
		identity := Identity{
			UserID: claims.UserID, OrganizationID: claims.OrganizationID, MembershipID: claims.MembershipID,
			Role: claims.Role, Permissions: map[string]bool{},
		}
		for _, permission := range claims.Permissions {
			identity.Permissions[permission] = true
		}
		var employeeID *string
		if err := s.db.Raw("SELECT employee_id FROM memberships WHERE id = ? AND user_id = ? AND organization_id = ?", claims.MembershipID, claims.UserID, claims.OrganizationID).Scan(&employeeID).Error; err == nil && employeeID != nil {
			identity.EmployeeID = *employeeID
		}
		c.Set("identity", identity)
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), identityContextKey{}, identity))
		c.Next()
	}
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
		return ""
	}
	return header[len(prefix):]
}

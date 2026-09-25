package httpv1

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/emount4/poidem-back/internal/account"
	"github.com/emount4/poidem-back/internal/api/apierr"
	"github.com/emount4/poidem-back/internal/api/pagination"
	"github.com/gin-gonic/gin"
)

type Admin interface {
	ListUsers(context.Context, string, account.AdminPage) ([]account.Profile, int64, error)
	GetUser(context.Context, int64) (account.Profile, error)
	SetUserStatus(context.Context, int64, int64, string) (account.Profile, error)
	Dashboard(context.Context) (account.AdminDashboard, error)
}

func RegisterAdminRoutes(routes *gin.RouterGroup, service Admin, authenticator Authenticator) {
	h := adminHandler{admin: service}
	admin := routes.Group("/admin")
	admin.Use(RequireAuthentication(authenticator), RequireAdmin())
	admin.GET("/dashboard", h.dashboard)
	admin.GET("/users", h.listUsers)
	admin.GET("/users/:userId", h.getUser)
	admin.POST("/users/:userId/:action", h.setStatus)
}

type adminHandler struct{ admin Admin }

func (h adminHandler) dashboard(c *gin.Context) {
	result, err := h.admin.Dashboard(c.Request.Context())
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"usersTotal": result.UsersTotal, "activeEvents": result.ActiveEvents,
		"pendingReports": result.PendingReports, "newRegistrations30d": result.NewRegistrations30d,
	})
}

func (h adminHandler) listUsers(c *gin.Context) {
	page, fields := pagination.Parse(c.Request.URL.Query())
	search := ""
	if raw, ok := c.Request.URL.Query()["search"]; ok {
		if len(raw) != 1 {
			fields["search"] = []string{"Параметр должен быть указан один раз"}
		} else {
			search = raw[0]
		}
	}
	if len(fields) > 0 {
		apierr.WriteValidation(c, fields)
		return
	}
	items, total, err := h.admin.ListUsers(c.Request.Context(), search, account.AdminPage{Offset: page.Offset(), Limit: page.Limit})
	if h.writeError(c, err) {
		return
	}
	responses := make([]profileResponse, 0, len(items))
	for _, item := range items {
		responses = append(responses, newProfileResponse(item))
	}
	c.JSON(http.StatusOK, pagination.NewResponse(responses, page, total))
}

func (h adminHandler) getUser(c *gin.Context) {
	userID, ok := adminUserID(c)
	if !ok {
		return
	}
	item, err := h.admin.GetUser(c.Request.Context(), userID)
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusOK, newProfileResponse(item))
}

func (h adminHandler) setStatus(c *gin.Context) {
	userID, ok := adminUserID(c)
	if !ok {
		return
	}
	status := ""
	switch c.Param("action") {
	case "ban":
		status = account.StatusBanned
	case "unban":
		status = account.StatusActive
	default:
		apierr.Write(c, http.StatusNotFound, "NOT_FOUND", "Маршрут не найден", nil)
		return
	}
	principal, _ := account.PrincipalFromContext(c.Request.Context())
	item, err := h.admin.SetUserStatus(c.Request.Context(), principal.UserID, userID, status)
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusOK, newProfileResponse(item))
}

func (h adminHandler) writeError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, account.ErrUserNotFound):
		apierr.Write(c, http.StatusNotFound, "USER_NOT_FOUND", "Пользователь не найден", nil)
	case errors.Is(err, account.ErrCannotChangeOwnStatus):
		apierr.Write(c, http.StatusConflict, "CANNOT_CHANGE_OWN_STATUS", "Администратор не может заблокировать сам себя", nil)
	case errors.Is(err, account.ErrCannotBanAdmin):
		apierr.Write(c, http.StatusConflict, "CANNOT_BAN_ADMIN", "Нельзя заблокировать администратора", nil)
	default:
		apierr.WriteInternal(c, err)
	}
	return true
}

func adminUserID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("userId"), 10, 64)
	if err != nil || id <= 0 {
		apierr.Write(c, http.StatusNotFound, "USER_NOT_FOUND", "Пользователь не найден", nil)
		return 0, false
	}
	return id, true
}

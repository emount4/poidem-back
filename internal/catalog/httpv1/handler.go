// Package httpv1 exposes catalog endpoints in API v1.
package httpv1

import (
	"context"
	"net/http"

	"github.com/emount4/poidem-back/internal/api/apierr"
	"github.com/emount4/poidem-back/internal/catalog"
	"github.com/gin-gonic/gin"
)

type Catalog interface {
	Cities(context.Context) ([]catalog.Item, error)
	Interests(context.Context) ([]catalog.Item, error)
	EventCategories(context.Context) ([]catalog.Item, error)
}

type dictionaryHandler struct {
	catalog Catalog
}

type dictionaryItemResponse struct {
	ID   int64   `json:"id"`
	Name string  `json:"name"`
	Slug *string `json:"slug"`
}

func RegisterRoutes(routes *gin.RouterGroup, catalog Catalog) {
	handler := dictionaryHandler{catalog: catalog}
	routes.GET("/cities", handler.cities)
	routes.GET("/interests", handler.interests)
	routes.GET("/event-categories", handler.eventCategories)
}

func (h dictionaryHandler) cities(c *gin.Context) {
	h.list(c, h.catalog.Cities)
}

func (h dictionaryHandler) interests(c *gin.Context) {
	h.list(c, h.catalog.Interests)
}

func (h dictionaryHandler) eventCategories(c *gin.Context) {
	h.list(c, h.catalog.EventCategories)
}

func (h dictionaryHandler) list(c *gin.Context, load func(context.Context) ([]catalog.Item, error)) {
	items, err := load(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		apierr.Write(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Внутренняя ошибка сервера", nil)
		return
	}
	response := make([]dictionaryItemResponse, len(items))
	for i, item := range items {
		response[i] = dictionaryItemResponse{ID: item.ID, Name: item.Name, Slug: item.Slug}
	}
	c.JSON(http.StatusOK, response)
}

package httpv1

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/emount4/poidem-back/internal/account"
	"github.com/emount4/poidem-back/internal/api/apierr"
	"github.com/gin-gonic/gin"
)

const maxProfilePatchBytes = 64 << 10

type Profiles interface {
	Get(context.Context, int64) (account.Profile, error)
	Update(context.Context, int64, account.ProfilePatch) (account.Profile, error)
}

func RegisterProfileRoutes(routes *gin.RouterGroup, profiles Profiles, authenticator Authenticator) {
	h := profileHandler{profiles: profiles}
	protected := routes.Group("")
	protected.Use(RequireAuthentication(authenticator))
	protected.GET("/auth/me", h.get)
	protected.GET("/users/me", h.get)
	protected.GET("/users/:userId", h.getPublic)
	protected.PATCH("/users/me", h.update)
}

func (h profileHandler) getPublic(c *gin.Context) {
	userID, err := strconv.ParseInt(c.Param("userId"), 10, 64)
	if err != nil || userID <= 0 {
		apierr.Write(c, http.StatusNotFound, "USER_NOT_FOUND", "Пользователь не найден", nil)
		return
	}
	profile, err := h.profiles.Get(c.Request.Context(), userID)
	if errors.Is(err, account.ErrUserNotFound) {
		apierr.Write(c, http.StatusNotFound, "USER_NOT_FOUND", "Пользователь не найден", nil)
		return
	}
	if err != nil {
		apierr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, newProfileResponse(profile))
}

type profileHandler struct{ profiles Profiles }

func (h profileHandler) get(c *gin.Context) {
	principal, ok := account.PrincipalFromContext(c.Request.Context())
	if !ok {
		apierr.Write(c, http.StatusUnauthorized, "UNAUTHORIZED", "Требуется авторизация", nil)
		return
	}
	profile, err := h.profiles.Get(c.Request.Context(), principal.UserID)
	if err != nil {
		apierr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, newProfileResponse(profile))
}

func (h profileHandler) update(c *gin.Context) {
	principal, ok := account.PrincipalFromContext(c.Request.Context())
	if !ok {
		apierr.Write(c, http.StatusUnauthorized, "UNAUTHORIZED", "Требуется авторизация", nil)
		return
	}
	request, fields := decodeProfilePatch(c)
	if len(fields) > 0 {
		apierr.WriteValidation(c, fields)
		return
	}
	profile, err := h.profiles.Update(c.Request.Context(), principal.UserID, request.patch())
	var validation *account.ValidationError
	switch {
	case errors.As(err, &validation):
		apierr.WriteValidation(c, validation.Fields)
		return
	case errors.Is(err, account.ErrCityRequired):
		apierr.WriteValidation(c, apierr.FieldErrors{"cityId": {"Город нельзя очистить после завершения профиля"}})
		return
	case errors.Is(err, account.ErrCityNotFound):
		apierr.WriteValidation(c, apierr.FieldErrors{"cityId": {"Город не найден"}})
		return
	case errors.Is(err, account.ErrInterestsInvalid):
		apierr.WriteValidation(c, apierr.FieldErrors{"interestIds": {"Один или несколько интересов не найдены"}})
		return
	case err != nil:
		apierr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, newProfileResponse(profile))
}

type patchField[T any] struct {
	Present bool
	Null    bool
	Value   T
}

func (f *patchField[T]) UnmarshalJSON(data []byte) error {
	f.Present = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		f.Null = true
		return nil
	}
	return json.Unmarshal(data, &f.Value)
}

type profilePatchRequest struct {
	FirstName   patchField[string]  `json:"firstName"`
	LastName    patchField[string]  `json:"lastName"`
	CityID      patchField[int64]   `json:"cityId"`
	About       patchField[string]  `json:"about"`
	Gender      patchField[string]  `json:"gender"`
	BirthDate   patchField[string]  `json:"birthDate"`
	InterestIDs patchField[[]int64] `json:"interestIds"`
}

func decodeProfilePatch(c *gin.Context) (profilePatchRequest, apierr.FieldErrors) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxProfilePatchBytes)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var request profilePatchRequest
	if err := decoder.Decode(&request); err != nil {
		return profilePatchRequest{}, apierr.FieldErrors{"body": {"Некорректный JSON"}}
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return profilePatchRequest{}, apierr.FieldErrors{"body": {"Ожидается один JSON-объект"}}
	}
	fields := make(apierr.FieldErrors)
	if request.FirstName.Present && request.FirstName.Null {
		fields["firstName"] = []string{"Поле не может быть null"}
	}
	if request.InterestIDs.Present && request.InterestIDs.Null {
		fields["interestIds"] = []string{"Поле не может быть null"}
	}
	if request.BirthDate.Present && !request.BirthDate.Null {
		if _, err := time.Parse(time.DateOnly, request.BirthDate.Value); err != nil {
			fields["birthDate"] = []string{"Ожидается дата в формате YYYY-MM-DD"}
		}
	}
	return request, fields
}

func (r profilePatchRequest) patch() account.ProfilePatch {
	var birthDate time.Time
	if r.BirthDate.Present && !r.BirthDate.Null {
		birthDate, _ = time.Parse(time.DateOnly, r.BirthDate.Value)
	}
	return account.ProfilePatch{
		FirstName:   account.Change[string]{Set: r.FirstName.Present, Value: r.FirstName.Value},
		LastName:    account.NullableChange[string]{Set: r.LastName.Present, Null: r.LastName.Null, Value: r.LastName.Value},
		CityID:      account.NullableChange[int64]{Set: r.CityID.Present, Null: r.CityID.Null, Value: r.CityID.Value},
		About:       account.NullableChange[string]{Set: r.About.Present, Null: r.About.Null, Value: r.About.Value},
		Gender:      account.NullableChange[string]{Set: r.Gender.Present, Null: r.Gender.Null, Value: r.Gender.Value},
		BirthDate:   account.NullableChange[time.Time]{Set: r.BirthDate.Present, Null: r.BirthDate.Null, Value: birthDate},
		InterestIDs: account.Change[[]int64]{Set: r.InterestIDs.Present, Value: r.InterestIDs.Value},
	}
}

type dictionaryItemResponse struct {
	ID   int64   `json:"id"`
	Name string  `json:"name"`
	Slug *string `json:"slug"`
}

type profileResponse struct {
	ID                int64                    `json:"id"`
	FirstName         string                   `json:"firstName"`
	LastName          *string                  `json:"lastName"`
	AvatarURL         *string                  `json:"avatarUrl"`
	City              *dictionaryItemResponse  `json:"city"`
	About             *string                  `json:"about"`
	Gender            *string                  `json:"gender"`
	BirthDate         *string                  `json:"birthDate"`
	Interests         []dictionaryItemResponse `json:"interests"`
	Role              string                   `json:"role"`
	Status            string                   `json:"status"`
	IsProfileComplete bool                     `json:"isProfileComplete"`
	CreatedAt         time.Time                `json:"createdAt"`
}

func newProfileResponse(profile account.Profile) profileResponse {
	response := profileResponse{
		ID: profile.ID, FirstName: profile.FirstName, LastName: profile.LastName,
		AvatarURL: profile.AvatarURL, About: profile.About, Role: profile.Role,
		Gender: profile.Gender,
		Status: profile.Status, IsProfileComplete: profile.IsComplete(), CreatedAt: profile.CreatedAt,
		Interests: make([]dictionaryItemResponse, 0, len(profile.Interests)),
	}
	if profile.BirthDate != nil {
		value := profile.BirthDate.Format(time.DateOnly)
		response.BirthDate = &value
	}
	if profile.City != nil {
		response.City = &dictionaryItemResponse{ID: profile.City.ID, Name: profile.City.Name, Slug: profile.City.Slug}
	}
	for _, interest := range profile.Interests {
		response.Interests = append(response.Interests, dictionaryItemResponse{ID: interest.ID, Name: interest.Name, Slug: interest.Slug})
	}
	return response
}

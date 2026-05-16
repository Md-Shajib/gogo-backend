package response

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/md-shajib/gogo-backend/pkg/apperr"
)

type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data"`
	Error   *errorBody  `json:"error"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Meta holds pagination metadata.
type Meta struct {
	Page       int `json:"page"`
	Limit      int `json:"limit"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

type paginatedEnvelope struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data"`
	Meta    Meta        `json:"meta"`
	Error   *errorBody  `json:"error"`
}

// Paginated writes a successful JSON response with pagination metadata.
func Paginated(w http.ResponseWriter, status int, data interface{}, meta Meta) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(paginatedEnvelope{
		Success: true,
		Data:    data,
		Meta:    meta,
		Error:   nil,
	})
}

// JSON writes a successful JSON response.
func JSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(Response{
		Success: true,
		Data:    data,
		Error:   nil,
	})
}

// Error writes a JSON error response.
// Maps *apperr.AppError to the correct HTTP status.
// Unexpected errors respond with 500 and a generic message — raw error never reaches the client.
func Error(w http.ResponseWriter, err error) {
	var status int
	var body errorBody

	appErr, ok := err.(*apperr.AppError)
	if ok {
		status = appErr.HTTPStatus
		body = errorBody{
			Code:    appErr.Code,
			Message: appErr.Message,
		}
	} else {
		slog.Error("unexpected error", "error", err)
		status = http.StatusInternalServerError
		body = errorBody{
			Code:    apperr.ErrInternal.Code,
			Message: apperr.ErrInternal.Message,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(Response{
		Success: false,
		Data:    nil,
		Error:   &body,
	})
}

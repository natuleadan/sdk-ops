package core

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/natuleadan/sdk-ops/internal/modules/math"
)

// Version defines the baseline service version.
const Version = "0.1.0"

// Greet returns a normalized greeting for the given name.
func Greet(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		trimmed = "world"
	}
	return "hello, " + trimmed
}

// NewMux builds the HTTP handler for health, greet, and modular math endpoints.
func NewMux() *http.ServeMux {
	mux := http.NewServeMux()
	mathModule := math.NewModule()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /greet", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		writeJSON(w, http.StatusOK, map[string]string{"message": Greet(name)})
	})
	mux.HandleFunc("GET /math/add", func(w http.ResponseWriter, r *http.Request) {
		a, errA := strconv.ParseInt(r.URL.Query().Get("a"), 10, 64)
		b, errB := strconv.ParseInt(r.URL.Query().Get("b"), 10, 64)
		if errA != nil || errB != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid integer operand"})
			return
		}
		res, err := mathModule.Controller.HandleAdd(a, b)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, res)
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

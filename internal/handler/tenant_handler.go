package handler

import (
	"encoding/json"
	"net/http"

	"github.com/ClareAI/astra-voice-service/internal/domain"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"github.com/ClareAI/astra-voice-service/internal/repository"
	"github.com/gorilla/mux"
)

// TenantHandler handles HTTP requests for voice tenants
type TenantHandler struct {
	tenantRepo repository.VoiceTenantRepository
}

// NewTenantHandler creates a new tenant handler
func NewTenantHandler(tenantRepo repository.VoiceTenantRepository) *TenantHandler {
	return &TenantHandler{
		tenantRepo: tenantRepo,
	}
}

// CreateTenant godoc
// @Summary Create a new tenant
// @Description Create a new voice tenant with the specified configuration
// @Tags tenants
// @Accept json
// @Produce json
// @Param tenant body domain.CreateVoiceTenantRequest true "Tenant creation request"
// @Success 201 {object} domain.VoiceTenant "Tenant created successfully"
// @Failure 400 {object} map[string]string "Invalid request body"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/tenants [post]
func (h *TenantHandler) CreateTenant(w http.ResponseWriter, r *http.Request) {
	var req domain.CreateVoiceTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	tenant, err := h.tenantRepo.Create(r.Context(), &req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(tenant)
}

// GetTenant godoc
// @Summary Get tenant by ID
// @Description Retrieve a specific voice tenant by its unique identifier
// @Tags tenants
// @Accept json
// @Produce json
// @Param id path string true "Tenant ID (UUID)" format(uuid)
// @Success 200 {object} domain.VoiceTenant "Tenant found"
// @Failure 404 {object} map[string]string "Tenant not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/tenants/{id} [get]
func (h *TenantHandler) GetTenant(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	tenant, err := h.tenantRepo.GetByID(r.Context(), id)
	if err != nil {
		if err.Error() == "voice tenant not found: "+id {
			http.Error(w, "Tenant not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tenant)
}

// GetTenantByTenantID godoc
// @Summary Get tenant by tenant ID
// @Description Retrieve a voice tenant by its tenant_id field
// @Tags tenants
// @Accept json
// @Produce json
// @Param tenant_id path string true "Business tenant ID"
// @Success 200 {object} domain.VoiceTenant "Tenant found"
// @Failure 404 {object} map[string]string "Tenant not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/tenants/by-tenant-id/{tenant_id} [get]
func (h *TenantHandler) GetTenantByTenantID(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	tenantID := vars["tenant_id"]

	tenant, err := h.tenantRepo.GetByTenantID(r.Context(), tenantID)
	if err != nil {
		if err.Error() == "voice tenant not found with tenant ID: "+tenantID {
			http.Error(w, "Tenant not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tenant)
}

// GetTenantByAstraKey godoc
// @Summary Get tenant by astra key
// @Description Retrieve a voice tenant by its astra_key field
// @Tags tenants
// @Accept json
// @Produce json
// @Param astra_key path string true "Astra API key"
// @Success 200 {object} domain.VoiceTenant "Tenant found"
// @Failure 404 {object} map[string]string "Tenant not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/tenants/by-astra-key/{astra_key} [get]
func (h *TenantHandler) GetTenantByAstraKey(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	astraKey := vars["astra_key"]

	tenant, err := h.tenantRepo.GetByAstraKey(r.Context(), astraKey)
	if err != nil {
		if err.Error() == "voice tenant not found with astra key: "+astraKey {
			http.Error(w, "Tenant not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tenant)
}

// GetTenants godoc
// @Summary List all tenants
// @Description Retrieve a list of all voice tenants
// @Tags tenants
// @Accept json
// @Produce json
// @Param include_disabled query boolean false "Include disabled tenants" default(false)
// @Success 200 {array} domain.VoiceTenant "List of tenants"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/tenants [get]
func (h *TenantHandler) GetTenants(w http.ResponseWriter, r *http.Request) {
	includeDisabledStr := r.URL.Query().Get("include_disabled")
	includeDisabled := includeDisabledStr == "true"

	tenants, err := h.tenantRepo.GetAll(r.Context(), includeDisabled)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tenants)
}

// GetTenantWithAgents godoc
// @Summary Get tenant with its agents
// @Description Retrieve a tenant along with all its associated voice agents
// @Tags tenants
// @Accept json
// @Produce json
// @Param id path string true "Tenant ID (UUID)" format(uuid)
// @Success 200 {object} domain.VoiceTenant "Tenant with agents"
// @Failure 404 {object} map[string]string "Tenant not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/tenants/{id}/with-agents [get]
func (h *TenantHandler) GetTenantWithAgents(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	tenantWithAgents, err := h.tenantRepo.GetWithAgents(r.Context(), id)
	if err != nil {
		if err.Error() == "voice tenant not found: "+id {
			http.Error(w, "Tenant not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tenantWithAgents)
}

// UpdateTenant godoc
// @Summary Update an existing tenant
// @Description Update an existing voice tenant's configuration
// @Tags tenants
// @Accept json
// @Produce json
// @Param id path string true "Tenant ID (UUID)" format(uuid)
// @Param tenant body domain.UpdateVoiceTenantRequest true "Tenant update request"
// @Success 200 {object} domain.VoiceTenant "Tenant updated successfully"
// @Failure 400 {object} map[string]string "Invalid request body"
// @Failure 404 {object} map[string]string "Tenant not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/tenants/{id} [put]
func (h *TenantHandler) UpdateTenant(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var req domain.UpdateVoiceTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	tenant, err := h.tenantRepo.Update(r.Context(), id, &req)
	if err != nil {
		if err.Error() == "voice tenant not found: "+id {
			http.Error(w, "Tenant not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tenant)
}

// DeleteTenant godoc
// @Summary Delete a tenant
// @Description Delete a voice tenant by its ID (soft delete)
// @Tags tenants
// @Accept json
// @Produce json
// @Param id path string true "Tenant ID (UUID)" format(uuid)
// @Success 204 "Tenant deleted successfully"
// @Failure 404 {object} map[string]string "Tenant not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/tenants/{id} [delete]
func (h *TenantHandler) DeleteTenant(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	err := h.tenantRepo.Delete(r.Context(), id)
	if err != nil {
		if err.Error() == "voice tenant not found: "+id {
			http.Error(w, "Tenant not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// CheckTenantExists godoc
// @Summary Check if tenant exists
// @Description Check whether a voice tenant with the specified ID exists
// @Tags tenants
// @Accept json
// @Produce json
// @Param id path string true "Tenant ID (UUID)" format(uuid)
// @Success 200 "Tenant exists"
// @Failure 404 "Tenant not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/tenants/{id} [head]
func (h *TenantHandler) CheckTenantExists(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	exists, err := h.tenantRepo.Exists(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if exists {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusNotFound)
	}
}

// CheckTenantExistsByTenantID godoc
// @Summary Check if tenant exists by tenant ID
// @Description Check whether a voice tenant with the specified tenant_id exists
// @Tags tenants
// @Accept json
// @Produce json
// @Param tenant_id path string true "Business tenant ID"
// @Success 200 "Tenant exists"
// @Failure 404 "Tenant not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/tenants/by-tenant-id/{tenant_id} [head]
func (h *TenantHandler) CheckTenantExistsByTenantID(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	tenantID := vars["tenant_id"]

	exists, err := h.tenantRepo.ExistsByTenantID(r.Context(), tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if exists {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusNotFound)
	}
}

// CheckTenantExistsByAstraKey godoc
// @Summary Check if tenant exists by astra key
// @Description Check whether a voice tenant with the specified astra_key exists
// @Tags tenants
// @Accept json
// @Produce json
// @Param astra_key path string true "Astra API key"
// @Success 200 "Tenant exists"
// @Failure 404 "Tenant not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/tenants/by-astra-key/{astra_key} [head]
func (h *TenantHandler) CheckTenantExistsByAstraKey(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	astraKey := vars["astra_key"]

	exists, err := h.tenantRepo.ExistsByAstraKey(r.Context(), astraKey)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if exists {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusNotFound)
	}
}

// SetupTenantRoutes sets up all tenant-related routes
func (h *TenantHandler) SetupTenantRoutes(router *mux.Router) {
	// Tenant CRUD routes
	router.HandleFunc("/tenants", h.CreateTenant).Methods("POST")
	router.HandleFunc("/tenants", h.GetTenants).Methods("GET")
	router.HandleFunc("/tenants/{id}", h.GetTenant).Methods("GET")
	router.HandleFunc("/tenants/{id}", h.UpdateTenant).Methods("PUT")
	router.HandleFunc("/tenants/{id}", h.DeleteTenant).Methods("DELETE")
	router.HandleFunc("/tenants/{id}", h.CheckTenantExists).Methods("HEAD")

	// Extended tenant routes
	router.HandleFunc("/tenants/{id}/with-agents", h.GetTenantWithAgents).Methods("GET")
	router.HandleFunc("/tenants/by-tenant-id/{tenant_id}", h.GetTenantByTenantID).Methods("GET")
	router.HandleFunc("/tenants/by-tenant-id/{tenant_id}", h.CheckTenantExistsByTenantID).Methods("HEAD")
	router.HandleFunc("/tenants/by-astra-key/{astra_key}", h.GetTenantByAstraKey).Methods("GET")
	router.HandleFunc("/tenants/by-astra-key/{astra_key}", h.CheckTenantExistsByAstraKey).Methods("HEAD")

	logger.Base().Info("Tenant routes registered")
}

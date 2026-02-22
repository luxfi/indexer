// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/luxfi/indexer/evm/account"
)

// AccountAPI provides HTTP handlers for user account endpoints.
// These endpoints require IAM authentication.
type AccountAPI struct {
	svc *account.Service
}

// NewAccountAPI creates account API handlers backed by the given service.
func NewAccountAPI(svc *account.Service) *AccountAPI {
	return &AccountAPI{svc: svc}
}

// HandleGetUserInfo returns the authenticated user's profile.
func (a *AccountAPI) HandleGetUserInfo(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	writeJSONOK(w, user)
}

// HandleGetCSRF returns a CSRF token header for the authenticated session.
func (a *AccountAPI) HandleGetCSRF(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	// For JWT-based auth, CSRF is not strictly needed since the token is
	// passed via Authorization header. We return a static acknowledgment
	// for frontend compatibility.
	w.Header().Set("x-bs-account-csrf", "jwt-authenticated")
	writeJSONOK(w, map[string]string{"status": "ok"})
}

// HandleListAPIKeys returns the user's API keys.
func (a *AccountAPI) HandleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	keys, err := a.svc.GetUserAPIKeys(r.Context(), user.ID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to list API keys")
		return
	}
	writeJSONOK(w, keys)
}

// HandleCreateAPIKey creates a new API key for the user.
func (a *AccountAPI) HandleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		req.Name = "default"
	}

	key, err := a.svc.CreateAPIKey(r.Context(), user.ID, req.Name)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSONStatus(w, http.StatusCreated, key)
}

// HandleDeleteAPIKey deletes one of the user's API keys.
func (a *AccountAPI) HandleDeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	keyID := mux.Vars(r)["id"]
	if keyID == "" {
		writeJSONError(w, http.StatusBadRequest, "missing key id")
		return
	}

	if err := a.svc.DeleteAPIKey(r.Context(), user.ID, keyID); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSONOK(w, map[string]string{"status": "deleted"})
}

// HandleListWatchlists returns the user's watchlists.
func (a *AccountAPI) HandleListWatchlists(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	watchlists, err := a.svc.GetUserWatchlists(r.Context(), user.ID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to list watchlists")
		return
	}
	writeJSONOK(w, watchlists)
}

// HandleGetWatchlistAddresses returns addresses in a watchlist.
func (a *AccountAPI) HandleGetWatchlistAddresses(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	watchlistIDStr := mux.Vars(r)["id"]
	watchlistID, err := strconv.ParseInt(watchlistIDStr, 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid watchlist id")
		return
	}

	// Verify ownership
	watchlist, err := a.svc.GetWatchlist(r.Context(), watchlistID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "watchlist not found")
		return
	}
	if watchlist.UserID != user.ID {
		writeJSONError(w, http.StatusForbidden, "access denied")
		return
	}

	addrs, err := a.svc.GetWatchlistAddresses(r.Context(), watchlistID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to list addresses")
		return
	}
	writeJSONOK(w, addrs)
}

// HandleAddWatchlistAddress adds an address to a watchlist.
func (a *AccountAPI) HandleAddWatchlistAddress(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	watchlistIDStr := mux.Vars(r)["id"]
	watchlistID, err := strconv.ParseInt(watchlistIDStr, 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid watchlist id")
		return
	}

	// Verify ownership
	watchlist, err := a.svc.GetWatchlist(r.Context(), watchlistID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "watchlist not found")
		return
	}
	if watchlist.UserID != user.ID {
		writeJSONError(w, http.StatusForbidden, "access denied")
		return
	}

	var addr account.WatchlistAddress
	if err := json.NewDecoder(r.Body).Decode(&addr); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	addr.WatchlistID = watchlistID

	if err := a.svc.AddWatchlistAddress(r.Context(), &addr); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSONStatus(w, http.StatusCreated, addr)
}

// HandleRemoveWatchlistAddress removes an address from a watchlist.
func (a *AccountAPI) HandleRemoveWatchlistAddress(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	vars := mux.Vars(r)
	watchlistIDStr := vars["id"]
	addressHash := vars["address_hash"]

	watchlistID, err := strconv.ParseInt(watchlistIDStr, 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid watchlist id")
		return
	}

	// Verify ownership
	watchlist, err := a.svc.GetWatchlist(r.Context(), watchlistID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "watchlist not found")
		return
	}
	if watchlist.UserID != user.ID {
		writeJSONError(w, http.StatusForbidden, "access denied")
		return
	}

	if err := a.svc.RemoveWatchlistAddress(r.Context(), watchlistID, addressHash); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSONOK(w, map[string]string{"status": "deleted"})
}

// HandleListAddressTags returns the user's custom address tags.
func (a *AccountAPI) HandleListAddressTags(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	tags, err := a.svc.GetUserAddressTags(r.Context(), user.ID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to list tags")
		return
	}
	writeJSONOK(w, tags)
}

// HandleSetAddressTag creates or updates an address tag.
func (a *AccountAPI) HandleSetAddressTag(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var req struct {
		AddressHash string `json:"address_hash"`
		Tag         string `json:"tag"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := a.svc.SetAddressTag(r.Context(), user.ID, req.AddressHash, req.Tag); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSONOK(w, map[string]string{"status": "ok"})
}

// HandleDeleteAddressTag removes a custom address tag.
func (a *AccountAPI) HandleDeleteAddressTag(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	addressHash := mux.Vars(r)["id"]
	if addressHash == "" {
		writeJSONError(w, http.StatusBadRequest, "missing address hash")
		return
	}

	if err := a.svc.DeleteAddressTag(r.Context(), user.ID, addressHash); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSONOK(w, map[string]string{"status": "deleted"})
}

// HandleListCustomABIs returns the user's custom ABIs.
func (a *AccountAPI) HandleListCustomABIs(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	abis, err := a.svc.GetUserCustomABIs(r.Context(), user.ID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to list custom ABIs")
		return
	}
	writeJSONOK(w, abis)
}

// HandleSetCustomABI creates or updates a custom ABI.
func (a *AccountAPI) HandleSetCustomABI(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var req struct {
		ContractHash string `json:"contract_hash"`
		Name         string `json:"name"`
		ABI          string `json:"abi"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := a.svc.SetCustomABI(r.Context(), user.ID, req.ContractHash, req.Name, req.ABI); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSONOK(w, map[string]string{"status": "ok"})
}

// HandleDeleteCustomABI removes a custom ABI.
func (a *AccountAPI) HandleDeleteCustomABI(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	contractHash := mux.Vars(r)["id"]
	if contractHash == "" {
		writeJSONError(w, http.StatusBadRequest, "missing contract hash")
		return
	}

	if err := a.svc.DeleteCustomABI(r.Context(), user.ID, contractHash); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSONOK(w, map[string]string{"status": "deleted"})
}

// HandleLogout is a no-op for JWT auth but provided for frontend compatibility.
func (a *AccountAPI) HandleLogout(w http.ResponseWriter, r *http.Request) {
	writeJSONOK(w, map[string]string{"status": "ok"})
}

// writeJSONOK writes a JSON response with status 200.
func writeJSONOK(w http.ResponseWriter, data interface{}) {
	writeJSONStatus(w, http.StatusOK, data)
}

// writeJSONStatus writes a JSON response with the given status code.
func writeJSONStatus(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

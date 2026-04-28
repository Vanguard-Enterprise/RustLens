package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"rustlens/backend/internal/outboundhttp"
	"rustlens/backend/internal/store"
)

type ownerProxyRequest struct {
	Endpoint string `json:"endpoint"`
}

func ownerProxyToMap(proxy store.OutboundProxy) map[string]any {
	return map[string]any{
		"id":       proxy.ID,
		"endpoint": proxy.RedactedEndpoint,
		"enabled":  proxy.Enabled,
	}
}

func validateProxyEndpoint(raw string) (string, string, error) {
	endpoint := strings.TrimSpace(raw)
	if endpoint == "" {
		return "", "", errors.New("proxy endpoint is required")
	}

	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", "", errors.New("proxy endpoint must be a valid URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", "", errors.New("proxy endpoint scheme must be http or https")
	}
	if parsed.Host == "" {
		return "", "", errors.New("proxy endpoint must include host and port")
	}
	if parsed.Port() == "" {
		return "", "", errors.New("proxy endpoint must include host and port")
	}
	if parsed.User == nil {
		return "", "", errors.New("proxy endpoint must include username and password")
	}
	if parsed.User.Username() == "" {
		return "", "", errors.New("proxy endpoint must include username")
	}
	if _, ok := parsed.User.Password(); !ok {
		return "", "", errors.New("proxy endpoint must include password")
	}

	return parsed.String(), outboundhttp.RedactProxyEndpoint(parsed.String()), nil
}

func (h *Handler) decryptedEnabledProxyEndpoints(ctx context.Context) ([]string, error) {
	proxies, err := h.postgres.ListEnabledOutboundProxies(ctx)
	if err != nil {
		return nil, err
	}
	endpoints := make([]string, 0, len(proxies))
	for _, proxy := range proxies {
		endpoint, err := h.proxyCrypto.Decrypt(proxy.Ciphertext, proxy.Nonce)
		if err != nil {
			return nil, err
		}
		endpoints = append(endpoints, endpoint)
	}
	return endpoints, nil
}

func (h *Handler) reloadOutboundProvider(ctx context.Context) error {
	if h.outboundProvider == nil {
		return nil
	}
	endpoints, err := h.decryptedEnabledProxyEndpoints(ctx)
	if err != nil {
		return err
	}
	if len(endpoints) == 0 {
		return h.outboundProvider.Replace(h.cfg.ProxyEnabled, h.cfg.ProxyEndpoints)
	}
	return h.outboundProvider.Replace(true, endpoints)
}

func (h *Handler) ListOwnerProxies(w http.ResponseWriter, r *http.Request) {
	proxies, err := h.postgres.ListOutboundProxies(r.Context())
	if err != nil {
		log.Printf("[backend] ListOwnerProxies error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to list proxies")
		return
	}

	items := make([]map[string]any, 0, len(proxies))
	for _, proxy := range proxies {
		items = append(items, ownerProxyToMap(proxy))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) CreateOwnerProxy(w http.ResponseWriter, r *http.Request) {
	if h.proxyCrypto == nil {
		writeError(w, http.StatusServiceUnavailable, "PROXY_ENCRYPTION_KEY is required before database proxies can be managed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var request ownerProxyRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	endpoint, redactedEndpoint, err := validateProxyEndpoint(request.Endpoint)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ciphertext, nonce, err := h.proxyCrypto.Encrypt(endpoint)
	if err != nil {
		log.Printf("[backend] CreateOwnerProxy encrypt error for %q: %v", redactedEndpoint, err)
		writeError(w, http.StatusInternalServerError, "failed to encrypt proxy")
		return
	}

	proxy, err := h.postgres.CreateOutboundProxy(r.Context(), redactedEndpoint, ciphertext, nonce, "default")
	if errors.Is(err, store.ErrOutboundProxyAlreadyExists) {
		writeError(w, http.StatusConflict, "proxy already exists")
		return
	}
	if err != nil {
		log.Printf("[backend] CreateOwnerProxy store error for %q: %v", redactedEndpoint, err)
		writeError(w, http.StatusInternalServerError, "failed to store proxy")
		return
	}
	if err := h.reloadOutboundProvider(r.Context()); err != nil {
		log.Printf("[backend] CreateOwnerProxy reload error: %v", err)
		writeError(w, http.StatusInternalServerError, "proxy saved but reload failed")
		return
	}

	writeJSON(w, http.StatusCreated, ownerProxyToMap(proxy))
}

func (h *Handler) DeleteOwnerProxy(w http.ResponseWriter, r *http.Request) {
	proxyID, err := strconv.ParseInt(chi.URLParam(r, "proxyID"), 10, 64)
	if err != nil || proxyID <= 0 {
		writeError(w, http.StatusBadRequest, "proxyID must be a positive integer")
		return
	}

	if err := h.postgres.DeleteOutboundProxy(r.Context(), proxyID); errors.Is(err, store.ErrOutboundProxyNotFound) {
		writeError(w, http.StatusNotFound, "proxy not found")
		return
	} else if err != nil {
		log.Printf("[backend] DeleteOwnerProxy store error for %d: %v", proxyID, err)
		writeError(w, http.StatusInternalServerError, "failed to delete proxy")
		return
	}
	if err := h.reloadOutboundProvider(r.Context()); err != nil {
		log.Printf("[backend] DeleteOwnerProxy reload error: %v", err)
		writeError(w, http.StatusInternalServerError, "proxy deleted but reload failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

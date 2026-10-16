package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wirezat/fileshare/internal/share"
)

func TestUnknownResolveErrorFailsClosed(t *testing.T) {
	rec := httptest.NewRecorder()
	resolveErr(rec, httptest.NewRequest(http.MethodGet, "/docs/a.txt", nil), "docs", share.Resolved{}, errors.New("boom"))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestUnknownUploadTargetErrorFailsClosed(t *testing.T) {
	rec := httptest.NewRecorder()
	uploadErr(rec, httptest.NewRequest(http.MethodPost, "/docs/chunk-init", nil), errors.New("boom"))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

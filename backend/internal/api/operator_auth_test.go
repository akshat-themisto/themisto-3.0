package api

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testOperatorServer() *Server {
	return &Server{
		apiKey: "operator-secret",
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestOperatorLoginIssuesHttpOnlySessionWithoutReturningSecret(t *testing.T) {
	s := testOperatorServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/operator/auth/login", bytes.NewBufferString(`{"access_key":"operator-secret"}`))
	rec := httptest.NewRecorder()

	s.handleOperatorLogin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "operator-secret") || strings.Contains(rec.Body.String(), "token") {
		t.Fatalf("login response exposed credentials: %s", rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies=%d want=1", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != operatorSessionCookie || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("unexpected operator cookie: %#v", cookie)
	}
	if _, _, valid := s.verifyOperatorSession(cookie.Value); !valid {
		t.Fatal("issued operator session did not verify")
	}
}

func TestOperatorLoginRejectsInvalidKey(t *testing.T) {
	s := testOperatorServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/operator/auth/login", bytes.NewBufferString(`{"access_key":"wrong"}`))
	rec := httptest.NewRecorder()
	s.handleOperatorLogin(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want=%d", rec.Code, http.StatusUnauthorized)
	}
}

func TestOperatorSessionRequiresConsoleHeaderForMutation(t *testing.T) {
	s := testOperatorServer()
	token, _, _, err := s.issueOperatorSession()
	if err != nil {
		t.Fatal(err)
	}
	handler := s.withOperatorAuth(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })

	withoutHeader := httptest.NewRequest(http.MethodPost, "/api/v1/operator/orgs", nil)
	withoutHeader.AddCookie(&http.Cookie{Name: operatorSessionCookie, Value: token})
	rec := httptest.NewRecorder()
	handler(rec, withoutHeader)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("without header status=%d want=%d", rec.Code, http.StatusForbidden)
	}

	withHeader := httptest.NewRequest(http.MethodPost, "/api/v1/operator/orgs", nil)
	withHeader.AddCookie(&http.Cookie{Name: operatorSessionCookie, Value: token})
	withHeader.Header.Set(operatorConsoleHeader, "console")
	rec = httptest.NewRecorder()
	handler(rec, withHeader)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("with header status=%d want=%d", rec.Code, http.StatusNoContent)
	}
}

func TestOperatorBearerRejectsBrowserOriginButAllowsHeadlessClient(t *testing.T) {
	s := testOperatorServer()
	handler := s.withOperatorAuth(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })

	browser := httptest.NewRequest(http.MethodGet, "/api/v1/operator/fleet", nil)
	browser.Header.Set("Authorization", "Bearer operator-secret")
	browser.Header.Set("Origin", "https://dashboard.example.com")
	rec := httptest.NewRecorder()
	handler(rec, browser)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("browser bearer status=%d want=%d", rec.Code, http.StatusUnauthorized)
	}

	headless := httptest.NewRequest(http.MethodGet, "/api/v1/operator/fleet", nil)
	headless.Header.Set("Authorization", "Bearer operator-secret")
	rec = httptest.NewRecorder()
	handler(rec, headless)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("headless bearer status=%d want=%d", rec.Code, http.StatusNoContent)
	}
}

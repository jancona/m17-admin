package web

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jancona/m17-admin/internal/platform"
	"github.com/jancona/m17-admin/internal/wizard"
)

type harness struct {
	t      *testing.T
	ts     *httptest.Server
	fake   *platform.Fake
	dir    string
	client *http.Client
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	fake := platform.NewFake()
	dir := t.TempDir()
	srv, err := New(fake, wizard.NewStore(dir), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	srv.Sessions().Sleep = func(time.Duration) {}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	h := &harness{t: t, ts: ts, fake: fake, dir: dir}
	h.client = h.newClient()
	return h
}

func (h *harness) newClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// get fetches path and returns the status, the redirect target (if any)
// and the body.
func (h *harness) get(c *http.Client, path string) (int, string, string) {
	h.t.Helper()
	resp, err := c.Get(h.ts.URL + path)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header.Get("Location"), string(b)
}

var csrfRE = regexp.MustCompile(`name="csrf" value="([^"]+)"`)

// post submits form to path with the CSRF token from the page at path.
func (h *harness) post(c *http.Client, path string, form url.Values) (int, string, string) {
	h.t.Helper()
	_, _, body := h.get(c, path)
	m := csrfRE.FindStringSubmatch(body)
	if m == nil {
		h.t.Fatalf("no CSRF token on %s:\n%s", path, body)
	}
	form.Set("csrf", m[1])
	return h.postRaw(c, path, form)
}

func (h *harness) postRaw(c *http.Client, path string, form url.Values) (int, string, string) {
	h.t.Helper()
	resp, err := c.PostForm(h.ts.URL+path, form)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header.Get("Location"), string(b)
}

func (h *harness) expectRedirect(code int, loc, wantLoc string) {
	h.t.Helper()
	if code != http.StatusFound && code != http.StatusSeeOther {
		h.t.Fatalf("status %d, want a redirect to %s", code, wantLoc)
	}
	if loc != wantLoc {
		h.t.Fatalf("redirect to %q, want %q", loc, wantLoc)
	}
}

// runWizard completes every step and applies the settings.
func (h *harness) runWizard(c *http.Client) {
	h.t.Helper()
	steps := []struct {
		path string
		form url.Values
	}{
		{"/setup/password", url.Values{"password": {"long-enough"}, "confirm": {"long-enough"}}},
		{"/setup/network", url.Values{"connection": {"wifi"}, "wifi_ssid": {"HomeNetwork"}, "wifi_password": {"wifi-secret"}, "wifi_country": {"US"}}},
		{"/setup/station", url.Values{"callsign": {"n1adj"}, "suffix": {"h"}, "hostname": {"m17-hotspot"}, "timezone": {"America/New_York"}}},
		{"/setup/hat", url.Values{"hat_type": {"mmdvm"}}},
		{"/setup/radio", url.Values{"rx_mhz": {"439.500"}, "power": {"10"}}},
		{"/setup/reflector", url.Values{"reflector": {"M17-XOR"}, "module": {"b"}}},
		{"/setup/updates", url.Values{"auto_update": {"notify"}, "update_hour": {"4"}}},
	}
	for i, s := range steps {
		code, loc, body := h.post(c, s.path, s.form)
		next := "/setup/review"
		if i+1 < len(steps) {
			next = steps[i+1].path
		}
		if loc != next {
			h.t.Fatalf("%s: status %d, redirect %q, want %q\n%s", s.path, code, loc, next, body)
		}
	}
	code, loc, _ := h.post(c, "/setup/review", url.Values{})
	h.expectRedirect(code, loc, "/setup/done")
}

func TestFullWizard(t *testing.T) {
	h := newHarness(t)
	c := h.client

	// Nothing but the password step until a password exists
	code, loc, _ := h.get(c, "/setup/")
	h.expectRedirect(code, loc, "/setup/password")
	code, loc, _ = h.get(c, "/setup/network")
	h.expectRedirect(code, loc, "/setup/password")
	code, loc, _ = h.get(c, "/setup/login")
	h.expectRedirect(code, loc, "/setup/password")

	h.runWizard(c)

	if len(h.fake.Applied) != 1 {
		t.Fatalf("Apply called %d times", len(h.fake.Applied))
	}
	got := h.fake.Applied[0]
	for _, want := range []platform.Setting{
		{Key: "wifi_ssid", Value: "HomeNetwork"},
		{Key: "wifi_password", Value: "wifi-secret"},
		{Key: "callsign", Value: "N1ADJ H"},
		{Key: "hat", Value: "mmdvm"},
		{Key: "frequency", Value: "439.5"},
		{Key: "reflector", Value: "M17-XOR"},
		{Key: "module", Value: "B"},
		{Key: "auto_update", Value: "notify"},
		{Key: "update_hour", Value: "4"},
	} {
		if !slices.Contains(got, want) {
			t.Errorf("applied settings lack %v: %v", want, got)
		}
	}
	if ok, _ := h.fake.CheckAdminPassword("long-enough"); !ok {
		t.Error("admin password not set")
	}

	code, _, body := h.get(c, "/setup/done")
	if code != 200 || !strings.Contains(body, "joining “HomeNetwork”") {
		t.Errorf("done page: %d\n%s", code, body)
	}
	if strings.Contains(body, "wifi-secret") {
		t.Error("the done page shows the WiFi password")
	}
	if _, err := os.Stat(filepath.Join(h.dir, "wizard.json")); !os.IsNotExist(err) {
		t.Errorf("wizard.json (with the WiFi password) kept after applying: %v", err)
	}
}

// TestRerunKeepsSavedWiFi: running the wizard on a hotspot that already
// has its WiFi set up must not rewrite that network without its password.
func TestRerunKeepsSavedWiFi(t *testing.T) {
	h := newHarness(t)
	h.fake.SetSavedWiFi("HomeNetwork")
	c := h.client
	h.post(c, "/setup/password", url.Values{"password": {"long-enough"}, "confirm": {"long-enough"}})
	_, _, body := h.get(c, "/setup/network")
	if !regexp.MustCompile(`value="HomeNetwork" checked`).MatchString(body) || !strings.Contains(body, "keep its saved password") {
		t.Error("the saved network isn't preselected with the keep-password hint")
	}
	h.post(c, "/setup/network", url.Values{"connection": {"wifi"}, "wifi_ssid": {"HomeNetwork"}, "wifi_password": {""}, "wifi_country": {"US"}})
	h.post(c, "/setup/station", url.Values{"callsign": {"N1ADJ"}, "hostname": {"m17-hotspot"}, "timezone": {"UTC"}})
	h.post(c, "/setup/hat", url.Values{"hat_type": {"mmdvm"}})
	h.post(c, "/setup/radio", url.Values{"rx_mhz": {"439.5"}, "power": {"10"}})
	h.post(c, "/setup/reflector", url.Values{"reflector": {"M17-M17"}, "module": {"C"}})
	h.post(c, "/setup/updates", url.Values{"auto_update": {"auto"}, "update_hour": {"3"}})
	_, _, body = h.get(c, "/setup/review")
	if !strings.Contains(body, "saved settings, unchanged") || strings.Contains(body, ", open") || strings.Contains(body, "will lose that connection") {
		t.Errorf("review for a kept network:\n%s", body)
	}
	h.post(c, "/setup/review", url.Values{})
	for _, s := range h.fake.Applied[0] {
		if s.Key == "wifi_ssid" || s.Key == "wifi_password" {
			t.Errorf("the saved network was rewritten: %v", h.fake.Applied[0])
		}
	}
	_, _, body = h.get(c, "/setup/done")
	if strings.Contains(body, "is joining") {
		t.Error("the done page says the hotspot is changing networks")
	}
}

func TestLoginRequiredOncePasswordSet(t *testing.T) {
	h := newHarness(t)
	h.runWizard(h.client)

	other := h.newClient()
	code, loc, _ := h.get(other, "/setup/")
	h.expectRedirect(code, loc, "/setup/login")
	code, loc, _ = h.get(other, "/setup/password")
	h.expectRedirect(code, loc, "/setup/login")

	code, _, body := h.post(other, "/setup/login", url.Values{"password": {"wrong-password"}})
	if code != http.StatusUnauthorized || !strings.Contains(body, "isn&#39;t the admin password") {
		t.Errorf("wrong password: %d", code)
	}
	code, loc, _ = h.post(other, "/setup/login", url.Values{"password": {"long-enough"}})
	h.expectRedirect(code, loc, "/setup/")
	code, _, _ = h.get(other, "/setup/password")
	if code != 200 {
		t.Errorf("after login, password step: %d", code)
	}

	// Log out, and the session is gone
	code, loc, _ = h.post(other, "/setup/password", url.Values{}) // keep password: empty fields
	h.expectRedirect(code, loc, "/setup/network")
	_, _, body = h.get(other, "/setup/network")
	m := csrfRE.FindStringSubmatch(body)
	code, loc, _ = h.postRaw(other, "/setup/logout", url.Values{"csrf": {m[1]}})
	h.expectRedirect(code, loc, "/setup/login")
	code, loc, _ = h.get(other, "/setup/network")
	h.expectRedirect(code, loc, "/setup/login")
}

func TestSessionIDChangesAtLogin(t *testing.T) {
	h := newHarness(t)
	h.runWizard(h.client)
	other := h.newClient()
	h.get(other, "/setup/login")
	u, _ := url.Parse(h.ts.URL)
	before := other.Jar.Cookies(u)
	h.post(other, "/setup/login", url.Values{"password": {"long-enough"}})
	after := other.Jar.Cookies(u)
	if len(before) != 1 || len(after) != 1 || before[0].Value == after[0].Value {
		t.Errorf("session cookie not replaced at login: %v -> %v", before, after)
	}
}

func TestCSRF(t *testing.T) {
	h := newHarness(t)
	code, _, _ := h.postRaw(h.client, "/setup/password", url.Values{"password": {"long-enough"}, "confirm": {"long-enough"}})
	if code != http.StatusForbidden {
		t.Errorf("POST without a CSRF token: %d, want 403", code)
	}
	code, _, _ = h.postRaw(h.client, "/setup/password", url.Values{"csrf": {"forged"}, "password": {"long-enough"}, "confirm": {"long-enough"}})
	if code != http.StatusForbidden {
		t.Errorf("POST with a wrong CSRF token: %d, want 403", code)
	}
	if set, _ := h.fake.AdminPasswordSet(); set {
		t.Error("a forged form set the password")
	}
}

func TestValidationErrorsKeepInput(t *testing.T) {
	h := newHarness(t)
	c := h.client
	code, _, body := h.post(c, "/setup/password", url.Values{"password": {"short"}, "confirm": {"short"}})
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "at least 8 characters") {
		t.Fatalf("short password: %d", code)
	}
	h.post(c, "/setup/password", url.Values{"password": {"long-enough"}, "confirm": {"long-enough"}})
	h.post(c, "/setup/network", url.Values{"connection": {"ethernet"}, "wifi_country": {"US"}})
	code, _, body = h.post(c, "/setup/station", url.Values{"callsign": {"n1*adj"}, "hostname": {"My Hotspot"}, "timezone": {"Mars"}})
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("bad station: %d", code)
	}
	for _, want := range []string{"Use only letters", "lower-case letters", "Choose a time zone", `value="N1*ADJ"`} {
		if !strings.Contains(body, want) {
			t.Errorf("station errors: missing %q", want)
		}
	}
	// Can't skip ahead
	code, loc, _ := h.get(c, "/setup/radio")
	h.expectRedirect(code, loc, "/setup/station")
}

func TestOutOfBandFrequencyIsAWarning(t *testing.T) {
	h := newHarness(t)
	c := h.client
	h.post(c, "/setup/password", url.Values{"password": {"long-enough"}, "confirm": {"long-enough"}})
	h.post(c, "/setup/network", url.Values{"connection": {"ethernet"}, "wifi_country": {"US"}})
	h.post(c, "/setup/station", url.Values{"callsign": {"N1ADJ"}, "hostname": {"m17-hotspot"}, "timezone": {"UTC"}})
	h.post(c, "/setup/hat", url.Values{"hat_type": {"cc1200"}})
	code, loc, _ := h.post(c, "/setup/radio", url.Values{"rx_mhz": {"460.0"}, "power": {"10"}, "duplex": {"on"}, "tx_mhz": {"455"}})
	h.expectRedirect(code, loc, "/setup/reflector")
	h.post(c, "/setup/reflector", url.Values{"reflector": {""}, "module": {"A"}})
	h.post(c, "/setup/updates", url.Values{"auto_update": {"auto"}, "update_hour": {"3"}})
	_, _, body := h.get(c, "/setup/review")
	if !strings.Contains(body, "outside the usual amateur bands") {
		t.Error("review page doesn't repeat the band warning")
	}
	if !strings.Contains(body, "None (RF only)") || strings.Contains(body, "duplex") {
		t.Errorf("review: RF only, and the CC1200 can't be duplex:\n%s", body)
	}
}

func TestDetectedHATIsPreselected(t *testing.T) {
	h := newHarness(t)
	c := h.client
	h.post(c, "/setup/password", url.Values{"password": {"long-enough"}, "confirm": {"long-enough"}})
	h.post(c, "/setup/network", url.Values{"connection": {"ethernet"}, "wifi_country": {"US"}})
	h.post(c, "/setup/station", url.Values{"callsign": {"N1ADJ"}, "hostname": {"m17-hotspot"}, "timezone": {"UTC"}})
	_, _, body := h.get(c, "/setup/hat")
	if !regexp.MustCompile(`value="mmdvm" checked`).MatchString(body) || !strings.Contains(body, "MMDVM_HS_Dual_Hat") {
		t.Error("the detected MMDVM HAT isn't shown and preselected")
	}
	h.fake.SetHAT(platform.HAT{})
	_, _, body = h.get(c, "/setup/hat")
	if !strings.Contains(body, "No HAT answered") {
		t.Error("no-HAT message missing")
	}
}

func TestApplyWarningsShown(t *testing.T) {
	h := newHarness(t)
	h.fake.ApplyWarnings = []string{"invalid callsign 'X'"}
	h.runWizard(h.client)
	_, _, body := h.get(h.client, "/setup/done")
	if !strings.Contains(body, "invalid callsign &#39;X&#39;") || !strings.Contains(body, "weren't applied") {
		t.Errorf("done page without the warnings:\n%s", body)
	}
}

func TestCaptivePortalAndHeaders(t *testing.T) {
	h := newHarness(t)
	for _, p := range []string{"/generate_204", "/hotspot-detect.html", "/connecttest.txt"} {
		req, _ := http.NewRequest("GET", h.ts.URL+p, nil)
		req.Host = "connectivitycheck.gstatic.com"
		resp, err := h.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "http://connectivitycheck.gstatic.com/setup/" {
			t.Errorf("%s: %d %s", p, resp.StatusCode, resp.Header.Get("Location"))
		}
	}
	resp, err := h.client.Get(h.ts.URL + "/setup/password")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
		t.Errorf("CSP: %q", csp)
	}
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Error("wizard pages must not be cached")
	}
	code, _, css := h.get(h.client, "/setup/static/style.css")
	if code != 200 || !strings.Contains(css, "prefers-color-scheme") {
		t.Errorf("static CSS: %d", code)
	}
}

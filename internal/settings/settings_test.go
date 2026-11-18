package settings

import (
	"math"
	"slices"
	"testing"

	"github.com/jancona/m17-admin/internal/platform"
)

func good() Draft {
	return Draft{
		UseWiFi: true, WiFiSSID: "Home", WiFiPassword: "secret-pass", WiFiCountry: "US",
		Callsign: "N1ADJ", Suffix: "H", Hostname: "m17-hotspot", TimeZone: "America/New_York",
		HATType: "mmdvm", RXFrequency: 439.5, TXFrequency: 439.5, Power: 10,
		Reflector: "M17-M17", Module: "C", AutoUpdate: "auto", UpdateHour: 3,
	}
}

var zones = []string{"America/New_York", "UTC"}

func TestChecksAcceptGoodDraft(t *testing.T) {
	d := good()
	for name, p := range map[string]Problems{
		"network":   d.CheckNetwork(CountryCodes()),
		"station":   d.CheckStation(zones),
		"hat":       d.CheckHAT(),
		"radio":     d.CheckRadio(),
		"reflector": d.CheckReflector([]platform.Reflector{{Name: "M17-M17"}}),
		"updates":   d.CheckUpdates(),
	} {
		if !p.OK() || len(p.Warnings) > 0 {
			t.Errorf("%s: errors %v, warnings %v", name, p.Errors, p.Warnings)
		}
	}
}

func TestChecksReject(t *testing.T) {
	cases := []struct {
		name  string
		edit  func(*Draft)
		check func(Draft) Problems
		field string
		warn  bool // a warning, not an error
	}{
		{"no ssid", func(d *Draft) { d.WiFiSSID = "" }, netCheck, "wifi_ssid", false},
		{"long ssid", func(d *Draft) { d.WiFiSSID = "123456789012345678901234567890123" }, netCheck, "wifi_ssid", false},
		{"short wifi password", func(d *Draft) { d.WiFiPassword = "1234567" }, netCheck, "wifi_password", false},
		{"bad country", func(d *Draft) { d.WiFiCountry = "XX" }, netCheck, "wifi_country", false},
		{"no callsign", func(d *Draft) { d.Callsign = "" }, stationCheck, "callsign", false},
		{"bad callsign chars", func(d *Draft) { d.Callsign = "N1*ADJ" }, stationCheck, "callsign", false},
		{"callsign too long", func(d *Draft) { d.Callsign = "AB1CDEFG" }, stationCheck, "callsign", false},
		{"callsign without digit", func(d *Draft) { d.Callsign = "NOCALL" }, stationCheck, "callsign", true},
		{"two-letter suffix", func(d *Draft) { d.Suffix = "HS" }, stationCheck, "suffix", false},
		{"bad hostname", func(d *Draft) { d.Hostname = "-hotspot" }, stationCheck, "hostname", false},
		{"upper-case hostname", func(d *Draft) { d.Hostname = "Hotspot" }, stationCheck, "hostname", false},
		{"unknown zone", func(d *Draft) { d.TimeZone = "Mars/Olympus" }, stationCheck, "timezone", false},
		{"bad hat", func(d *Draft) { d.HATType = "rtl-sdr" }, func(d Draft) Problems { return d.CheckHAT() }, "hat_type", false},
		{"no frequency", func(d *Draft) { d.RXFrequency = math.NaN() }, radioCheck, "rx_mhz", false},
		{"frequency in Hz", func(d *Draft) { d.RXFrequency = 439500000 }, radioCheck, "rx_mhz", false},
		{"out of band", func(d *Draft) { d.RXFrequency = 460.0 }, radioCheck, "rx_mhz", true},
		{"duplex same frequency", func(d *Draft) { d.Duplex = true }, radioCheck, "tx_mhz", false},
		{"power", func(d *Draft) { d.Power = 40 }, radioCheck, "power", false},
		{"unknown reflector", func(d *Draft) { d.Reflector = "M17-NOPE" }, reflCheck, "reflector", false},
		{"bad module", func(d *Draft) { d.Module = "c1" }, reflCheck, "module", false},
		{"bad policy", func(d *Draft) { d.AutoUpdate = "sometimes" }, func(d Draft) Problems { return d.CheckUpdates() }, "auto_update", false},
		{"bad hour", func(d *Draft) { d.UpdateHour = 24 }, func(d Draft) Problems { return d.CheckUpdates() }, "update_hour", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := good()
			c.edit(&d)
			p := c.check(d)
			if c.warn {
				if !p.OK() || p.Warnings[c.field] == "" {
					t.Errorf("want a warning on %s, got errors %v warnings %v", c.field, p.Errors, p.Warnings)
				}
				return
			}
			if p.Errors[c.field] == "" {
				t.Errorf("want an error on %s, got %v", c.field, p.Errors)
			}
		})
	}
}

func netCheck(d Draft) Problems     { return d.CheckNetwork(CountryCodes()) }
func stationCheck(d Draft) Problems { return d.CheckStation(zones) }
func radioCheck(d Draft) Problems   { return d.CheckRadio() }
func reflCheck(d Draft) Problems {
	return d.CheckReflector([]platform.Reflector{{Name: "M17-M17"}})
}

func TestEthernetNeedsNoWiFi(t *testing.T) {
	d := good()
	d.UseWiFi = false
	d.WiFiSSID = ""
	if p := d.CheckNetwork(CountryCodes()); !p.OK() {
		t.Errorf("Ethernet: %v", p.Errors)
	}
	for _, s := range d.Settings() {
		if s.Key == "wifi_ssid" || s.Key == "wifi_password" {
			t.Errorf("Ethernet draft produced %s", s.Key)
		}
	}
}

func TestSavedNetworkIsKept(t *testing.T) {
	d := FromCurrent(platform.Current{WiFiSSIDs: []string{"Home"}, WiFiCountry: "GB", Online: true})
	if !d.UseWiFi || d.WiFiSSID != "Home" || !d.KeepWiFi {
		t.Fatalf("from current: %+v", d)
	}
	d.UpdateKeepWiFi()
	got := d.Settings()
	if slices.ContainsFunc(got, func(s platform.Setting) bool { return s.Key == "wifi_ssid" || s.Key == "wifi_password" }) {
		t.Errorf("a saved network with no new password was rewritten: %v", got)
	}
	if !slices.Contains(got, platform.Setting{Key: "wifi_country", Value: "GB"}) {
		t.Errorf("country not kept: %v", got)
	}
	// A new password for the saved network: rewrite it
	d.WiFiPassword = "new-password"
	d.UpdateKeepWiFi()
	if d.KeepWiFi || !slices.Contains(d.Settings(), platform.Setting{Key: "wifi_password", Value: "new-password"}) {
		t.Errorf("new password for a saved network not applied: %+v", d)
	}
	// Another network with no password: an open network
	d.WiFiSSID, d.WiFiPassword = "Cafe", ""
	d.UpdateKeepWiFi()
	if d.KeepWiFi || !slices.Contains(d.Settings(), platform.Setting{Key: "wifi_ssid", Value: "Cafe"}) {
		t.Errorf("a new open network wasn't written: %+v", d)
	}
}

func TestOpenNetworkAndHexKey(t *testing.T) {
	d := good()
	d.WiFiPassword = ""
	if p := d.CheckNetwork(CountryCodes()); !p.OK() {
		t.Errorf("open network: %v", p.Errors)
	}
	d.WiFiPassword = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if p := d.CheckNetwork(CountryCodes()); !p.OK() {
		t.Errorf("64-digit hex key: %v", p.Errors)
	}
}

func TestSettings(t *testing.T) {
	d := good()
	got := d.Settings()
	want := []platform.Setting{
		{Key: "wifi_country", Value: "US"},
		{Key: "wifi_ssid", Value: "Home"},
		{Key: "wifi_password", Value: "secret-pass"},
		{Key: "hostname", Value: "m17-hotspot"},
		{Key: "timezone", Value: "America/New_York"},
		{Key: "callsign", Value: "N1ADJ H"},
		{Key: "hat", Value: "mmdvm"},
		{Key: "frequency", Value: "439.5"},
		{Key: "duplex", Value: "false"},
		{Key: "power", Value: "10"},
		{Key: "reflector", Value: "M17-M17"},
		{Key: "module", Value: "C"},
		{Key: "auto_update", Value: "auto"},
		{Key: "update_hour", Value: "3"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("Settings:\n got %v\nwant %v", got, want)
	}

	d.Duplex, d.TXFrequency = true, 434.5
	d.Reflector = ""
	got = d.Settings()
	for _, w := range []platform.Setting{
		{Key: "rx_frequency", Value: "439.5"}, {Key: "tx_frequency", Value: "434.5"},
		{Key: "duplex", Value: "true"}, {Key: "reflector", Value: "none"},
	} {
		if !slices.Contains(got, w) {
			t.Errorf("duplex, RF only: missing %v in %v", w, got)
		}
	}
	if slices.ContainsFunc(got, func(s platform.Setting) bool { return s.Key == "module" }) {
		t.Errorf("RF only still sets module: %v", got)
	}
}

func TestParseMHz(t *testing.T) {
	for in, want := range map[string]float64{"439.5": 439.5, " 439,500 ": 439.5, "144": 144} {
		if got := ParseMHz(in); got != want {
			t.Errorf("ParseMHz(%q) = %v, want %v", in, got, want)
		}
	}
	if !math.IsNaN(ParseMHz("abc")) {
		t.Error("ParseMHz(abc) is not NaN")
	}
}

func TestAdminPassword(t *testing.T) {
	if CheckAdminPassword("short", "short").OK() {
		t.Error("short password accepted")
	}
	if CheckAdminPassword("long-enough", "different").OK() {
		t.Error("mismatched confirmation accepted")
	}
	if p := CheckAdminPassword("long-enough", "long-enough"); !p.OK() {
		t.Errorf("good password: %v", p.Errors)
	}
}

func TestFromCurrent(t *testing.T) {
	d := FromCurrent(platform.Current{Callsign: "N1ADJ H", RXFrequency: 439_500_000, Online: true})
	if d.Callsign != "N1ADJ" || d.Suffix != "H" {
		t.Errorf("callsign split: %q %q", d.Callsign, d.Suffix)
	}
	if d.RXFrequency != 439.5 {
		t.Errorf("frequency: %v", d.RXFrequency)
	}
	if d.UseWiFi {
		t.Error("online with no WiFi configured should default to Ethernet")
	}
	if d.WiFiCountry != "US" || d.AutoUpdate != "auto" {
		t.Errorf("defaults: %q %q", d.WiFiCountry, d.AutoUpdate)
	}
}

package platform

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

// Fake is an in-memory Platform for tests and for running the UI on a
// workstation (m17-admin -dev). It starts as an unconfigured hotspot with
// an MMDVM HAT and a few WiFi networks in range.
type Fake struct {
	mu           sync.Mutex
	current      Current
	networks     []Network
	hat          HAT
	reflectors   []Reflector
	passwordHash []byte

	// Applied records every Apply call, for tests.
	Applied [][]Setting
	// ApplyWarnings, if set, are returned by the next Apply.
	ApplyWarnings []string
}

// NewFake returns a Fake in its unconfigured state.
func NewFake() *Fake {
	return &Fake{
		current: Current{
			Hostname:    "m17-hotspot",
			TimeZone:    "UTC",
			WiFiCountry: "US",
			HATType:     "cc1200",
			RXFrequency: 439_000_000,
			TXFrequency: 439_000_000,
			Power:       10,
			Reflector:   "M17-M17",
			Module:      "C",
			AutoUpdate:  "auto",
			UpdateHour:  3,
			Online:      false,
			SSHKeys:     0,
		},
		networks: []Network{
			{SSID: "HomeNetwork", Signal: -48, Secured: true, Frequency: 2437},
			{SSID: "Neighbors 5G", Signal: -71, Secured: true, Frequency: 5180},
			{SSID: "CoffeeShop Guest", Signal: -80, Secured: false, Frequency: 2412},
			{SSID: "HomeNetwork", Signal: -62, Secured: true, Frequency: 5745},
		},
		hat: HAT{
			Type:  "mmdvm",
			Ident: "MMDVM_HS_Dual_Hat-v1.6.1 20261116 14.7456MHz dual ADF7021 FW by CA6JAU (M17)",
			Speed: 115200,
		},
		reflectors: []Reflector{
			{Name: "M17-M17", Server: "107.191.121.105", Port: 17000},
			{Name: "M17-XOR", Server: "81.95.8.37", Port: 17000},
			{Name: "M17-USA", Server: "174.138.30.142", Port: 17000},
			{Name: "M17-DEV", Server: "152.70.192.70", Port: 17000},
			{Name: "M17-GBR", Server: "176.58.120.23", Port: 17000},
			{Name: "M17-QTC", Server: "158.101.153.161", Port: 17000},
		},
	}
}

func (f *Fake) Current(ctx context.Context) (Current, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.current
	c.WiFiSSIDs = append([]string(nil), f.current.WiFiSSIDs...)
	return c, nil
}

func (f *Fake) ScanWiFi(ctx context.Context) ([]Network, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Network(nil), f.networks...), nil
}

func (f *Fake) DetectHAT(ctx context.Context) (HAT, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hat, nil
}

// SetSavedWiFi makes the fake have settings for these networks, as a
// hotspot that is already set up does.
func (f *Fake) SetSavedWiFi(ssids ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.current.WiFiSSIDs = ssids
	f.current.Online = true
}

// SetHAT changes what DetectHAT reports.
func (f *Fake) SetHAT(h HAT) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hat = h
}

func (f *Fake) Reflectors(ctx context.Context) ([]Reflector, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Reflector(nil), f.reflectors...), nil
}

func (f *Fake) TimeZones(ctx context.Context) ([]string, error) {
	return []string{
		"America/Chicago", "America/Denver", "America/Los_Angeles", "America/New_York",
		"Asia/Tokyo", "Australia/Sydney", "Europe/Berlin", "Europe/London", "Europe/Warsaw", "UTC",
	}, nil
}

func (f *Fake) AdminPasswordSet() (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.passwordHash) > 0, nil
}

func (f *Fake) CheckAdminPassword(password string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.passwordHash) == 0 {
		return false, nil
	}
	return bcrypt.CompareHashAndPassword(f.passwordHash, []byte(password)) == nil, nil
}

func (f *Fake) SetAdminPassword(ctx context.Context, password string) error {
	if len(password) < 8 {
		return errors.New("the password must be at least 8 characters")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.passwordHash = h
	return nil
}

func (f *Fake) Apply(ctx context.Context, settings []Setting) (Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Applied = append(f.Applied, append([]Setting(nil), settings...))
	var res Result
	res.Warnings, f.ApplyWarnings = f.ApplyWarnings, nil
	var ssids []string
	for _, s := range settings {
		switch {
		case s.Key == "callsign":
			f.current.Callsign = strings.ToUpper(s.Value)
		case s.Key == "hostname":
			f.current.Hostname = s.Value
		case s.Key == "timezone":
			f.current.TimeZone = s.Value
		case s.Key == "wifi_country":
			f.current.WiFiCountry = s.Value
		case s.Key == "wifi_ssid" || strings.HasPrefix(s.Key, "wifi_ssid_"):
			ssids = append(ssids, s.Value)
			res.NetworkChanged = true
		case s.Key == "hat":
			if s.Value == "sx1255" && f.current.HATType != "sx1255" {
				res.RebootNeeded = true
			}
			f.current.HATType = s.Value
		case s.Key == "reflector":
			f.current.Reflector = s.Value
		case s.Key == "module":
			f.current.Module = s.Value
		case s.Key == "auto_update":
			f.current.AutoUpdate = s.Value
		case s.Key == "ssh_key":
			f.current.SSHKeys++
		case s.Key == "enable_ssh_password":
			f.current.SSHPasswordLogins = s.Value == "true"
		}
	}
	if ssids != nil {
		sort.Strings(ssids)
		f.current.WiFiSSIDs = ssids
	}
	return res, nil
}

var _ Platform = (*Fake)(nil)

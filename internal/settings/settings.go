// Package settings holds the setup wizard's answers (the Draft), checks
// them, and turns them into the m17-config.txt keys that m17os-config
// applies.
package settings

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jancona/m17-admin/internal/platform"
)

// Draft is everything the wizard has collected so far.
type Draft struct {
	// Network
	UseWiFi      bool   `json:"use_wifi"`
	WiFiSSID     string `json:"wifi_ssid"`
	WiFiPassword string `json:"wifi_password"`
	WiFiCountry  string `json:"wifi_country"`

	// Station
	Callsign string `json:"callsign"` // base callsign, e.g. N1ADJ
	Suffix   string `json:"suffix"`   // optional single character, e.g. H
	Hostname string `json:"hostname"`
	TimeZone string `json:"timezone"`

	// Radio
	HATType     string  `json:"hat_type"`
	RXFrequency float64 `json:"rx_mhz"`
	TXFrequency float64 `json:"tx_mhz"`
	Duplex      bool    `json:"duplex"`
	Power       int     `json:"power"`

	// Reflector ("" for RF only)
	Reflector string `json:"reflector"`
	Module    string `json:"module"`

	// Updates
	AutoUpdate string `json:"auto_update"`
	UpdateHour int    `json:"update_hour"`
}

// FromCurrent starts a draft from the hotspot's current settings.
func FromCurrent(c platform.Current) Draft {
	base, suffix, _ := strings.Cut(c.Callsign, " ")
	d := Draft{
		UseWiFi:     !c.Online || len(c.WiFiSSIDs) > 0,
		WiFiCountry: c.WiFiCountry,
		Callsign:    base,
		Suffix:      suffix,
		Hostname:    c.Hostname,
		TimeZone:    c.TimeZone,
		HATType:     c.HATType,
		RXFrequency: float64(c.RXFrequency) / 1e6,
		TXFrequency: float64(c.TXFrequency) / 1e6,
		Duplex:      c.Duplex,
		Power:       c.Power,
		Reflector:   c.Reflector,
		Module:      c.Module,
		AutoUpdate:  c.AutoUpdate,
		UpdateHour:  c.UpdateHour,
	}
	if len(c.WiFiSSIDs) > 0 {
		d.WiFiSSID = c.WiFiSSIDs[0]
	}
	if d.WiFiCountry == "" {
		d.WiFiCountry = "US"
	}
	if d.AutoUpdate == "" {
		d.AutoUpdate = "auto"
	}
	return d
}

// Problems are a form's errors (which stop the step) and warnings (which
// are shown but don't), keyed by field name.
type Problems struct {
	Errors   map[string]string
	Warnings map[string]string
}

func newProblems() Problems {
	return Problems{Errors: map[string]string{}, Warnings: map[string]string{}}
}

// OK reports whether there are no errors.
func (p Problems) OK() bool { return len(p.Errors) == 0 }

// MinPasswordLength is the shortest admin password rpi-dashboard accepts.
const MinPasswordLength = 8

// CheckAdminPassword checks a new admin password and its confirmation.
func CheckAdminPassword(password, confirm string) Problems {
	p := newProblems()
	switch {
	case utf8.RuneCountInString(password) < MinPasswordLength:
		p.Errors["password"] = fmt.Sprintf("Use at least %d characters.", MinPasswordLength)
	case password != confirm:
		p.Errors["confirm"] = "The two passwords don't match."
	}
	return p
}

// CheckNetwork checks the network step.
func (d Draft) CheckNetwork(countries []string) Problems {
	p := newProblems()
	if !d.UseWiFi {
		return p
	}
	switch n := len(d.WiFiSSID); {
	case n == 0:
		p.Errors["wifi_ssid"] = "Choose a network, or type its name."
	case n > 32:
		p.Errors["wifi_ssid"] = "A WiFi network name is at most 32 bytes."
	}
	if d.WiFiPassword != "" {
		n := len(d.WiFiPassword)
		hexKey := n == 64 && regexp.MustCompile(`^[0-9a-fA-F]+$`).MatchString(d.WiFiPassword)
		if !hexKey && (n < 8 || n > 63) {
			p.Errors["wifi_password"] = "A WPA password is 8 to 63 characters."
		}
	}
	if !slices.Contains(countries, d.WiFiCountry) {
		p.Errors["wifi_country"] = "Choose the country the hotspot is in."
	}
	return p
}

var (
	callsignRE = regexp.MustCompile(`^[A-Z0-9/.-]+$`)
	hostnameRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
)

// FullCallsign is the callsign as the gateway uses it: "N1ADJ" or
// "N1ADJ H".
func (d Draft) FullCallsign() string {
	if d.Suffix == "" {
		return d.Callsign
	}
	return d.Callsign + " " + d.Suffix
}

// CheckStation checks the station step. Callsign and Suffix should already
// be upper case.
func (d Draft) CheckStation(timeZones []string) Problems {
	p := newProblems()
	switch {
	case d.Callsign == "":
		p.Errors["callsign"] = "Enter your callsign."
	case !callsignRE.MatchString(d.Callsign):
		p.Errors["callsign"] = "Use only letters, digits, / - and ."
	case !strings.ContainsAny(d.Callsign, "0123456789"):
		p.Warnings["callsign"] = "Callsigns usually contain a digit. Check it's right."
	}
	if d.Suffix != "" && !regexp.MustCompile(`^[A-Z0-9]$`).MatchString(d.Suffix) {
		p.Errors["suffix"] = "The suffix is a single letter or digit."
	}
	// M17 encodes up to 9 characters (base 40)
	if n := len(d.FullCallsign()); n > 9 && p.Errors["callsign"] == "" {
		p.Errors["callsign"] = fmt.Sprintf("M17 callsigns are at most 9 characters with the suffix and its space (this is %d).", n)
	}
	if !hostnameRE.MatchString(d.Hostname) {
		p.Errors["hostname"] = "Use lower-case letters, digits and hyphens (not at the start or end)."
	}
	if !slices.Contains(timeZones, d.TimeZone) {
		p.Errors["timezone"] = "Choose a time zone from the list."
	}
	return p
}

// HATTypes are the radio HATs m17-gateway supports.
var HATTypes = []string{"cc1200", "sx1255", "mmdvm"}

// CheckHAT checks the HAT step.
func (d Draft) CheckHAT() Problems {
	p := newProblems()
	if !slices.Contains(HATTypes, d.HATType) {
		p.Errors["hat_type"] = "Choose the radio HAT on your Pi."
	}
	return p
}

// band is an amateur band where M17 is commonly used. Allocations differ
// between countries, so a frequency outside them is a warning only.
type band struct {
	name     string
	low, top float64 // MHz
}

var bands = []band{
	{"2 m", 144, 148},
	{"1.25 m", 219, 225},
	{"70 cm", 420, 450},
	{"33 cm", 902, 928},
	{"23 cm", 1240, 1300},
}

func bandOf(mhz float64) string {
	for _, b := range bands {
		if mhz >= b.low && mhz <= b.top {
			return b.name
		}
	}
	return ""
}

// CheckRadio checks the radio step.
func (d Draft) CheckRadio() Problems {
	p := newProblems()
	check := func(field string, mhz float64) {
		switch {
		case math.IsNaN(mhz) || mhz <= 0:
			p.Errors[field] = "Enter the frequency in MHz, e.g. 439.500."
		case mhz < 100 || mhz > 6000:
			p.Errors[field] = "That doesn't look like a frequency in MHz."
		case bandOf(mhz) == "":
			p.Warnings[field] = "This is outside the usual amateur bands. Check it's allowed where you are."
		}
	}
	check("rx_mhz", d.RXFrequency)
	if d.Duplex {
		check("tx_mhz", d.TXFrequency)
		if p.Errors["tx_mhz"] == "" && d.TXFrequency == d.RXFrequency {
			p.Errors["tx_mhz"] = "In duplex the receive and transmit frequencies must differ."
		}
	}
	if d.Power < -20 || d.Power > 30 {
		p.Errors["power"] = "Enter a power between -20 and 30 dBm."
	}
	return p
}

var moduleRE = regexp.MustCompile(`^[A-Z]$`)

// CheckReflector checks the reflector step. An empty Reflector means RF
// only (no reflector).
func (d Draft) CheckReflector(reflectors []platform.Reflector) Problems {
	p := newProblems()
	if d.Reflector == "" {
		return p
	}
	if !slices.ContainsFunc(reflectors, func(r platform.Reflector) bool { return r.Name == d.Reflector }) {
		p.Errors["reflector"] = "Choose a reflector from the list."
	}
	if !moduleRE.MatchString(d.Module) {
		p.Errors["module"] = "The module is a single letter, A to Z."
	}
	return p
}

// CheckUpdates checks the updates step.
func (d Draft) CheckUpdates() Problems {
	p := newProblems()
	if !slices.Contains([]string{"auto", "notify", "off"}, d.AutoUpdate) {
		p.Errors["auto_update"] = "Choose how updates are handled."
	}
	if d.UpdateHour < 0 || d.UpdateHour > 23 {
		p.Errors["update_hour"] = "Choose an hour from 0 to 23."
	}
	return p
}

// ParseMHz reads a frequency typed in MHz ("439.5", "439,500"). It returns
// NaN for anything that isn't a number.
func ParseMHz(s string) float64 {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", ".")
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return math.NaN()
	}
	return f
}

// mhz formats MHz for m17-config.txt, which accepts MHz with a decimal
// point (and converts to Hz).
func mhz(f float64) string {
	return strconv.FormatFloat(math.Round(f*1e6)/1e6, 'f', -1, 64)
}

// Settings converts the draft into m17-config.txt keys, in the order
// m17os-config should apply them. The HAT comes before the radio settings
// so its default serial speed doesn't overwrite anything else.
func (d Draft) Settings() []platform.Setting {
	var s []platform.Setting
	add := func(k, v string) { s = append(s, platform.Setting{Key: k, Value: v}) }
	if d.UseWiFi {
		add("wifi_country", d.WiFiCountry)
		add("wifi_ssid", d.WiFiSSID)
		if d.WiFiPassword != "" {
			add("wifi_password", d.WiFiPassword)
		}
	}
	add("hostname", d.Hostname)
	add("timezone", d.TimeZone)
	add("callsign", d.FullCallsign())
	add("hat", d.HATType)
	if d.Duplex {
		add("rx_frequency", mhz(d.RXFrequency))
		add("tx_frequency", mhz(d.TXFrequency))
		add("duplex", "true")
	} else {
		add("frequency", mhz(d.RXFrequency))
		add("duplex", "false")
	}
	add("power", strconv.Itoa(d.Power))
	if d.Reflector != "" {
		add("reflector", d.Reflector)
		add("module", d.Module)
	} else {
		// RF only: m17os-config clears the reflector for "none" (an empty
		// value would leave the current one in place)
		add("reflector", "none")
	}
	add("auto_update", d.AutoUpdate)
	add("update_hour", strconv.Itoa(d.UpdateHour))
	return s
}

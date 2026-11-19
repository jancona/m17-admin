// Package firmware finds the firmware files for the radio HAT's
// microcontroller and decides which one, if any, the HAT should get.
//
// Every firmware build carries the same description of itself that the
// HAT reports when asked (MMDVM's GetVersion, the CC1200 HAT's
// CMD_GET_IDENT), for example
//
//	MMDVM_HS_Dual_Hat-v1.6.1 20261116 14.7456MHz dual ADF7021 FW by CA6JAU (M17) GitID #02e5646
//	CC1200-HAT 420-450 MHz / FW v2.3 by Wojciech SP5WWP
//
// so the file for a board is the one whose description names the same
// board (and clock), and it is newer when its date or version is.
package firmware

import (
	"bytes"
	"cmp"
	"io/fs"
	"os"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Image is a firmware file.
type Image struct {
	Name  string // relative to the firmware directory: "mmdvm_hs/MMDVM_HS_Hat.bin"
	Ident string // the description built into it, on one line; "" if none was found
}

// Family is the directory the file is in: cc1200, mmdvm_hs or mmdvm.
func (i Image) Family() string { return path.Dir(i.Name) }

// Board is the file name without its directory and .bin.
func (i Image) Board() string { return strings.TrimSuffix(path.Base(i.Name), ".bin") }

// Families are the firmware directories for a HAT type, in the order
// they're offered.
func Families(hatType string) []string {
	switch hatType {
	case "cc1200":
		return []string{"cc1200"}
	case "mmdvm":
		return []string{"mmdvm_hs", "mmdvm"}
	}
	return nil
}

// FamilyTitle names a firmware directory for people.
func FamilyTitle(family string) string {
	switch family {
	case "cc1200":
		return "CC1200 HAT"
	case "mmdvm_hs":
		return "MMDVM_HS hotspot boards"
	case "mmdvm":
		return "MMDVM repeater boards"
	}
	return family
}

// Scan reads the .bin files under dir (the m17-firmware packages'
// /usr/share/m17-firmware). A missing directory is no files.
func Scan(dir string) ([]Image, error) {
	var imgs []Image
	err := fs.WalkDir(os.DirFS(dir), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == "." {
				return fs.SkipAll
			}
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(p, ".bin") {
			return nil
		}
		b, err := os.ReadFile(path.Join(dir, p))
		if err != nil {
			return nil
		}
		imgs = append(imgs, Image{Name: p, Ident: IdentIn(b)})
		return nil
	})
	slices.SortFunc(imgs, func(a, b Image) int { return cmp.Compare(a.Name, b.Name) })
	return imgs, err
}

// IdentIn finds the description built into a firmware file: the first
// run of text that names a clock (MHz) and a version (GitID or FW v).
func IdentIn(b []byte) string {
	start := -1
	for i := 0; i <= len(b); i++ {
		printable := i < len(b) && (b[i] >= 32 && b[i] < 127 || b[i] == '\n' || b[i] == '\r')
		if printable {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 && i-start >= 16 {
			run := b[start:i]
			if bytes.Contains(run, []byte("MHz")) && (bytes.Contains(run, []byte("GitID")) || bytes.Contains(run, []byte("FW v"))) {
				return OneLine(string(run))
			}
		}
		start = -1
	}
	return ""
}

// OneLine puts a multi-line description on one line, the way the HAT
// probe reports it: lines joined with " / ", runs of spaces made one.
func OneLine(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "\n", " / ")), " ")
}

// ── Comparing descriptions ───────────────────────────────────────────

var (
	dateRE    = regexp.MustCompile(`^(19|20)\d{6}$`)
	clockRE   = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*MHz`)
	fwVerRE   = regexp.MustCompile(`FW v(\d+(?:\.\d+)*)`)
	nameVerRE = regexp.MustCompile(`-v(\d+(?:\.\d+)*)$`)
	gitRE     = regexp.MustCompile(`GitID #([0-9a-fA-F]+)`)
)

// build is what a description says about the board and the build.
type build struct {
	board   string // which board (and clock) it is for
	date    string // YYYYMMDD; "" if none
	version []int  // FW vX.Y or -vX.Y.Z; nil if none
	git     string
}

func parse(ident string) build {
	var b build
	f := strings.Fields(ident)
	if len(f) == 0 {
		return b
	}
	if m := gitRE.FindStringSubmatch(ident); m != nil {
		b.git = strings.ToLower(m[1])
	}
	if m := fwVerRE.FindStringSubmatch(ident); m != nil {
		b.version = versionParts(m[1])
	}
	if strings.HasPrefix(f[0], "CC1200-HAT") {
		// CC1200-HAT 420-450 MHz / FW v2.3 ...: the board and its band
		b.board = f[0]
		if len(f) > 1 {
			b.board += " " + f[1]
		}
		return b
	}
	name := f[0]
	if m := nameVerRE.FindStringSubmatch(name); m != nil {
		b.version = versionParts(m[1])
		name = strings.TrimSuffix(name, m[0])
	}
	model := []string{name}
	for _, w := range f[1:] {
		if dateRE.MatchString(w) {
			b.date = w
			break
		}
		model = append(model, w)
	}
	if b.date == "" {
		model = model[:1] // no date: only the name is reliably the board
	}
	clock := ""
	if m := clockRE.FindStringSubmatch(ident); m != nil {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			clock = strconv.FormatFloat(v, 'f', 4, 64)
		}
	}
	dual := slices.Contains(f, "dual")
	b.board = strings.Join(model, " ") + "|" + clock + "|" + strconv.FormatBool(dual)
	return b
}

func versionParts(s string) []int {
	var v []int
	for _, p := range strings.Split(s, ".") {
		n, _ := strconv.Atoi(p)
		v = append(v, n)
	}
	return v
}

// compare orders two builds of the same board: by date, then version.
func compare(a, b build) int {
	if a.date != "" && b.date != "" {
		if c := cmp.Compare(a.date, b.date); c != 0 {
			return c
		}
	}
	return slices.Compare(a.version, b.version)
}

// ── The decision ─────────────────────────────────────────────────────

// State is how the HAT's firmware compares with the files.
type State string

const (
	// Unknown: the HAT didn't say what it is, or no file is for its board.
	Unknown State = "unknown"
	// Current: the HAT has the same build as the file for it.
	Current State = "current"
	// Update: the file for the HAT is newer than what it has.
	Update State = "update"
	// Newer: the HAT has a newer build than the file.
	Newer State = "newer"
)

// Report is what Check decided.
type Report struct {
	State State
	// Matches are the files for the HAT's board: usually one, several when
	// boards share a description (MMDVM repeater boards). Empty when the
	// board is unknown.
	Matches []Image
	// Recommended is the file to flash: the only match, when it differs
	// from what the HAT has. "" if none.
	Recommended string
}

// Check compares what the HAT reported (ident; "" if it didn't answer)
// with the files in families.
func Check(ident string, families []string, imgs []Image) Report {
	r := Report{State: Unknown}
	if strings.TrimSpace(ident) == "" {
		return r
	}
	have := parse(ident)
	if have.board == "" {
		return r
	}
	for _, img := range imgs {
		if !slices.Contains(families, img.Family()) || img.Ident == "" {
			continue
		}
		if parse(img.Ident).board == have.board {
			r.Matches = append(r.Matches, img)
		}
	}
	if len(r.Matches) == 0 {
		return r
	}
	file := parse(r.Matches[0].Ident)
	switch c := compare(file, have); {
	case c > 0:
		r.State = Update
	case c < 0:
		r.State = Newer
	case file.git != "" && have.git != "" && file.git != have.git:
		// Same date and version, different source: offer the packaged one
		r.State = Update
	default:
		r.State = Current
	}
	if r.State == Update && len(r.Matches) == 1 {
		r.Recommended = r.Matches[0].Name
	}
	return r
}

// For returns the files in families, in family order.
func For(families []string, imgs []Image) []Image {
	var out []Image
	for _, f := range families {
		for _, img := range imgs {
			if img.Family() == f {
				out = append(out, img)
			}
		}
	}
	return out
}

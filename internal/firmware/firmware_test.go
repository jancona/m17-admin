package firmware

import (
	"os"
	"path/filepath"
	"testing"
)

// The descriptions in the m17-firmware packages' files (November 2026)
var catalog = []Image{
	{"cc1200/CC1200_HAT-fw.bin", "CC1200-HAT 420-450 MHz / FW v2.3 by Wojciech SP5WWP"},
	{"mmdvm/MMDVM_RPT_Hat_0.3.bin", "MMDVM 20251014 12.0000 MHz GitID #9267a90"},
	{"mmdvm/Repeater-Builder_v3.bin", "MMDVM 20251014 12.0000 MHz GitID #9267a90"},
	{"mmdvm/Repeater-Builder_v4.bin", "MMDVM RB_STM32_DVM(446) 20251014 12.0000 MHz GitID #9267a90"},
	{"mmdvm_hs/MMDVM_HS_Dual_Hat-12mhz.bin", "MMDVM_HS_Dual_Hat-v1.6.1 20261116 12.2880MHz dual ADF7021 FW by CA6JAU (M17) GitID #02e5646"},
	{"mmdvm_hs/MMDVM_HS_Dual_Hat.bin", "MMDVM_HS_Dual_Hat-v1.6.1 20261116 14.7456MHz dual ADF7021 FW by CA6JAU (M17) GitID #02e5646"},
	{"mmdvm_hs/MMDVM_HS_Hat.bin", "MMDVM_HS_Hat-v1.6.1 20261116 14.7456MHz ADF7021 FW by CA6JAU (M17) GitID #02e5646"},
	{"mmdvm_hs/generic_duplex_gpio.bin", "MMDVM_HS-v1.6.1 20261116 14.7456MHz dual ADF7021 FW by CA6JAU (M17) GitID #02e5646"},
	{"mmdvm_hs/generic_gpio.bin", "MMDVM_HS-v1.6.1 20261116 14.7456MHz ADF7021 FW by CA6JAU (M17) GitID #02e5646"},
}

func TestCheck(t *testing.T) {
	mmdvm, cc := Families("mmdvm"), Families("cc1200")
	for _, tc := range []struct {
		name, ident string
		families    []string
		state       State
		recommended string
		matches     int
	}{
		{"same build", "MMDVM_HS_Dual_Hat-v1.6.1 20261116 14.7456MHz dual ADF7021 FW by CA6JAU (M17) GitID #02e5646",
			mmdvm, Current, "", 1},
		{"older dual hat", "MMDVM_HS_Dual_Hat-v1.5.2 20201108 14.7456MHz dual ADF7021 FW by CA6JAU GitID #89daa20",
			mmdvm, Update, "mmdvm_hs/MMDVM_HS_Dual_Hat.bin", 1},
		{"12 MHz clock", "MMDVM_HS_Dual_Hat-v1.4.17 20190529 12.2880MHz dual ADF7021 FW by CA6JAU GitID #cc451c4",
			mmdvm, Update, "mmdvm_hs/MMDVM_HS_Dual_Hat-12mhz.bin", 1},
		{"simplex hat", "MMDVM_HS_Hat-v1.4.17 20190529 14.7456MHz ADF7021 FW by CA6JAU GitID #cc451c4",
			mmdvm, Update, "mmdvm_hs/MMDVM_HS_Hat.bin", 1},
		{"generic duplex", "MMDVM_HS-v1.5.2 20201108 14.7456MHz dual ADF7021 FW by CA6JAU GitID #89daa20",
			mmdvm, Update, "mmdvm_hs/generic_duplex_gpio.bin", 1},
		{"newer on the board", "MMDVM_HS_Hat-v1.6.2 20270301 14.7456MHz ADF7021 FW by CA6JAU (M17) GitID #abcdef0",
			mmdvm, Newer, "", 1},
		{"same date, other source", "MMDVM_HS_Hat-v1.6.1 20261116 14.7456MHz ADF7021 FW by CA6JAU GitID #1111111",
			mmdvm, Update, "mmdvm_hs/MMDVM_HS_Hat.bin", 1},
		{"repeater boards share a description", "MMDVM 20240101 12.0000 MHz GitID #0000000",
			mmdvm, Update, "", 2},
		{"repeater builder v4", "MMDVM RB_STM32_DVM(446) 20240101 12.0000 MHz GitID #0000000",
			mmdvm, Update, "mmdvm/Repeater-Builder_v4.bin", 1},
		{"no file for this board", "ZUMspot-v1.6.0 20220101 14.7456MHz ADF7021 FW by CA6JAU GitID #1234567",
			mmdvm, Unknown, "", 0},
		{"nothing answered", "", mmdvm, Unknown, "", 0},
		{"cc1200 older", "CC1200-HAT 420-450 MHz / FW v1.1 by Wojciech SP5WWP", cc, Update, "cc1200/CC1200_HAT-fw.bin", 1},
		{"cc1200 same", "CC1200-HAT 420-450 MHz / FW v2.3 by Wojciech SP5WWP", cc, Current, "", 1},
		{"cc1200 other band", "CC1200-HAT 144-148 MHz / FW v1.0 by Wojciech SP5WWP", cc, Unknown, "", 0},
		{"wrong family", "CC1200-HAT 420-450 MHz / FW v1.1 by Wojciech SP5WWP", mmdvm, Unknown, "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := Check(tc.ident, tc.families, catalog)
			if r.State != tc.state || r.Recommended != tc.recommended || len(r.Matches) != tc.matches {
				t.Errorf("got %s, recommended %q, %d matches; want %s, %q, %d",
					r.State, r.Recommended, len(r.Matches), tc.state, tc.recommended, tc.matches)
			}
		})
	}
}

func TestScanAndIdentIn(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, b []byte) {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Firmware-like: binary, other strings, then the description
	hs := append([]byte{0x00, 0x20, 0x00, 0x20, 0xff}, []byte("RCC_48MHzClockSourceConfig\x00")...)
	hs = append(hs, []byte("\x00\x01MMDVM_HS_Hat-v1.6.1 20261116 14.7456MHz ADF7021 FW by CA6JAU (M17) GitID #02e5646\x00\xfe")...)
	write("mmdvm_hs/MMDVM_HS_Hat.bin", hs)
	write("cc1200/CC1200_HAT-fw.bin", []byte("\x10\x00CC1200-HAT 420-450 MHz\nFW v2.3 by Wojciech SP5WWP\x00\x00"))
	write("mmdvm/F446RE.elf", []byte("MMDVM 20251014 12.0000 MHz GitID #9267a90"))
	write("mmdvm/blank.bin", []byte{0, 1, 2, 3})

	imgs, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []Image{
		{"cc1200/CC1200_HAT-fw.bin", "CC1200-HAT 420-450 MHz / FW v2.3 by Wojciech SP5WWP"},
		{"mmdvm/blank.bin", ""},
		{"mmdvm_hs/MMDVM_HS_Hat.bin", "MMDVM_HS_Hat-v1.6.1 20261116 14.7456MHz ADF7021 FW by CA6JAU (M17) GitID #02e5646"},
	}
	if len(imgs) != len(want) {
		t.Fatalf("got %v, want %v", imgs, want)
	}
	for i := range want {
		if imgs[i] != want[i] {
			t.Errorf("image %d: got %+v, want %+v", i, imgs[i], want[i])
		}
	}

	if imgs, err := Scan(filepath.Join(dir, "missing")); err != nil || len(imgs) != 0 {
		t.Errorf("missing directory: %v, %v", imgs, err)
	}
}

func TestFor(t *testing.T) {
	got := For(Families("mmdvm"), catalog)
	if len(got) != 8 || got[0].Family() != "mmdvm_hs" || got[len(got)-1].Family() != "mmdvm" {
		t.Errorf("For(mmdvm) = %v", got)
	}
	if got := For(Families("sx1255"), catalog); len(got) != 0 {
		t.Errorf("For(sx1255) = %v", got)
	}
}

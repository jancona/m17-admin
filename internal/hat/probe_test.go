package hat

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

// fakePort answers requests as a modem at one speed would.
type fakePort struct {
	baud   int
	answer func(baud int, req []byte) []byte
	out    bytes.Buffer
}

func (p *fakePort) Write(b []byte) (int, error) {
	if r := p.answer(p.baud, b); r != nil {
		p.out.Write(r)
	}
	return len(b), nil
}

// Read returns 0 bytes when nothing is waiting, as a serial port does when
// its read timeout expires
func (p *fakePort) Read(b []byte) (int, error) {
	if p.out.Len() == 0 {
		return 0, nil
	}
	return p.out.Read(b)
}
func (p *fakePort) Close() error                       { return nil }
func (p *fakePort) ResetInputBuffer() error            { return nil }
func (p *fakePort) SetReadTimeout(time.Duration) error { return nil }

func opener(answer func(int, []byte) []byte) Opener {
	return func(path string, baud int) (Port, error) {
		return &fakePort{baud: baud, answer: answer}, nil
	}
}

// mmdvmV1 is a protocol 1 GetVersion reply carrying desc.
func mmdvmV1(desc string) []byte {
	b := []byte{0xE0, byte(4 + len(desc)), 0x00, 0x01}
	return append(b, desc...)
}

// cc1200Reply is a CMD_GET_IDENT reply carrying t.
func cc1200Reply(t string) []byte {
	n := 3 + len(t)
	return append([]byte{0x80, byte(n), byte(n >> 8)}, t...)
}

const dualHat = "MMDVM_HS_Dual_Hat-v1.6.1 20261116 14.7456MHz dual ADF7021 FW by CA6JAU (M17)"

func TestProbeMMDVMAt115200(t *testing.T) {
	r, err := Probe("/dev/ttyAMA0", opener(func(baud int, req []byte) []byte {
		if baud == 115200 && bytes.Equal(req, mmdvmVersion) {
			return append([]byte{0x00, 0xFF}, mmdvmV1(dualHat+"\x00\x00")...) // noise before, NULs after
		}
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if r.Type != "mmdvm" || r.Ident != dualHat || r.Speed != 115200 {
		t.Errorf("got %+v", r)
	}
}

func TestProbeMMDVMAt460800(t *testing.T) {
	r, _ := Probe("/dev/ttyAMA0", opener(func(baud int, req []byte) []byte {
		if baud == 460800 && bytes.Equal(req, mmdvmVersion) {
			return mmdvmV1("MMDVM 20260101 (M17)")
		}
		return nil
	}))
	if r.Type != "mmdvm" || r.Speed != 460800 {
		t.Errorf("got %+v", r)
	}
}

func TestProbeMMDVMProtocol2(t *testing.T) {
	frame := []byte{0xE0, 0, 0x00, 0x02}
	frame = append(frame, make([]byte, 19)...) // capabilities, CPU, UDID
	frame = append(frame, "MMDVM_HS-v2.0.0 (M17)"...)
	frame[1] = byte(len(frame))
	r, _ := Probe("x", opener(func(baud int, req []byte) []byte {
		if baud == 115200 && bytes.Equal(req, mmdvmVersion) {
			return frame
		}
		return nil
	}))
	if r.Ident != "MMDVM_HS-v2.0.0 (M17)" {
		t.Errorf("protocol 2: %+v", r)
	}
}

func TestProbeCC1200(t *testing.T) {
	r, _ := Probe("x", opener(func(baud int, req []byte) []byte {
		if baud == 460800 && bytes.Equal(req, cc1200Ident) {
			return cc1200Reply("CC1200-HAT 420-450 MHz\nFW v2.3 by Wojciech SP5WWP")
		}
		return nil
	}))
	if r.Type != "cc1200" || r.Ident != "CC1200-HAT 420-450 MHz / FW v2.3 by Wojciech SP5WWP" || r.Speed != 460800 {
		t.Errorf("got %+v", r)
	}
}

func TestProbeNothing(t *testing.T) {
	r, err := Probe("x", opener(func(int, []byte) []byte { return []byte{0xE0, 0x05, 0x00, 0x01, 0xFF} }))
	if err != nil || r.Type != "" {
		t.Errorf("garbage reply: %+v, %v", r, err)
	}
}

func TestProbeOpenError(t *testing.T) {
	_, err := Probe("/dev/nope", func(string, int) (Port, error) { return nil, errors.New("no such port") })
	if err == nil {
		t.Error("an unopenable port should be an error, not 'nothing answered'")
	}
}

func TestText(t *testing.T) {
	if text([]byte{'a', 'b', 0x01, 'c', 'd'}) != "" {
		t.Error("control characters accepted")
	}
	if text([]byte("abc")) != "" {
		t.Error("too short accepted")
	}
}

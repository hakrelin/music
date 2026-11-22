package qrcode

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEncodeAndSVG(t *testing.T) {
	samples := []string{
		"hello",
		"https://music.163.com/login?codekey=7f4b2a19-8a3c-4d1e-91c2-5b6d7e8f9012",
		"https://passport.bilibili.com/h5-app/passport/login/scan?navhide=1&qrcode_key=1234567890abcdef1234567890abcdef&from=main-fe-header",
		strings.Repeat("A", 220), // Version >= 10 (16-bit byte length indicator)
	}
	for _, s := range samples {
		m, err := Encode(s)
		if err != nil {
			t.Fatalf("Encode(%d bytes) failed: %v", len(s), err)
		}
		if m.Size != m.Version*4+17 {
			t.Fatalf("invalid size %d for version %d", m.Size, m.Version)
		}
		uri, err := SVGDataURL(s)
		if err != nil {
			t.Fatalf("SVGDataURL failed: %v", err)
		}
		if !strings.HasPrefix(uri, "data:image/svg+xml;base64,") {
			t.Fatalf("unexpected data URI prefix")
		}
	}
	if _, err := Encode(""); err == nil {
		t.Fatal("empty text should fail")
	}
}

// TestDecodeWithOpenCV 用独立第三方解码器（OpenCV）客观核验二维码可扫描性。
func TestDecodeWithOpenCV(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available")
	}
	check := exec.Command(py, "-c", "import cv2")
	if err := check.Run(); err != nil {
		t.Skip("python3 cv2 not available")
	}

	samples := []string{
		"https://music.163.com/login?codekey=7f4b2a19-8a3c-4d1e-91c2-5b6d7e8f9012",
		"https://passport.bilibili.com/h5-app/passport/login/scan?navhide=1&qrcode_key=1234567890abcdef1234567890abcdef",
		"https://passport.bilibili.com/h5-app/passport/login/scan?navhide=1&qrcode_key=" + strings.Repeat("a1", 70),
	}
	dir := t.TempDir()
	for i, want := range samples {
		m, err := Encode(want)
		if err != nil {
			t.Fatalf("Encode(%q): %v", want, err)
		}
		const scale = 6
		const quiet = 4
		dim := (m.Size + quiet*2) * scale
		img := image.NewGray(image.Rect(0, 0, dim, dim))
		for y := 0; y < dim; y++ {
			for x := 0; x < dim; x++ {
				img.SetGray(x, y, color.Gray{Y: 255})
			}
		}
		for y := 0; y < m.Size; y++ {
			for x := 0; x < m.Size; x++ {
				if !m.Modules[y][x] {
					continue
				}
				for dy := 0; dy < scale; dy++ {
					for dx := 0; dx < scale; dx++ {
						img.SetGray((x+quiet)*scale+dx, (y+quiet)*scale+dy, color.Gray{Y: 0})
					}
				}
			}
		}
		p := filepath.Join(dir, "qr"+string(rune('0'+i))+".png")
		f, err := os.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			f.Close()
			t.Fatal(err)
		}
		f.Close()

		out, err := exec.Command(py, "-c",
			"import sys, cv2; img=cv2.imread(sys.argv[1]); d,_,_=cv2.QRCodeDetector().detectAndDecode(img); sys.stdout.write(d)",
			p).CombinedOutput()
		if err != nil {
			t.Fatalf("cv2 decode failed: %v (%s)", err, string(out))
		}
		if got := string(out); got != want {
			t.Fatalf("QR V%d mismatch:\n want=%q\n  got=%q", m.Version, want, got)
		}
	}
}

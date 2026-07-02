package silk

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDebugDiffImage(t *testing.T) {
	a, dir := os.Getenv("DEBUG_A"), os.Getenv("DEBUG_OUT")
	if a == "" || dir == "" {
		t.Skip("set DEBUG_A/DEBUG_OUT")
	}
	in, _ := os.ReadFile(a)
	out, err := Optimize(in, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "b.svg"), out, 0o644)
	render := func(svg, name string) *image.NRGBA {
		pngf := filepath.Join(dir, name+".png")
		if msg, err := exec.Command("resvg", "--width", "512", svg, pngf).CombinedOutput(); err != nil {
			t.Fatalf("resvg: %v %s", err, msg)
		}
		f, _ := os.Open(pngf)
		defer f.Close()
		img, err := png.Decode(f)
		if err != nil {
			t.Fatal(err)
		}
		n := image.NewNRGBA(img.Bounds())
		for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
			for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
				n.Set(x, y, img.At(x, y))
			}
		}
		return n
	}
	ia, ib := render(a, "a"), render(filepath.Join(dir, "b.svg"), "b")
	d := image.NewNRGBA(ia.Bounds())
	var wx, wy, wv int
	for y := 0; y < ia.Bounds().Dy(); y++ {
		for x := 0; x < ia.Bounds().Dx(); x++ {
			i := ia.PixOffset(x, y)
			m := 0
			for c := 0; c < 4; c++ {
				v := int(ia.Pix[i+c]) - int(ib.Pix[i+c])
				if v < 0 {
					v = -v
				}
				if v > m {
					m = v
				}
			}
			if m > wv {
				wx, wy, wv = x, y, m
			}
			g := uint8(min(255, m*8))
			d.Set(x, y, color.NRGBA{g, g, g, 255})
		}
	}
	f, _ := os.Create(filepath.Join(dir, "diff.png"))
	png.Encode(f, d)
	f.Close()
	fmt.Printf("worst pixel (%d,%d) diff %d\n", wx, wy, wv)
}

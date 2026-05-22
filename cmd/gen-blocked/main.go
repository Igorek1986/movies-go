// Generates internal/api/blocked.png — prohibition sign (red ring + diagonal bar).
// Run once: go run ./cmd/gen-blocked/
package main

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

func main() {
	const w, h = 300, 450
	img := image.NewRGBA(image.Rect(0, 0, w, h))

	cx, cy := float64(w)/2, float64(h)/2
	outerR := float64(w)/2 * 0.86
	innerR := outerR * 0.62
	barHalf := outerR * 0.19 // half-width of the diagonal bar

	red := color.RGBA{R: 220, G: 38, B: 38, A: 255}
	bg := color.RGBA{R: 18, G: 18, B: 18, A: 255}

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx := float64(x) - cx
			dy := float64(y) - cy
			dist := math.Sqrt(dx*dx + dy*dy)

			// Smooth edges via supersampling (2x2)
			var redSum float64
			for sy := 0; sy < 2; sy++ {
				for sx := 0; sx < 2; sx++ {
					sdx := dx + float64(sx)*0.5 - 0.25
					sdy := dy + float64(sy)*0.5 - 0.25
					sdist := math.Sqrt(sdx*sdx + sdy*sdy)

					inRing := sdist <= outerR && sdist >= innerR

					// Diagonal bar: rotate 45° and check half-width
					angle := math.Pi / 4
					rx := sdx*math.Cos(-angle) - sdy*math.Sin(-angle)
					inBar := math.Abs(rx) <= barHalf && sdist <= outerR*0.94

					if inRing || inBar {
						redSum++
					}
				}
			}

			_ = dist // used via sdist inside supersampling loop
			alpha := redSum / 4.0
			c := color.RGBA{
				R: uint8(float64(red.R)*alpha + float64(bg.R)*(1-alpha)),
				G: uint8(float64(red.G)*alpha + float64(bg.G)*(1-alpha)),
				B: uint8(float64(red.B)*alpha + float64(bg.B)*(1-alpha)),
				A: 255,
			}
			img.Set(x, y, c)
		}
	}

	f, err := os.Create("internal/api/blocked.png")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		panic(err)
	}
}

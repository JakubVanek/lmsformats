// Package rgf provides functionality for reading and writing EV3 RGF files.
package rgf

import (
	"errors"
	"image"
	"image/color"
	"io"
)

// Bitmap represents RGF file contents.
// It is a compact version of the XBM format.
type Bitmap struct {
	Width     uint8
	Height    uint8
	PixelRows [][]uint8
}

// Create a new RGF Bitmap.
// This allocates memory for the bitmap data.
func Create(Width, Height uint8) (bmp *Bitmap) {
	bmp = &Bitmap{
		Width:     Width,
		Height:    Height,
		PixelRows: make([][]uint8, Height)}
	stride := (int(bmp.Width) + 7) / 8
	for y := 0; y < int(bmp.Height); y++ {
		bmp.PixelRows[y] = make([]uint8, stride)
	}
	return
}

// Get bitmap pixel data.
// Returns true if pixel at [x, y] is to be black.
func (img *Bitmap) Get(x, y uint8) bool {
	if x >= img.Width || y >= img.Height {
		return false
	}
	ByteIndex := x / 8
	BitIndex := x % 8
	return (img.PixelRows[y][ByteIndex]>>BitIndex)&0x01 == 0x01
}

// Set pixel at [x, y] depending on the black bool flag.
func (img *Bitmap) Set(x, y uint8, black bool) {
	if x >= img.Width || y >= img.Height {
		return
	}
	ByteIndex := x / 8
	BitIndex := x % 8
	if black {
		img.PixelRows[y][ByteIndex] |= 1 << BitIndex
	} else {
		img.PixelRows[y][ByteIndex] &= 0xFF ^ (1 << BitIndex)
	}
}

// Write RGF binary file to a stream.
func (img *Bitmap) Write(w io.Writer) (int, error) {
	header := [2]byte{byte(img.Width), byte(img.Height)}
	n, err := w.Write(header[:])
	if err != nil {
		return n, err
	}
	for _, row := range img.PixelRows {
		n2, err := w.Write(row)
		n += n2
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// Read RGF binary file from a stream.
func Read(r io.Reader) (*Bitmap, error) {
	header := [2]uint8{}
	n, err := r.Read(header[:])
	if err != nil {
		return nil, err
	} else if n != 2 {
		return nil, errors.New("Bitmap header is truncated")
	}
	w := header[0]
	h := header[1]
	stride := (int(w) + 7) / 8
	bmp := Create(w, h)
	for _, v := range bmp.PixelRows {
		n, err := r.Read(v)
		if err != nil {
			return nil, err
		}
		if n < stride {
			return nil, errors.New("Bitmap is truncated")
		}
	}
	return bmp, nil
}

// Bounds returns 'image' pkg compatible image bounding box.
func (img *Bitmap) Bounds() image.Rectangle {
	return image.Rect(0, 0, int(img.Width), int(img.Height))
}

// At returns 'image' pkg compatible color at [x, y].
func (img *Bitmap) At(x, y int) color.Color {
	if x < 0 || x >= int(img.Width) || y < 0 || y >= int(img.Height) {
		return color.White
	}
	black := img.Get(uint8(x), uint8(y))
	if black {
		return color.Black
	} else {
		return color.White
	}
}

// ColorModel returns 'image' pkg compatible color model.
func (img *Bitmap) ColorModel() color.Model {
	return color.RGBAModel
}

// colorToGray converts any color to a grayscale color.
func colorToGray(col color.Color) color.Gray {
	return color.GrayModel.Convert(col).(color.Gray)
}

// createFromImageSize clips existing image dimensions and
// creates a bitmap with them.
func createFromImageSize(src image.Image) (bmp *Bitmap, w, h int) {
	w = src.Bounds().Max.X - src.Bounds().Min.X
	h = src.Bounds().Max.Y - src.Bounds().Min.Y
	if w < 0 {
		w = 0
	} else if w > 255 {
		w = 255
	}
	if h < 0 {
		h = 0
	} else if h > 255 {
		h = 255
	}
	bmp = Create(uint8(w), uint8(h))
	return
}

// ByThreshold loads an Image to a Bitmap by checking all
// pixels against a threshold value.
func ByThreshold(src image.Image, threshold uint8) *Bitmap {
	bmp, w, h := createFromImageSize(src)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			col := src.At(x+src.Bounds().Min.X, y+src.Bounds().Min.Y)
			lum := colorToGray(col).Y
			bmp.Set(uint8(x), uint8(y), lum < threshold)
		}
	}
	return bmp
}

// floatGrayMap converts Image to a flat normalized float64 array.
// The resulting array has w*h length. Rows are laid sequentially top-down,
// columns within them are laid left-to-right.
func floatGrayMap(src image.Image, w, h int) (grayMap []float64) {
	grayMap = make([]float64, w*h)

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			col := src.At(x+src.Bounds().Min.X, y+src.Bounds().Min.Y)
			lum := colorToGray(col).Y
			grayMap[y*w+x] = float64(lum) / 255.0
		}
	}
	return
}

// floydSteinbergDither applies Floyd-Steinberg dithering
// to a float64 gray map.
// Algorithm at https://en.wikipedia.org/wiki/Floyd%E2%80%93Steinberg_dithering
func floydSteinbergDither(grayMap []float64, w, h int) {
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			orig := grayMap[y*w+x]
			new := 0.0
			if orig >= 0.5 {
				new = 1.0
			}
			grayMap[y*w+x] = new
			quantError := orig - new

			if x != w-1 {
				grayMap[(y+0)*w+x+1] += quantError * 7 / 16
			}
			if y != h-1 {
				grayMap[(y+1)*w+x-1] += quantError * 3 / 16
				grayMap[(y+1)*w+x+0] += quantError * 5 / 16
				if x != w-1 {
					grayMap[(y+1)*w+x+1] += quantError * 1 / 16
				}
			}
		}
	}
}

// fillByGrayMap imports a float gray map to a Bitmap via
// a 50% threshold value.
func fillByGrayMap(src []float64, dst *Bitmap, w, h int) {
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			white := src[y*w+x] > 0.5
			dst.Set(uint8(x), uint8(y), !white)
		}
	}
}

// ByThreshold loads an Image to a Bitmap by performing
// Floyd-Steinberg dithering on the grayscale image data.
func ByDither(src image.Image) *Bitmap {
	bmp, w, h := createFromImageSize(src)
	grayMap := floatGrayMap(src, w, h)
	floydSteinbergDither(grayMap, w, h)
	fillByGrayMap(grayMap, bmp, w, h)
	return bmp
}
// Package media turns uploads into web-ready images: rotated upright, resized
// into a few widths and re-encoded, which also strips metadata such as the GPS
// location phones embed in photos.
package media

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"sitesmith/internal/store"
)

const MaxUpload = 30 << 20

// Widths we generate. Anything larger than maxWidth is scaled down.
var widths = []int{480, 960, 1600}

const maxWidth = 2400

var ErrUnsupported = errors.New("only JPEG, PNG, GIF and WebP images are supported")

// Process decodes an upload and writes its renditions into dir.
func Process(dir string, data []byte, originalName string) (*store.Media, error) {
	ctype := http.DetectContentType(data)
	var ext string
	switch ctype {
	case "image/jpeg", "image/webp":
		ext = ".jpg"
	case "image/png":
		ext = ".png"
	case "image/gif":
		ext = ".gif"
	default:
		return nil, ErrUnsupported
	}

	stem := store.Slugify(strings.TrimSuffix(filepath.Base(originalName), filepath.Ext(originalName)))
	if len(stem) > 40 {
		stem = stem[:40]
	}
	rnd := make([]byte, 4)
	_, _ = rand.Read(rnd)
	m := &store.Media{Name: stem + "-" + hex.EncodeToString(rnd), Ext: ext, OriginalName: filepath.Base(originalName)}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	// GIFs are kept as-is so animations survive.
	if ext == ".gif" {
		cfg, err := gif.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		m.Width, m.Height, m.Sizes = cfg.Width, cfg.Height, []int{cfg.Width}
		return m, os.WriteFile(filepath.Join(dir, m.File(0)), data, 0o644)
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("couldn't read image: %w", err)
	}
	if ctype == "image/jpeg" {
		img = applyOrientation(img, jpegOrientation(data))
	}
	if ext == ".png" && isOpaque(img) && img.Bounds().Dx()*img.Bounds().Dy() > 1_000_000 {
		// Large opaque PNGs are almost always photos saved in the wrong format.
		ext, m.Ext = ".jpg", ".jpg"
	}

	full := img
	if img.Bounds().Dx() > maxWidth {
		full = resize(img, maxWidth)
	}
	m.Width, m.Height = full.Bounds().Dx(), full.Bounds().Dy()

	src := full
	for i := len(widths) - 1; i >= 0; i-- {
		w := widths[i]
		if w >= m.Width {
			continue
		}
		// Resize from the next size up: faster, and quality is just as good.
		src = resize(src, w)
		if err := save(filepath.Join(dir, m.File(w)), src, ext); err != nil {
			return nil, err
		}
		m.Sizes = append([]int{w}, m.Sizes...)
	}
	m.Sizes = append(m.Sizes, m.Width)
	if err := save(filepath.Join(dir, m.File(m.Width)), full, ext); err != nil {
		return nil, err
	}
	return m, nil
}

// Remove deletes every rendition of a media item.
func Remove(dir string, m *store.Media) {
	for _, f := range m.Files() {
		_ = os.Remove(filepath.Join(dir, f))
	}
}

func resize(img image.Image, width int) image.Image {
	b := img.Bounds()
	height := max(1, b.Dy()*width/b.Dx())
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	return dst
}

func save(path string, img image.Image, ext string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if ext == ".png" {
		err = (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(f, img)
	} else {
		err = jpeg.Encode(f, flatten(img), &jpeg.Options{Quality: 82})
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// flatten puts transparent images on white before JPEG encoding.
func flatten(img image.Image) image.Image {
	if isOpaque(img) {
		return img
	}
	dst := image.NewRGBA(img.Bounds())
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), img, img.Bounds().Min, draw.Over)
	return dst
}

func isOpaque(img image.Image) bool {
	if o, ok := img.(interface{ Opaque() bool }); ok {
		return o.Opaque()
	}
	return false
}

// jpegOrientation reads the EXIF orientation tag (1–8), or 1 if absent.
// Phones store photos sideways and rely on this tag to display them upright.
func jpegOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}
	i := 2
	for i+4 <= len(data) {
		if data[i] != 0xFF {
			return 1
		}
		marker := data[i+1]
		size := int(binary.BigEndian.Uint16(data[i+2:]))
		if marker == 0xDA || size < 2 || i+2+size > len(data) {
			return 1
		}
		seg := data[i+4 : i+2+size]
		if marker == 0xE1 && len(seg) > 14 && string(seg[:6]) == "Exif\x00\x00" {
			return tiffOrientation(seg[6:])
		}
		i += 2 + size
	}
	return 1
}

func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	off := int(bo.Uint32(t[4:]))
	if off+2 > len(t) {
		return 1
	}
	n := int(bo.Uint16(t[off:]))
	for k := 0; k < n; k++ {
		e := off + 2 + k*12
		if e+12 > len(t) {
			return 1
		}
		if bo.Uint16(t[e:]) == 0x0112 {
			v := int(bo.Uint16(t[e+8:]))
			if v >= 1 && v <= 8 {
				return v
			}
			return 1
		}
	}
	return 1
}

func applyOrientation(img image.Image, o int) image.Image {
	if o <= 1 {
		return img
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	swap := o >= 5
	dw, dh := w, h
	if swap {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch o {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			dst.Set(dx, dy, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

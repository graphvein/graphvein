package main

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"strings"
)

// cropBox is a region of a screenshot in source pixels. A feature row
// shows this part of the screenshot (the part that tells its story) and
// links to the whole image; the manifest entry, and so the current-build
// check, stays the source file's.
type cropBox struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

// cropName is the output name of a screenshot's cropped derivative.
func cropName(file string) string {
	return strings.TrimSuffix(file, ".png") + "-crop.png"
}

// sourceOfCrop returns the screenshot a cropped output was made from.
func sourceOfCrop(name string) (string, bool) {
	if !strings.HasSuffix(name, "-crop.png") {
		return "", false
	}
	return strings.TrimSuffix(name, "-crop.png") + ".png", true
}

// makeCrops writes the cropped derivative of every screenshot whose
// manifest entry has a crop box. Cropping happens in the build, with the
// standard library, so the derivative can never drift from its source.
func (s *site) makeCrops() error {
	for _, f := range s.c.Media.Files {
		if f.Crop == nil {
			continue
		}
		src, ok := s.out["media/"+f.File]
		if !ok {
			return fmt.Errorf("media.json %s: crop of a file that is not in site/media", f.File)
		}
		img, err := png.Decode(bytes.NewReader(src))
		if err != nil {
			return fmt.Errorf("decoding %s: %w", f.File, err)
		}
		c := f.Crop
		r := image.Rect(c.X, c.Y, c.X+c.W, c.Y+c.H)
		if c.W <= 0 || c.H <= 0 || !r.In(img.Bounds()) {
			return fmt.Errorf("media.json %s: crop %v lies outside the image %v", f.File, r, img.Bounds())
		}
		sub, ok := img.(interface {
			SubImage(image.Rectangle) image.Image
		})
		if !ok {
			return fmt.Errorf("%s: this PNG kind cannot be cropped", f.File)
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, sub.SubImage(r)); err != nil {
			return fmt.Errorf("encoding the crop of %s: %w", f.File, err)
		}
		s.out["media/"+cropName(f.File)] = buf.Bytes()
	}
	return nil
}

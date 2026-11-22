package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/jpeg"
	"io"
)

const (
	previewMax  = 32 << 20
	webpExifMax = 4 << 20
	webpChunks  = 64
)

type preview struct {
	at, n       int64
	w, h        int
	orientation int
}

// findPreview returns the largest decodable JPEG preview that the image file in r carries: the Exif thumbnail of
// a JPEG, WebP or HEIF file, or a preview of a camera raw file (TIFF-based, CR3 or RAF). Its orientation is the one the file gives for it, else the one of the preview itself.
// Previews over FitsDecodeLimit or 32 MiB do not count; ok is false when none is left.
func findPreview(r io.ReadSeeker) (best preview, ok bool) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return preview{}, false
	}
	var head [16]byte
	n, _ := io.ReadFull(r, head[:])
	h := head[:n]
	var found []preview
	switch {
	case bytes.HasPrefix(h, []byte{0xFF, 0xD8}):
		found = jpegPreviews(r)
	case len(h) >= 12 && string(h[:4]) == "RIFF" && string(h[8:12]) == "WEBP":
		found = webpPreviews(r)
	case len(h) >= 12 && string(h[4:12]) == "ftypcrx ":
		found = cr3Previews(r)
	case bytes.HasPrefix(h, []byte("FUJIFILMCCD-RAW")):
		found = rafPreviews(r)
	case len(h) >= 8 && (string(h[:4]) == "II*\x00" || string(h[:4]) == "MM\x00*" || string(h[:4]) == "IIRO" || string(h[:4]) == "IIU\x00" || string(h[:4]) == "MMOR"):
		found = tiffRawPreviews(r, h)
	case len(h) >= 8 && string(h[4:8]) == "ftyp":
		if p, ok := heifExifPreview(r); ok {
			found = []preview{p}
		}
	}
	for _, p := range found {
		if p.n <= 0 || p.n > previewMax {
			continue
		}
		cfg, format, err := image.DecodeConfig(sectionOf(r, p))
		if err != nil || format != "jpeg" || !FitsDecodeLimit(cfg.Width, cfg.Height) || cfg.Width*cfg.Height <= best.w*best.h {
			continue
		}
		p.w, p.h = cfg.Width, cfg.Height
		if p.orientation == 0 {
			p.orientation = ExifOrientation(sectionOf(r, p))
		}
		best, ok = p, true
	}
	return best, ok
}

// sectionOf returns a reader over the bytes of preview p in r.
func sectionOf(r io.ReadSeeker, p preview) *io.SectionReader {
	return io.NewSectionReader(seekReaderAt{r}, p.at, p.n)
}

type seekReaderAt struct{ r io.ReadSeeker }

// ReadAt reads len(b) bytes of the underlying reader from offset off.
func (s seekReaderAt) ReadAt(b []byte, off int64) (int, error) {
	if _, err := s.r.Seek(off, io.SeekStart); err != nil {
		return 0, err
	}
	return io.ReadFull(s.r, b)
}

// encodePreview decodes preview p of r and returns it as a JPEG whose long edge is at most maxEdge,
// turned upright by the orientation of p.
func encodePreview(ctx context.Context, r io.ReadSeeker, p preview, maxEdge int) ([]byte, error) {
	data, err := io.ReadAll(sectionOf(r, p))
	if err != nil {
		return nil, err
	}
	img, err := scaledImage(ctx, bytes.NewReader(data), maxEdge)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, Orient(img, p.orientation), &jpeg.Options{Quality: ThumbnailQuality}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// jpegPreviews returns the Exif thumbnail of the JPEG in r.
func jpegPreviews(r io.ReadSeeker) []preview {
	t, at, ok := exifTIFF(r)
	if !ok {
		return nil
	}
	off, n, ok := tiffThumbnail(t)
	if !ok {
		return nil
	}
	return []preview{{at: at + int64(off), n: int64(n), orientation: tiffOrientation(t)}}
}

// webpPreviews returns the Exif thumbnail of the EXIF chunk of the WebP file in r.
func webpPreviews(r io.ReadSeeker) []preview {
	pos := int64(12)
	for range webpChunks {
		if _, err := r.Seek(pos, io.SeekStart); err != nil {
			return nil
		}
		var head [8]byte
		if _, err := io.ReadFull(r, head[:]); err != nil {
			return nil
		}
		size := int64(binary.LittleEndian.Uint32(head[4:]))
		if string(head[:4]) == "EXIF" {
			if size > webpExifMax {
				return nil
			}
			t := make([]byte, size)
			if _, err := io.ReadFull(r, t); err != nil {
				return nil
			}
			at := pos + 8
			if bytes.HasPrefix(t, []byte("Exif\x00\x00")) {
				t, at = t[6:], at+6
			}
			off, n, ok := tiffThumbnail(t)
			if !ok {
				return nil
			}
			return []preview{{at: at + int64(off), n: int64(n), orientation: tiffOrientation(t)}}
		}
		pos += 8 + size + size%2
	}
	return nil
}

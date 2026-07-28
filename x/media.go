package x

import (
	"fmt"
	"strconv"
	"strings"
)

// media.go is surface 6, which is the one surface that serves bytes rather than
// facts (spec 3003 doc 01 section 6, doc 05 section 2).
//
// It needs no credential and it has no JSON in it. What it does have is a
// convention: a photo URL carries the size in it, and asking for the wrong one
// silently gets you a thumbnail of a picture you wanted at full resolution. The
// URL a record carries is whatever the plane that found it happened to say, so
// the size is decided here, once, at the point of fetch.

// MediaSizes are the photo sizes pbs.twimg.com serves, smallest first. orig is
// the file as it was uploaded; the rest are X's re-encodes.
var MediaSizes = []string{"thumb", "small", "medium", "large", "orig"}

// DefaultMediaSize is what a download gets when nobody said. The point of
// downloading a photo is to have the photo, so it is the original.
const DefaultMediaSize = "orig"

// MediaURL is where to fetch one media item.
//
// A photo is one file at five sizes, so size picks the size. A video is several
// encodings of one file, so size means nothing and variant picks the encoding;
// with no variant asked for it is the highest-bitrate MP4, which is the one a
// player will take.
func MediaURL(m Media, size, variant string) (string, error) {
	if len(m.Variants) > 0 {
		return pickVariant(m.Variants, variant)
	}
	if variant != "" {
		return "", fmt.Errorf("%s media has no variants to pick from", mediaKind(m))
	}
	return PhotoURL(m.URL, size)
}

func mediaKind(m Media) string {
	if m.Type == "" {
		return "this"
	}
	return m.Type
}

// PhotoURL is a pbs.twimg.com photo at a given size.
//
// X's own pages write it as ?format=jpg&name=large these days, and the older
// .jpg:large still answers. This writes the current form, because a URL the tool
// hands back should be one the user can paste today.
//
// A URL that is not a pbs photo is returned untouched: a video thumbnail on
// video.twimg.com has no sizes, and rewriting it would break it.
func PhotoURL(u, size string) (string, error) {
	if u == "" {
		return "", fmt.Errorf("no url on this media item")
	}
	if size == "" {
		size = DefaultMediaSize
	}
	if err := CheckSize(size); err != nil {
		return "", err
	}
	if !strings.Contains(u, "pbs.twimg.com/") {
		return u, nil
	}
	base, ext := splitPhotoURL(u)
	if ext == "" {
		// No extension to name the format with, so leave the URL alone rather
		// than guess jpg at something that might be a png.
		return u, nil
	}
	return base + "?format=" + ext + "&name=" + size, nil
}

// splitPhotoURL strips whatever size the URL already carried, in either of the
// two forms X has used, and returns the bare path and the format.
func splitPhotoURL(u string) (base, ext string) {
	if i := strings.IndexByte(u, '?'); i >= 0 {
		// The query names the format, and it is the only place it is named once
		// the extension has been dropped from the path.
		q := u[i+1:]
		u = u[:i]
		for _, kv := range strings.Split(q, "&") {
			if v, ok := strings.CutPrefix(kv, "format="); ok {
				ext = v
			}
		}
	}
	// The :large suffix is on the last path segment, so the colon in https: is
	// not a candidate.
	if i := strings.LastIndexByte(u, '/'); i >= 0 {
		if j := strings.IndexByte(u[i:], ':'); j >= 0 {
			u = u[:i+j]
		}
	}
	if i := strings.LastIndexByte(u, '.'); i > strings.LastIndexByte(u, '/') {
		return u[:i], u[i+1:]
	}
	return u, ext
}

// CheckSize rejects a size X does not serve. A caller checks it once before the
// first request, because a typo in --size is worth hearing about before a page
// of timeline has been fetched for it.
func CheckSize(size string) error {
	for _, v := range MediaSizes {
		if v == size {
			return nil
		}
	}
	return fmt.Errorf("no size %q; there is %s", size, strings.Join(MediaSizes, ", "))
}

// pickVariant chooses one encoding of a video.
//
// With nothing asked for it is the highest-bitrate MP4: a video timeline also
// ships an m3u8 playlist, which is the right answer for a player and the wrong
// one for a file on disk. want matches a resolution, a bitrate, or any other
// distinguishing part of the variant's URL or content type.
func pickVariant(vs []Variant, want string) (string, error) {
	if want != "" {
		for _, v := range vs {
			if v.URL == "" {
				continue
			}
			if strings.Contains(v.URL, want) || v.ContentType == want ||
				strconv.Itoa(v.Bitrate) == want {
				return v.URL, nil
			}
		}
		return "", fmt.Errorf("no variant matching %q; there is %s", want, variantList(vs))
	}
	best, rate := "", -1
	for _, v := range vs {
		if v.URL == "" || !strings.Contains(v.ContentType, "mp4") {
			continue
		}
		if v.Bitrate > rate {
			best, rate = v.URL, v.Bitrate
		}
	}
	if best != "" {
		return best, nil
	}
	// No MP4 at all, so whatever there is beats nothing.
	for _, v := range vs {
		if v.URL != "" {
			return v.URL, nil
		}
	}
	return "", fmt.Errorf("this video has variants but none of them has a url")
}

// variantList names what was on offer, so a rejected --variant can be corrected
// without a second command.
func variantList(vs []Variant) string {
	var parts []string
	for _, v := range vs {
		if v.URL == "" {
			continue
		}
		parts = append(parts, VariantName(v))
	}
	if len(parts) == 0 {
		return "nothing"
	}
	return strings.Join(parts, ", ")
}

// VariantName is a variant as a person would name it: the resolution when the
// URL states one, the bitrate otherwise.
func VariantName(v Variant) string {
	if r := resolutionOf(v.URL); r != "" {
		return r
	}
	if v.Bitrate > 0 {
		return strconv.Itoa(v.Bitrate)
	}
	return v.ContentType
}

// resolutionOf is the WxH segment X puts in a video URL, if it has one. An m3u8
// playlist has none, which is one more reason it is not the default.
func resolutionOf(u string) string {
	for _, seg := range strings.Split(u, "/") {
		w, h, ok := strings.Cut(seg, "x")
		if !ok || w == "" || h == "" {
			continue
		}
		if isDigits(w) && isDigits(h) {
			return seg
		}
	}
	return ""
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

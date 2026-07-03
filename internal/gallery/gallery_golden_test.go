package gallery_test

import (
	"testing"

	"github.com/dklKevin/agentforest/internal/canvas"
	"github.com/dklKevin/agentforest/internal/gallery"
	"github.com/dklKevin/agentforest/internal/goldentest"
)

// The reference sheets are pure functions of (kind, width, height, profile) -
// no clock - so a fixed size and the shape-only profile fully pin them. These
// goldens pin nearly every sprite primitive in internal/sprite, and they lock
// the two gallery layout fixes (the settlement caption clear of the first
// building label, and the finished monument board reserved clear of the sheet
// edge) so neither can silently regress.
const (
	galleryW = 160
	galleryH = 42
)

func TestGalleryGolden(t *testing.T) {
	for _, kind := range gallery.Kinds {
		t.Run(kind, func(t *testing.T) {
			got, err := gallery.RenderGallery(kind, galleryW, galleryH, canvas.NoColor)
			if err != nil {
				t.Fatal(err)
			}
			goldentest.Assert(t, kind, got)
		})
	}
}

func TestGalleryUnknownKind(t *testing.T) {
	if _, err := gallery.RenderGallery("nope", galleryW, galleryH, canvas.NoColor); err == nil {
		t.Fatal("expected an error for an unknown gallery kind")
	}
}

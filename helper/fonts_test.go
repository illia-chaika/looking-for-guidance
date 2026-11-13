package main

import (
	"testing"

	chat "github.com/illia-chaika/looking-for-guidance"
	"golang.org/x/image/font/sfnt"
)

func TestBundledFontsCoverRussian(t *testing.T) {
	const alphabet = "АБВГДЕЁЖЗИЙКЛМНОПРСТУФХЦЧШЩЪЫЬЭЮЯабвгдеёжзийклмнопрстуфхцчшщъыьэюя"
	for _, face := range []string{"PTSansNarrow-Regular", "PTSansNarrow-Bold", "PTSans-Regular", "PTSerif-Bold"} {
		t.Run(face, func(t *testing.T) {
			data, err := chat.Addon.ReadFile("looking-for-guidance/Media/" + face + ".ttf")
			if err != nil {
				t.Fatal(err)
			}
			font, err := sfnt.Parse(data)
			if err != nil {
				t.Fatal(err)
			}
			var buffer sfnt.Buffer
			for _, letter := range alphabet {
				glyph, err := font.GlyphIndex(&buffer, letter)
				if err != nil || glyph == 0 {
					t.Errorf("missing %c (%U) in %s: %v", letter, letter, face, err)
				}
			}
		})
	}
}

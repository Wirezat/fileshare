package web

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/Wirezat/fileshare/internal/filetype"
)

var iconGlyphs = map[string]string{
	filetype.IconImage:    "🖼️",
	filetype.IconVideo:    "🎬",
	filetype.IconAudio:    "🎵",
	filetype.IconPDF:      "📕",
	filetype.IconDoc:      "📄",
	filetype.IconSheet:    "📊",
	filetype.IconSlides:   "📽️",
	filetype.IconText:     "📝",
	filetype.IconMarkdown: "📝",
	filetype.IconCode:     "💻",
	filetype.IconArchive:  "📦",
	filetype.IconFile:     "📎",
	filetype.IconFolder:   "📁",
}

// iconGlyph returns the character shown for a filetype icon name; unknown names get the file icon.
func iconGlyph(icon string) string {
	if g, ok := iconGlyphs[icon]; ok {
		return g
	}
	return iconGlyphs[filetype.IconFile]
}

// entryIcon returns the character shown for a listing entry called name; isDir gives the folder icon.
func entryIcon(name string, isDir bool) string {
	if isDir {
		return iconGlyph(filetype.IconFolder)
	}
	return iconGlyph(filetype.Lookup(name).Icon)
}

// loadShareText reads the share page texts (keys starting with "share.") from the de and en locale files
// under assets; the result maps a language to its texts.
func loadShareText(assets string) (map[string]map[string]string, error) {
	out := map[string]map[string]string{}
	for _, lang := range []string{"de", "en"} {
		raw, err := os.ReadFile(filepath.Join(assets, "locales", lang+".json"))
		if err != nil {
			return nil, err
		}
		var all map[string]string
		if err := json.Unmarshal(raw, &all); err != nil {
			return nil, err
		}
		out[lang] = map[string]string{}
		for k, v := range all {
			if strings.HasPrefix(k, "share.") {
				out[lang][k] = v
			}
		}
	}
	return out, nil
}

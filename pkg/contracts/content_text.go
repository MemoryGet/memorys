package contracts

import (
	"sort"
	"strings"
)

// SearchableText returns the deterministic text projection used by index
// preparation. It never invents OCR or transcription when those fields are
// absent; callers retain the original Content as the citation boundary.
func SearchableText(content Content) (string, bool) {
	switch content.Kind {
	case TextBlock:
		return content.Text, strings.TrimSpace(content.Text) != ""
	case TableBlock:
		if content.Table == nil {
			return "", false
		}
		cells := append([]TableCell(nil), content.Table.Cells...)
		sort.SliceStable(cells, func(i, j int) bool {
			if cells[i].Row == cells[j].Row {
				return cells[i].Column < cells[j].Column
			}
			return cells[i].Row < cells[j].Row
		})
		rows := make([]string, 0, content.Table.RowCount)
		currentRow := -1
		current := []string{}
		flush := func() {
			if currentRow >= 0 {
				rows = append(rows, strings.Join(current, "\t"))
			}
			current = nil
		}
		for _, cell := range cells {
			if cell.Row != currentRow {
				flush()
				currentRow = cell.Row
			}
			current = append(current, cell.Text)
		}
		flush()
		text := strings.Join(rows, "\n")
		if content.Table.Caption != nil && strings.TrimSpace(*content.Table.Caption) != "" {
			text = *content.Table.Caption + "\n" + text
		}
		return text, strings.TrimSpace(text) != ""
	case ImageBlock:
		if content.Image == nil {
			return "", false
		}
		parts := []string{}
		if content.Image.Caption != nil && strings.TrimSpace(*content.Image.Caption) != "" {
			parts = append(parts, *content.Image.Caption)
		}
		if content.Image.OCRText != nil && strings.TrimSpace(*content.Image.OCRText) != "" {
			parts = append(parts, *content.Image.OCRText)
		}
		text := strings.Join(parts, "\n")
		return text, strings.TrimSpace(text) != ""
	case AudioBlock:
		if content.Audio == nil {
			return "", false
		}
		parts := make([]string, 0, len(content.Audio.Segments))
		for _, segment := range content.Audio.Segments {
			if strings.TrimSpace(segment.Text) != "" {
				parts = append(parts, segment.Text)
			}
		}
		text := strings.Join(parts, "\n")
		return text, strings.TrimSpace(text) != ""
	default:
		return "", false
	}
}

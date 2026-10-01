// Package text — label.go
//
// Label prediction that mirrors GeoGebra's LabelType + LabelManager.getNextIndexedLabel.
// When a bare command (no "=") is executed, GeoGebra auto-generates a label
// by iterating through the type's character set with increasing indices:
//
//	charset = {'A','B','C',...}
//	pass 0:  A, B, C, ...
//	pass 1:  A_1, B_1, C_1, ...
//	pass 2:  A_2, B_2, C_2, ...
//
// This module predicts that label so the validator can track dependencies
// for bare commands.
package text

import "strings"

// labelCharSets mirrors GeoGebra's LabelType.java character arrays.
// Each entry is the set of base characters for a GeoElement type.
var labelCharSets = map[string][]rune{
	// Points: A, B, C, D, E, F, G, H, I, J, K, L, M, N, O, P, Q, R, S, T, U, V, W, Z
	"Point": {'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 'J', 'K', 'L', 'M', 'N', 'O', 'P', 'Q', 'R', 'S', 'T', 'U', 'V', 'W', 'Z'},
	// Functions: f, g, h, p, q, r, s, t
	"Function": {'f', 'g', 'h', 'p', 'q', 'r', 's', 't'},
	// Lines: f, g, h, i, j, k, l, m, n, p, q, r, s, t, a, b, c, d, e
	"Line": {'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm', 'n', 'p', 'q', 'r', 's', 't', 'a', 'b', 'c', 'd', 'e'},
	// Vectors: u, v, w, a, b, c, d, e, f, g, h, i, j, k, l, m, n, p, q, r, s, t
	"Vector": {'u', 'v', 'w', 'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm', 'n', 'p', 'q', 'r', 's', 't'},
	// Conics (circles, ellipses, parabolas, hyperbolas): c, d, e, f, g, h, k, p, q, r, s, t
	"Conic": {'c', 'd', 'e', 'f', 'g', 'h', 'k', 'p', 'q', 'r', 's', 't'},
	// General lowercase (segments, polygons, curves, etc.): a, b, c, ..., w
	"General": {'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm', 'n', 'o', 'p', 'q', 'r', 's', 't', 'u', 'v', 'w'},
	// Integer sliders: n, i, j, k, l, m
	"IntegerSlider": {'n', 'i', 'j', 'k', 'l', 'm'},
	// Planes: p, q, r
	"Plane": {'p', 'q', 'r'},
}

// labelTypeForCommand maps a command name to its label type key.
// This mirrors how GeoGebra's GeoFactory assigns label types to commands.
// Commands not listed here default to "General".
//
// Keys are UPPERCASE and looked up with strings.ToUpper, because GeoGebra
// command names are case-insensitive (so is the catalog) while a Go map lookup
// is not. With a case-sensitive table, `Midpoint(A,B)` correctly predicted the
// Point label "C" but `midpoint(A,B)` fell through to "General" and took "a" —
// which then collided with a later `a = 5` and raised a bogus dep/redefine.
var labelTypeForCommand = map[string]string{
	// Points
	"POINT":            "Point",
	"MIDPOINT":         "Point",
	"INTERSECT":        "Point",
	"INTERSECTION":     "Point",
	"FOOT":             "Point",
	"ORTHOGONALLINE":   "Point",
	"PARALLELLINE":     "Point",
	"ANGULARBISECTOR":  "Point",
	"LINEBISECTOR":     "Point",
	"CIRCLE":           "Conic",
	"CIRCUMCIRCLE":     "Conic",
	"INCIRCLE":         "Conic",
	"ELLIPTICALSECTOR": "Conic",
	// Lines
	"LINE":    "Line",
	"SEGMENT": "General",
	"RAY":     "General",
	// Polygons
	"POLYGON":        "General",
	"REGULARPOLYGON": "General",
	// Functions
	"CURVE":           "Function",
	"PARAMETRICCURVE": "Function",
	"PLOT":            "Function",
	// Vectors
	"VECTOR": "Vector",
	// Planes
	"PLANE": "Plane",
	// Sliders
	"SLIDER": "General",
	// Integer sliders
	"SEQUENCE": "IntegerSlider",
}

// predictLabel generates the next available label for a bare command,
// mirroring GeoGebra's LabelManager.getNextIndexedLabel.
//
// usedLabels is the set of labels already taken (from prior statements).
// cmdName is the command name (used to look up the label type).
func predictLabel(cmdName string, usedLabels map[string]bool) string {
	labelType := labelTypeForCommand[strings.ToUpper(cmdName)]
	if labelType == "" {
		labelType = "General"
	}
	chars, ok := labelCharSets[labelType]
	if !ok {
		chars = labelCharSets["General"]
	}

	// Iterate through combinations: pass 0 (no index), pass 1 (index 1), etc.
	for q := 0; q < 1000; q++ { // safety limit
		for _, r := range chars {
			base := string(r)
			var label string
			if q == 0 {
				label = base
			} else if q < 10 {
				label = base + "_" + string(rune('0'+q))
			} else {
				label = base + "_{" + intToStr(q) + "}"
			}
			if !usedLabels[label] {
				return label
			}
		}
	}
	// Fallback: should never happen
	return "z_999"
}

func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

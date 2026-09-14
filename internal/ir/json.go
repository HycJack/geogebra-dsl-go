package ir

import (
	"encoding/json"
	"fmt"

	"github.com/hycjack/geogebra-dsl-go/internal/diag"
)

// jsonIR mirrors the input IR JSON shape (objects + goals). fields are kept
// permissive: unknown fields are ignored, required ones validated below.
type jsonIR struct {
	Objects []jsonObject `json:"objects"`
	Goals   []string     `json:"goals"`
}

type jsonObject struct {
	ID   string   `json:"id"`
	Kind string   `json:"kind,omitempty"` // optional; cross-checked
	Cmd  string   `json:"cmd,omitempty"`
	Args []string `json:"args,omitempty"`
	Refs []string `json:"refs,omitempty"`
}

// ParseJSON builds a *Graph from IR JSON bytes. Structural errors are returned
// as a diag.Problem slice with CodeParseJSON / CodeParseSyntax codes.
func ParseJSON(data []byte) (*Graph, []diag.Problem, error) {
	var j jsonIR
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, []diag.Problem{{
			Code: diag.CodeParseJSON,
			Msg:  "不是合法的 JSON：" + err.Error(),
		}}, err
	}
	if len(j.Objects) == 0 {
		return nil, []diag.Problem{{
			Code: diag.CodeParseJSON,
			Msg:  "IR 缺少 objects 数组",
		}}, fmt.Errorf("empty objects")
	}

	g := New()
	var probs []diag.Problem
	for i, jo := range j.Objects {
		if jo.ID == "" {
			probs = append(probs, diag.Problem{
				Code: diag.CodeParseJSON,
				Msg:  fmt.Sprintf("objects[%d] 缺少 id", i),
			})
			continue
		}
		if _, exists := g.Get(jo.ID); exists {
			probs = append(probs, diag.Problem{
				Code: diag.CodeDepRedefine,
				Msg:  "对象重复定义：" + jo.ID,
				Obj:  jo.ID,
			})
			continue // keep the first definition; skip the redefinition
		}
		kind := KindFromString(jo.Kind)
		g.Add(&Object{
			ID:   jo.ID,
			Cmd:  jo.Cmd,
			Kind: kind,
			Args: copyArgs(jo.Args),
			Refs: copyArgs(jo.Refs),
		})
	}
	g.SetGoals(copyArgs(j.Goals))
	return g, probs, nil
}

// KindFromString maps a JSON "kind" string to a Kind. Unknown strings map to
// KUnknown (the signature stage decides by cmd).
func KindFromString(s string) Kind {
	switch s {
	case "Point", "point":
		return KPoint
	case "Line", "line":
		return KLine
	case "Segment", "segment":
		return KSegment
	case "Ray", "ray":
		return KRay
	case "Vector", "vector":
		return KVector
	case "Circle", "circle":
		return KCircle
	case "Conic", "conic":
		return KConic
	case "Polygon", "polygon":
		return KPolygon
	case "Number", "number":
		return KNumber
	case "Boolean", "boolean", "Bool", "bool":
		return KBool
	case "Function", "function":
		return KFunction
	case "List", "list":
		return KList
	case "Plane", "plane":
		return KPlane
	case "Quadric", "quadric":
		return KQuadric
	case "Solid", "solid":
		return KSolid
	case "Polyhedron", "polyhedron":
		return KPolyhedron
	default:
		return KUnknown
	}
}

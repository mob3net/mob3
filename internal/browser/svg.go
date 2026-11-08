package browser

import (
	"bytes"
	"errors"
	"image"
	"image/draw"
	"strings"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
)

const maxSVGDim = 1024

var errBadSVG = errors.New("invalid SVG")

// isSVG reports whether fetched data is an SVG image.
func isSVG(ctype string, data []byte) bool {
	ct := strings.ToLower(ctype)
	if strings.Contains(ct, "image/svg+xml") {
		return true
	}
	head := strings.TrimSpace(string(data[:minInt(len(data), 1024)]))
	if strings.HasPrefix(strings.ToLower(head), "<svg") {
		return true
	}
	if strings.HasPrefix(head, "<?xml") && strings.Contains(strings.ToLower(head), "<svg") {
		return true
	}
	return false
}

// decodeSVG rasterizes an SVG into an RGBA image, bounded in size.
func decodeSVG(data []byte) (image.Image, error) {
	if len(data) == 0 {
		return nil, errBadSVG
	}
	icon, err := oksvg.ReadIconStream(bytes.NewReader(data), oksvg.IgnoreErrorMode)
	if err != nil {
		return nil, err
	}
	w, h := icon.ViewBox.W, icon.ViewBox.H
	if w <= 0 || h <= 0 {
		w, h = 300, 150
	}
	scale := 1.0
	if w > maxSVGDim {
		scale = float64(maxSVGDim) / w
	}
	if h > maxSVGDim {
		if s := float64(maxSVGDim) / h; s < scale {
			scale = s
		}
	}
	w = float64(maxInt(1, int(w*scale)))
	h = float64(maxInt(1, int(h*scale)))
	rgba := image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	draw.Draw(rgba, rgba.Bounds(), image.Transparent, image.Point{}, draw.Src)
	scanner := rasterx.NewScannerGV(int(w), int(h), rgba, rgba.Bounds())
	raster := rasterx.NewDasher(int(w), int(h), scanner)
	icon.SetTarget(0, 0, w, h)
	icon.Draw(raster, 1.0)
	return rgba, nil
}

// loadInlineSVGs rasterizes inline <svg> elements into their Node.Img field so
// layout can treat them as images.
func (l *Loader) loadInlineSVGs(root *Node) {
	nodes := root.FindAll(func(n *Node) bool { return n.Tag == "svg" })
	for _, n := range nodes {
		if n.Img != nil || n.ImgFailed {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(n.Attr("aria-hidden")), "true") {
			continue
		}
		img, err := decodeSVG([]byte(serializeSVG(n)))
		if err != nil {
			n.ImgFailed = true
			continue
		}
		n.Img = img
		n.ImgSrc = "inline-svg"
	}
}

var svgTagCase = map[string]string{
	"lineargradient":      "linearGradient",
	"radialgradient":      "radialGradient",
	"clippath":            "clipPath",
	"textpath":            "textPath",
	"foreignobject":       "foreignObject",
	"feblend":             "feBlend",
	"fecolormatrix":       "feColorMatrix",
	"fecomponenttransfer": "feComponentTransfer",
	"fecomposite":         "feComposite",
	"feconvolvematrix":    "feConvolveMatrix",
	"fediffuselighting":   "feDiffuseLighting",
	"fedisplacementmap":   "feDisplacementMap",
	"fedistantlight":      "feDistantLight",
	"fedropshadow":        "feDropShadow",
	"feflood":             "feFlood",
	"fefunca":             "feFuncA",
	"fefuncb":             "feFuncB",
	"fefuncg":             "feFuncG",
	"fefuncr":             "feFuncR",
	"fegaussianblur":      "feGaussianBlur",
	"feimage":             "feImage",
	"femerge":             "feMerge",
	"femergenode":         "feMergeNode",
	"femorphology":        "feMorphology",
	"feoffset":            "feOffset",
	"fepointlight":        "fePointLight",
	"fespecularlighting":  "feSpecularLighting",
	"fespotlight":         "feSpotLight",
	"fetile":              "feTile",
	"feturbulence":        "feTurbulence",
}

var svgAttrCase = map[string]string{
	"viewbox":                   "viewBox",
	"preserveaspectratio":       "preserveAspectRatio",
	"gradientunits":             "gradientUnits",
	"gradienttransform":         "gradientTransform",
	"patternunits":              "patternUnits",
	"patterncontentunits":       "patternContentUnits",
	"patterntransform":          "patternTransform",
	"clippathunits":             "clipPathUnits",
	"maskunits":                 "maskUnits",
	"maskcontentunits":          "maskContentUnits",
	"markerunits":               "markerUnits",
	"markerwidth":               "markerWidth",
	"markerheight":              "markerHeight",
	"refx":                      "refX",
	"refy":                      "refY",
	"spreadmethod":              "spreadMethod",
	"startoffset":               "startOffset",
	"textlength":                "textLength",
	"lengthadjust":              "lengthAdjust",
	"baseprofile":               "baseProfile",
	"zoomandpan":                "zoomAndPan",
	"attributename":             "attributeName",
	"attributetype":             "attributeType",
	"repeatcount":               "repeatCount",
	"repeatdur":                 "repeatDur",
	"keytimes":                  "keyTimes",
	"keysplines":                "keySplines",
	"keypoints":                 "keyPoints",
	"calcmode":                  "calcMode",
	"pathlength":                "pathLength",
	"requiredfeatures":          "requiredFeatures",
	"requiredextensions":        "requiredExtensions",
	"systemlanguage":            "systemLanguage",
	"externalresourcesrequired": "externalResourcesRequired",
	"xml:space":                 "xml:space",
}

func canonicalSVGTag(tag string) string {
	if v, ok := svgTagCase[tag]; ok {
		return v
	}
	return tag
}

func canonicalSVGAttr(name string) string {
	lower := strings.ToLower(name)
	if v, ok := svgAttrCase[lower]; ok {
		return v
	}
	return lower
}

// serializeSVG turns the parsed inline SVG subtree back into XML. HTML parsing
// lowercases names, so known SVG camelCase names are restored for oksvg.
func serializeSVG(n *Node) string {
	var b strings.Builder
	var walk func(*Node)
	walk = func(x *Node) {
		if x == nil {
			return
		}
		if x.Type == TextNode {
			b.WriteString(textEscape(x.Text))
			return
		}
		if x.Type != ElementNode {
			return
		}
		tag := canonicalSVGTag(x.Tag)
		b.WriteString("<")
		b.WriteString(tag)
		if x.Tag == "svg" && x.Attr("xmlns") == "" {
			b.WriteString(` xmlns="http://www.w3.org/2000/svg"`)
		}
		for _, a := range x.Attrs {
			b.WriteString(" ")
			b.WriteString(canonicalSVGAttr(a.Key))
			b.WriteString(`="`)
			b.WriteString(EscapeHTML(a.Value))
			b.WriteString(`"`)
		}
		if len(x.Children) == 0 {
			b.WriteString("/>")
			return
		}
		b.WriteString(">")
		for _, c := range x.Children {
			walk(c)
		}
		b.WriteString("</")
		b.WriteString(tag)
		b.WriteString(">")
	}
	walk(n)
	return b.String()
}

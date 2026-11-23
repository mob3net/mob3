package engine

import "strings"

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

// SerializeSVG turns the parsed inline SVG subtree back into XML. HTML parsing
// lowercases names, so known SVG camelCase names are restored for oksvg.
func SerializeSVG(n *Node) string {
	var b strings.Builder
	var walk func(*Node)
	walk = func(x *Node) {
		if x == nil {
			return
		}
		if x.Type == TextNode {
			b.WriteString(TextEscape(x.Text))
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

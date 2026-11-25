package engine

import (
	"strconv"
	"strings"

	"github.com/grafana/sobek"
)

// initCanvasObject adds the minimal canvas surface. There is no real pixel
// buffer: getContext("2d") returns a no-op context so libraries that feature
// test or color-parse canvas do not throw. WebGL contexts are unsupported.
func (j *jsRuntime) initCanvasObject(o *sobek.Object, n *Node) {
	dim := func(attr string, def int) (func() sobek.Value, func(sobek.Value)) {
		get := func() sobek.Value {
			if s := AttrValue(n, attr); s != "" {
				if v, err := strconv.Atoi(s); err == nil {
					return j.vm.ToValue(v)
				}
			}
			return j.vm.ToValue(def)
		}
		set := func(v sobek.Value) {
			SetAttr(n, attr, strconv.Itoa(int(v.ToInteger())))
			j.markDirty()
		}
		return get, set
	}
	gw, sw := dim("width", 300)
	gh, sh := dim("height", 150)
	j.accessor(o, "width", gw, sw)
	j.accessor(o, "height", gh, sh)
	j.method(o, "getContext", func(call sobek.FunctionCall) sobek.Value {
		kind := strings.ToLower(strings.TrimSpace(call.Argument(0).String()))
		if kind != "2d" {
			return sobek.Null()
		}
		if ctx := j.canvasContexts[n]; ctx != nil {
			return ctx
		}
		ctx := j.newCanvas2DContext(o)
		j.canvasContexts[n] = ctx
		return ctx
	})
	j.method(o, "toDataURL", func(sobek.FunctionCall) sobek.Value {
		return j.vm.ToValue("data:image/png;base64,")
	})
}

func (j *jsRuntime) newCanvas2DContext(canvas *sobek.Object) sobek.Value {
	ctx := j.vm.NewObject()
	_ = ctx.Set("canvas", canvas)
	_ = ctx.Set("fillStyle", "#000000")
	_ = ctx.Set("strokeStyle", "#000000")
	_ = ctx.Set("globalAlpha", 1)
	_ = ctx.Set("globalCompositeOperation", "source-over")
	_ = ctx.Set("lineWidth", 1)
	_ = ctx.Set("lineCap", "butt")
	_ = ctx.Set("lineJoin", "miter")
	_ = ctx.Set("font", "10px sans-serif")
	_ = ctx.Set("textAlign", "start")
	_ = ctx.Set("textBaseline", "alphabetic")
	_ = ctx.Set("shadowBlur", 0)
	_ = ctx.Set("shadowColor", "rgba(0, 0, 0, 0)")
	noop := j.vm.ToValue(func(sobek.FunctionCall) sobek.Value { return sobek.Undefined() })
	for _, name := range []string{
		"save", "restore", "scale", "rotate", "translate", "transform",
		"setTransform", "resetTransform", "clearRect", "fillRect", "strokeRect",
		"beginPath", "closePath", "moveTo", "lineTo", "bezierCurveTo",
		"quadraticCurveTo", "arc", "arcTo", "ellipse", "rect", "fill",
		"stroke", "clip", "drawImage", "putImageData", "setLineDash",
		"fillText", "strokeText", "drawFocusIfNeeded", "scrollPathIntoView",
		"createPattern", "isPointInPath", "isPointInStroke",
	} {
		_ = ctx.Set(name, noop)
	}
	j.method(ctx, "getLineDash", func(sobek.FunctionCall) sobek.Value { return j.vm.NewArray() })
	j.method(ctx, "measureText", func(call sobek.FunctionCall) sobek.Value {
		m := j.vm.NewObject()
		_ = m.Set("width", 0)
		_ = m.Set("actualBoundingBoxAscent", 0)
		_ = m.Set("actualBoundingBoxDescent", 0)
		_ = m.Set("actualBoundingBoxLeft", 0)
		_ = m.Set("actualBoundingBoxRight", 0)
		return m
	})
	makeGradient := func(sobek.FunctionCall) sobek.Value {
		g := j.vm.NewObject()
		_ = g.Set("addColorStop", noop)
		return g
	}
	j.method(ctx, "createLinearGradient", makeGradient)
	j.method(ctx, "createRadialGradient", makeGradient)
	j.method(ctx, "createConicGradient", makeGradient)
	j.method(ctx, "getImageData", func(call sobek.FunctionCall) sobek.Value {
		w := int(call.Argument(2).ToInteger())
		h := int(call.Argument(3).ToInteger())
		return j.newImageData(w, h)
	})
	j.method(ctx, "createImageData", func(call sobek.FunctionCall) sobek.Value {
		return j.newImageData(int(call.Argument(0).ToInteger()), int(call.Argument(1).ToInteger()))
	})
	return ctx
}

func (j *jsRuntime) newImageData(w, h int) *sobek.Object {
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	img := j.vm.NewObject()
	_ = img.Set("width", w)
	_ = img.Set("height", h)
	data := j.vm.NewArray()
	if ctor, ok := j.vm.Get("Uint8ClampedArray").(*sobek.Object); ok {
		if arr, err := j.vm.New(ctor, j.vm.ToValue(w*h*4)); err == nil {
			data = arr
		}
	}
	_ = img.Set("data", data)
	return img
}

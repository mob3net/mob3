package browser

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/dop251/goja"
)

// installIntl adds a practical subset of the ECMAScript Intl API. It is
// English-only and does not attempt full CLDR data, but it covers the
// formatters that ordinary pages commonly use.
func (j *jsRuntime) installIntl() {
	_ = j.vm.Set("__mob3IntlNumberFormat", j.vm.ToValue(j.newIntlNumberFormat))
	_ = j.vm.Set("__mob3IntlDateTimeFormat", j.vm.ToValue(j.newIntlDateTimeFormat))
	_ = j.vm.Set("__mob3IntlCollator", j.vm.ToValue(j.newIntlCollator))
	_ = j.vm.Set("__mob3IntlPluralRules", j.vm.ToValue(j.newIntlPluralRules))
	_ = j.vm.Set("__mob3IntlRelativeTimeFormat", j.vm.ToValue(j.newIntlRelativeTimeFormat))
	_ = j.vm.Set("__mob3IntlListFormat", j.vm.ToValue(j.newIntlListFormat))
	_ = j.vm.Set("__mob3IntlCanonicalLocales", j.vm.ToValue(j.intlCanonicalLocales))
	_ = j.vm.Set("__mob3IntlSupportedValuesOf", j.vm.ToValue(j.intlSupportedValuesOf))

	const script = `
(function () {
	var NF = globalThis.__mob3IntlNumberFormat;
	var DF = globalThis.__mob3IntlDateTimeFormat;
	var C = globalThis.__mob3IntlCollator;
	var PR = globalThis.__mob3IntlPluralRules;
	var RTF = globalThis.__mob3IntlRelativeTimeFormat;
	var LF = globalThis.__mob3IntlListFormat;
	var CL = globalThis.__mob3IntlCanonicalLocales;
	var SV = globalThis.__mob3IntlSupportedValuesOf;
	function wrap(ctor, fn, locales, options) {
		var o = fn(locales, options);
		Object.setPrototypeOf(o, ctor.prototype);
		return o;
	}
	function NumberFormat(locales, options) { return wrap(NumberFormat, NF, locales, options); }
	function DateTimeFormat(locales, options) { return wrap(DateTimeFormat, DF, locales, options); }
	function Collator(locales, options) { return wrap(Collator, C, locales, options); }
	function PluralRules(locales, options) { return wrap(PluralRules, PR, locales, options); }
	function RelativeTimeFormat(locales, options) { return wrap(RelativeTimeFormat, RTF, locales, options); }
	function ListFormat(locales, options) { return wrap(ListFormat, LF, locales, options); }
	globalThis.Intl = {
		NumberFormat: NumberFormat,
		DateTimeFormat: DateTimeFormat,
		Collator: Collator,
		PluralRules: PluralRules,
		RelativeTimeFormat: RelativeTimeFormat,
		ListFormat: ListFormat,
		getCanonicalLocales: function (locales) { return CL(locales); },
		supportedValuesOf: function (key) { return SV(key); }
	};
	delete globalThis.__mob3IntlNumberFormat;
	delete globalThis.__mob3IntlDateTimeFormat;
	delete globalThis.__mob3IntlCollator;
	delete globalThis.__mob3IntlPluralRules;
	delete globalThis.__mob3IntlRelativeTimeFormat;
	delete globalThis.__mob3IntlListFormat;
	delete globalThis.__mob3IntlCanonicalLocales;
	delete globalThis.__mob3IntlSupportedValuesOf;
})();
`
	if _, err := j.vm.RunScript("Intl", script); err != nil {
		j.reportError(err)
	}
}

func jsObject(v goja.Value) *goja.Object {
	if o, ok := v.(*goja.Object); ok {
		return o
	}
	return nil
}

func jsOptionString(o *goja.Object, key, def string) string {
	if o == nil {
		return def
	}
	v := o.Get(key)
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return def
	}
	return v.String()
}

func jsOptionInt(o *goja.Object, key string, def int) int {
	if o == nil {
		return def
	}
	v := o.Get(key)
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return def
	}
	return int(v.ToInteger())
}

func jsOptionBool(o *goja.Object, key string, def bool) bool {
	if o == nil {
		return def
	}
	v := o.Get(key)
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return def
	}
	return v.ToBoolean()
}

func (j *jsRuntime) newIntlNumberFormat(call goja.FunctionCall) goja.Value {
	options := jsObject(call.Argument(1))
	style := jsOptionString(options, "style", "decimal")
	currency := jsOptionString(options, "currency", "")
	minFrac := jsOptionInt(options, "minimumFractionDigits", -1)
	maxFrac := jsOptionInt(options, "maximumFractionDigits", -1)
	if minFrac < 0 {
		switch style {
		case "currency":
			minFrac = 2
		default:
			minFrac = 0
		}
	}
	if maxFrac < 0 {
		switch style {
		case "currency":
			maxFrac = 2
		case "percent":
			maxFrac = 0
		default:
			maxFrac = 3
		}
	}
	if maxFrac < minFrac {
		maxFrac = minFrac
	}
	if maxFrac > 20 {
		maxFrac = 20
	}
	if minFrac > 20 {
		minFrac = 20
	}
	useGrouping := jsOptionBool(options, "useGrouping", true)

	o := j.vm.NewObject()
	j.method(o, "format", func(call goja.FunctionCall) goja.Value {
		return j.vm.ToValue(formatIntlNumber(call.Argument(0).ToFloat(), style, currency, minFrac, maxFrac, useGrouping))
	})
	j.method(o, "formatToParts", func(call goja.FunctionCall) goja.Value {
		parts := intlNumberParts(call.Argument(0).ToFloat(), style, currency, minFrac, maxFrac, useGrouping)
		arr := j.vm.NewArray()
		for i, p := range parts {
			po := j.vm.NewObject()
			_ = po.Set("type", p.typ)
			_ = po.Set("value", p.value)
			_ = arr.Set(strconv.Itoa(i), po)
		}
		return arr
	})
	j.method(o, "formatRange", func(call goja.FunctionCall) goja.Value {
		a := formatIntlNumber(call.Argument(0).ToFloat(), style, currency, minFrac, maxFrac, useGrouping)
		b := formatIntlNumber(call.Argument(1).ToFloat(), style, currency, minFrac, maxFrac, useGrouping)
		return j.vm.ToValue(a + " – " + b)
	})
	j.method(o, "resolvedOptions", func(goja.FunctionCall) goja.Value {
		ro := j.vm.NewObject()
		_ = ro.Set("locale", "en-US")
		_ = ro.Set("numberingSystem", "latn")
		_ = ro.Set("style", style)
		if style == "currency" {
			_ = ro.Set("currency", currency)
		}
		_ = ro.Set("minimumFractionDigits", minFrac)
		_ = ro.Set("maximumFractionDigits", maxFrac)
		_ = ro.Set("useGrouping", useGrouping)
		return ro
	})
	return o
}

type intlPart struct {
	typ, value string
}

func formatIntlNumber(n float64, style, currency string, minFrac, maxFrac int, grouping bool) string {
	if math.IsNaN(n) {
		return "NaN"
	}
	if math.IsInf(n, 0) {
		if n < 0 {
			return "-∞"
		}
		return "∞"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	suffix := ""
	prefix := ""
	switch style {
	case "percent":
		n *= 100
		suffix = "%"
	case "currency":
		prefix = intlCurrencySymbol(currency)
	}
	scale := math.Pow(10, float64(maxFrac))
	n = math.Round(n*scale) / scale
	s := strconv.FormatFloat(n, 'f', maxFrac, 64)
	if i := strings.IndexByte(s, '.'); i >= 0 {
		frac := s[i+1:]
		for len(frac) > minFrac && strings.HasSuffix(frac, "0") {
			frac = frac[:len(frac)-1]
		}
		if frac == "" {
			s = s[:i]
		} else {
			s = s[:i] + "." + frac
		}
	}
	if grouping {
		intPart, frac := s, ""
		if i := strings.IndexByte(s, '.'); i >= 0 {
			intPart, frac = s[:i], s[i:]
		}
		var b strings.Builder
		for i, c := range intPart {
			if i > 0 && (len(intPart)-i)%3 == 0 {
				b.WriteByte(',')
			}
			b.WriteRune(c)
		}
		s = b.String() + frac
	}
	if neg {
		s = "-" + s
	}
	return prefix + s + suffix
}

func intlCurrencySymbol(code string) string {
	switch strings.ToUpper(code) {
	case "USD":
		return "$"
	case "EUR":
		return "€"
	case "GBP":
		return "£"
	case "JPY":
		return "¥"
	case "CNY":
		return "¥"
	}
	if code == "" {
		return ""
	}
	return code + " "
}

func intlNumberParts(n float64, style, currency string, minFrac, maxFrac int, grouping bool) []intlPart {
	s := formatIntlNumber(n, style, currency, minFrac, maxFrac, grouping)
	var parts []intlPart
	if strings.HasPrefix(s, "-") {
		parts = append(parts, intlPart{"minusSign", "-"})
		s = s[1:]
	}
	if style == "currency" {
		sym := intlCurrencySymbol(currency)
		if sym != "" && strings.HasPrefix(s, sym) {
			parts = append(parts, intlPart{"currency", sym})
			s = strings.TrimPrefix(s, sym)
		}
	}
	dot := strings.IndexByte(s, '.')
	suffix := ""
	if style == "percent" {
		suffix = "%"
		s = strings.TrimSuffix(s, suffix)
	}
	if dot < 0 {
		dot = len(s)
	}
	intPart := s[:dot]
	for _, group := range strings.Split(intPart, ",") {
		if group == "" {
			continue
		}
		if len(parts) > 0 && parts[len(parts)-1].typ == "integer" {
			parts = append(parts, intlPart{"group", ","})
		}
		parts = append(parts, intlPart{"integer", group})
	}
	if dot < len(s) {
		parts = append(parts, intlPart{"decimal", "."})
		parts = append(parts, intlPart{"fraction", s[dot+1:]})
	}
	if suffix != "" {
		parts = append(parts, intlPart{"percentSign", suffix})
	}
	return parts
}

type intlDateOptions struct {
	weekday, era, year, month, day     string
	hour, minute, second, timeZoneName string
	timeZone                           string
	hour12                             bool
	dateStyle, timeStyle               string
}

func (j *jsRuntime) newIntlDateTimeFormat(call goja.FunctionCall) goja.Value {
	options := jsObject(call.Argument(1))
	opts := intlDateOptions{
		weekday:      jsOptionString(options, "weekday", ""),
		era:          jsOptionString(options, "era", ""),
		year:         jsOptionString(options, "year", ""),
		month:        jsOptionString(options, "month", ""),
		day:          jsOptionString(options, "day", ""),
		hour:         jsOptionString(options, "hour", ""),
		minute:       jsOptionString(options, "minute", ""),
		second:       jsOptionString(options, "second", ""),
		timeZoneName: jsOptionString(options, "timeZoneName", ""),
		timeZone:     jsOptionString(options, "timeZone", ""),
		hour12:       jsOptionBool(options, "hour12", true),
		dateStyle:    jsOptionString(options, "dateStyle", ""),
		timeStyle:    jsOptionString(options, "timeStyle", ""),
	}
	if opts.dateStyle == "" && opts.timeStyle == "" &&
		opts.weekday == "" && opts.era == "" && opts.year == "" && opts.month == "" &&
		opts.day == "" && opts.hour == "" && opts.minute == "" && opts.second == "" {
		opts.year = "numeric"
		opts.month = "numeric"
		opts.day = "numeric"
	}
	loc := time.Local
	if opts.timeZone != "" {
		if l, err := time.LoadLocation(opts.timeZone); err == nil {
			loc = l
		}
	}
	o := j.vm.NewObject()
	j.method(o, "format", func(call goja.FunctionCall) goja.Value {
		return j.vm.ToValue(formatIntlDate(intlTimeValue(call.Argument(0)), opts, loc))
	})
	j.method(o, "formatToParts", func(call goja.FunctionCall) goja.Value {
		text := formatIntlDate(intlTimeValue(call.Argument(0)), opts, loc)
		arr := j.vm.NewArray()
		po := j.vm.NewObject()
		_ = po.Set("type", "literal")
		_ = po.Set("value", text)
		_ = arr.Set("0", po)
		return arr
	})
	j.method(o, "formatRange", func(call goja.FunctionCall) goja.Value {
		a := formatIntlDate(intlTimeValue(call.Argument(0)), opts, loc)
		b := formatIntlDate(intlTimeValue(call.Argument(1)), opts, loc)
		return j.vm.ToValue(a + " – " + b)
	})
	j.method(o, "resolvedOptions", func(goja.FunctionCall) goja.Value {
		ro := j.vm.NewObject()
		_ = ro.Set("locale", "en-US")
		_ = ro.Set("calendar", "gregory")
		_ = ro.Set("numberingSystem", "latn")
		_ = ro.Set("timeZone", loc.String())
		_ = ro.Set("hour12", opts.hour12)
		return ro
	})
	return o
}

func intlTimeValue(v goja.Value) time.Time {
	switch x := v.Export().(type) {
	case time.Time:
		return x
	case int64:
		return time.UnixMilli(x)
	case float64:
		return time.UnixMilli(int64(x))
	case string:
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02", "2006-01-02 15:04:05"} {
			if t, err := time.ParseInLocation(layout, x, time.Local); err == nil {
				return t
			}
		}
	}
	return time.Time{}
}

func formatIntlDate(t time.Time, o intlDateOptions, loc *time.Location) string {
	if t.IsZero() {
		return "Invalid Date"
	}
	t = t.In(loc)
	dateStyle := o.dateStyle
	timeStyle := o.timeStyle
	if dateStyle != "" {
		switch dateStyle {
		case "full":
			o.weekday, o.month, o.day, o.year = "long", "long", "numeric", "numeric"
		case "long":
			o.month, o.day, o.year = "long", "numeric", "numeric"
		case "medium":
			o.month, o.day, o.year = "short", "numeric", "numeric"
		case "short":
			o.month, o.day, o.year = "numeric", "numeric", "2-digit"
		}
	}
	if timeStyle != "" {
		switch timeStyle {
		case "full", "long", "medium":
			o.hour, o.minute, o.second = "numeric", "2-digit", "2-digit"
		case "short":
			o.hour, o.minute = "numeric", "2-digit"
		}
	}
	var parts []string
	if o.weekday != "" {
		parts = append(parts, intlWeekday(t, o.weekday))
	}
	hasDate := o.era != "" || o.year != "" || o.month != "" || o.day != ""
	if hasDate {
		numericDate := false
		if (o.month == "numeric" || o.month == "2-digit") &&
			(o.day == "numeric" || o.day == "2-digit") &&
			(o.year == "numeric" || o.year == "2-digit") {
			numericDate = true
		}
		if numericDate {
			month := strconv.Itoa(int(t.Month()))
			if o.month == "2-digit" {
				month = fmt.Sprintf("%02d", int(t.Month()))
			}
			day := strconv.Itoa(t.Day())
			if o.day == "2-digit" {
				day = fmt.Sprintf("%02d", t.Day())
			}
			year := strconv.Itoa(t.Year())
			if o.year == "2-digit" {
				year = fmt.Sprintf("%02d", t.Year()%100)
			}
			parts = append(parts, month+"/"+day+"/"+year)
		} else {
			var date []string
			if o.month != "" {
				date = append(date, intlMonth(t, o.month))
			}
			if o.day != "" {
				d := strconv.Itoa(t.Day())
				if o.day == "2-digit" {
					d = fmt.Sprintf("%02d", t.Day())
				}
				date = append(date, d)
			}
			if o.year != "" {
				y := strconv.Itoa(t.Year())
				if o.year == "2-digit" {
					y = fmt.Sprintf("%02d", t.Year()%100)
				}
				date = append(date, y)
			}
			if len(date) > 0 {
				parts = append(parts, strings.Join(date, " "))
			}
		}
	}
	hasTime := o.hour != "" || o.minute != "" || o.second != ""
	if hasTime {
		parts = append(parts, intlTime(t, o))
	}
	if o.timeZoneName != "" {
		parts = append(parts, t.Format("MST"))
	}
	return strings.Join(parts, ", ")
}

func intlWeekday(t time.Time, style string) string {
	switch style {
	case "long":
		return t.Weekday().String()
	case "short":
		return t.Format("Mon")
	case "narrow":
		return t.Format("Mon")[:1]
	}
	return t.Weekday().String()
}

func intlMonth(t time.Time, style string) string {
	switch style {
	case "long":
		return t.Format("January")
	case "short":
		return t.Format("Jan")
	case "narrow":
		return t.Format("Jan")[:1]
	case "2-digit":
		return fmt.Sprintf("%02d", int(t.Month()))
	}
	return strconv.Itoa(int(t.Month()))
}

func intlTime(t time.Time, o intlDateOptions) string {
	if o.hour12 {
		layout := "3 PM"
		if o.minute != "" {
			layout = "3:04 PM"
		}
		if o.second != "" {
			layout = "3:04:05 PM"
		}
		return t.Format(layout)
	}
	layout := "15"
	if o.minute != "" {
		layout = "15:04"
	}
	if o.second != "" {
		layout = "15:04:05"
	}
	return t.Format(layout)
}

func (j *jsRuntime) newIntlCollator(call goja.FunctionCall) goja.Value {
	o := j.vm.NewObject()
	j.method(o, "compare", func(call goja.FunctionCall) goja.Value {
		a := strings.ToLower(call.Argument(0).String())
		b := strings.ToLower(call.Argument(1).String())
		switch {
		case a < b:
			return j.vm.ToValue(-1)
		case a > b:
			return j.vm.ToValue(1)
		default:
			return j.vm.ToValue(0)
		}
	})
	j.method(o, "resolvedOptions", func(goja.FunctionCall) goja.Value {
		ro := j.vm.NewObject()
		_ = ro.Set("locale", "en-US")
		_ = ro.Set("usage", "sort")
		_ = ro.Set("sensitivity", "variant")
		_ = ro.Set("ignorePunctuation", false)
		return ro
	})
	return o
}

func (j *jsRuntime) newIntlPluralRules(call goja.FunctionCall) goja.Value {
	o := j.vm.NewObject()
	selectOne := func(n float64) string {
		if math.Abs(n) == 1 {
			return "one"
		}
		return "other"
	}
	j.method(o, "select", func(call goja.FunctionCall) goja.Value {
		return j.vm.ToValue(selectOne(call.Argument(0).ToFloat()))
	})
	j.method(o, "selectRange", func(call goja.FunctionCall) goja.Value {
		return j.vm.ToValue(selectOne(call.Argument(1).ToFloat()))
	})
	j.method(o, "resolvedOptions", func(goja.FunctionCall) goja.Value {
		ro := j.vm.NewObject()
		_ = ro.Set("locale", "en-US")
		_ = ro.Set("type", "cardinal")
		_ = ro.Set("pluralCategories", j.vm.NewArray("one", "other"))
		return ro
	})
	return o
}

func (j *jsRuntime) newIntlRelativeTimeFormat(call goja.FunctionCall) goja.Value {
	options := jsObject(call.Argument(1))
	numeric := jsOptionString(options, "numeric", "always")
	o := j.vm.NewObject()
	j.method(o, "format", func(call goja.FunctionCall) goja.Value {
		return j.vm.ToValue(intlRelative(call.Argument(0).ToFloat(), call.Argument(1).String(), numeric))
	})
	j.method(o, "formatToParts", func(call goja.FunctionCall) goja.Value {
		text := intlRelative(call.Argument(0).ToFloat(), call.Argument(1).String(), numeric)
		arr := j.vm.NewArray()
		po := j.vm.NewObject()
		_ = po.Set("type", "literal")
		_ = po.Set("value", text)
		_ = arr.Set("0", po)
		return arr
	})
	j.method(o, "resolvedOptions", func(goja.FunctionCall) goja.Value {
		ro := j.vm.NewObject()
		_ = ro.Set("locale", "en-US")
		_ = ro.Set("numeric", numeric)
		_ = ro.Set("style", "long")
		return ro
	})
	return o
}

func intlRelative(n float64, unit, numeric string) string {
	if numeric == "auto" {
		switch {
		case n == 0 && (unit == "second" || unit == "minute" || unit == "hour"):
			return "now"
		case n == -1 && unit == "day":
			return "yesterday"
		case n == 0 && unit == "day":
			return "today"
		case n == 1 && unit == "day":
			return "tomorrow"
		}
	}
	abs := math.Abs(n)
	word := unit
	if abs != 1 && !strings.HasSuffix(word, "s") {
		word += "s"
	}
	v := strconv.FormatFloat(abs, 'f', -1, 64)
	if n < 0 {
		return v + " " + word + " ago"
	}
	return "in " + v + " " + word
}

func (j *jsRuntime) newIntlListFormat(call goja.FunctionCall) goja.Value {
	options := jsObject(call.Argument(1))
	typ := jsOptionString(options, "type", "conjunction")
	style := jsOptionString(options, "style", "long")
	o := j.vm.NewObject()
	j.method(o, "format", func(call goja.FunctionCall) goja.Value {
		arr := jsObject(call.Argument(0))
		var vals []string
		if arr != nil {
			n := jsArrayLength(arr)
			for i := 0; i < n; i++ {
				vals = append(vals, arr.Get(strconv.Itoa(i)).String())
			}
		}
		return j.vm.ToValue(intlList(vals, typ, style))
	})
	j.method(o, "formatToParts", func(call goja.FunctionCall) goja.Value {
		arr := jsObject(call.Argument(0))
		var vals []string
		if arr != nil {
			n := jsArrayLength(arr)
			for i := 0; i < n; i++ {
				vals = append(vals, arr.Get(strconv.Itoa(i)).String())
			}
		}
		po := j.vm.NewObject()
		_ = po.Set("type", "literal")
		_ = po.Set("value", intlList(vals, typ, style))
		out := j.vm.NewArray()
		_ = out.Set("0", po)
		return out
	})
	j.method(o, "resolvedOptions", func(goja.FunctionCall) goja.Value {
		ro := j.vm.NewObject()
		_ = ro.Set("locale", "en-US")
		_ = ro.Set("type", typ)
		_ = ro.Set("style", style)
		return ro
	})
	return o
}

func intlList(vals []string, typ, style string) string {
	if len(vals) == 0 {
		return ""
	}
	if len(vals) == 1 {
		return vals[0]
	}
	sep := ", "
	if style == "narrow" {
		sep = " "
	}
	if len(vals) == 2 {
		if typ == "disjunction" {
			return vals[0] + " or " + vals[1]
		}
		return vals[0] + " and " + vals[1]
	}
	last := "and"
	if typ == "disjunction" {
		last = "or"
	}
	return strings.Join(vals[:len(vals)-1], sep) + sep + last + " " + vals[len(vals)-1]
}

func (j *jsRuntime) intlCanonicalLocales(call goja.FunctionCall) goja.Value {
	v := call.Argument(0)
	var out []string
	if o := jsObject(v); o != nil {
		n := jsArrayLength(o)
		for i := 0; i < n; i++ {
			out = append(out, o.Get(strconv.Itoa(i)).String())
		}
	} else if !goja.IsUndefined(v) && !goja.IsNull(v) {
		out = append(out, v.String())
	}
	return j.vm.ToValue(out)
}

func (j *jsRuntime) intlSupportedValuesOf(call goja.FunctionCall) goja.Value {
	key := call.Argument(0).String()
	var vals []string
	switch key {
	case "calendar":
		vals = []string{"gregory"}
	case "collation":
		vals = []string{"default"}
	case "currency":
		vals = []string{"CNY", "EUR", "GBP", "JPY", "USD"}
	case "numberingSystem":
		vals = []string{"latn"}
	case "timeZone":
		vals = []string{"UTC"}
	case "unit":
		vals = []string{"day", "hour", "minute", "month", "second", "week", "year"}
	default:
		return j.vm.NewArray()
	}
	return j.vm.ToValue(vals)
}

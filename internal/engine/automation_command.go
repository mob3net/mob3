package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// RunCommand executes one named automation command with JSON arguments. It is
// shared by protocol front ends so each transport only owns framing. Command
// names and argument shapes are the MCP tool definitions in mcp.go.
func (a *Automation) RunCommand(ctx context.Context, name string, args json.RawMessage) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if a == nil || a.b == nil {
		return "", errors.New("automation is closed")
	}
	switch name {
	case "navigate":
		var p struct {
			URL string `json:"url"`
		}
		if err := decodeArgs(args, &p); err != nil {
			return "", err
		}
		if strings.TrimSpace(p.URL) == "" {
			return "", errors.New("url is required")
		}
		if err := a.Navigate(ctx, p.URL); err != nil {
			return "", err
		}
		return a.Snapshot(), nil
	case "snapshot":
		return a.Snapshot(), nil
	case "click":
		var p struct {
			Link     int    `json:"link"`
			Text     string `json:"text"`
			Selector string `json:"selector"`
			X        *int   `json:"x"`
			Y        *int   `json:"y"`
		}
		if err := decodeArgs(args, &p); err != nil {
			return "", err
		}
		target := ClickTarget{Link: p.Link, Text: p.Text, Selector: p.Selector}
		targets := 0
		if target.Link > 0 {
			targets++
		}
		if target.Text != "" {
			targets++
		}
		if target.Selector != "" {
			targets++
		}
		if p.X != nil || p.Y != nil {
			if p.X == nil || p.Y == nil {
				return "", errors.New("click x and y must be provided together")
			}
			target.X, target.Y, target.HasCoords = *p.X, *p.Y, true
			targets++
		}
		if targets == 0 {
			return "", errors.New("click needs link, text, selector, or x/y")
		}
		if targets > 1 {
			return "", errors.New("click takes only one of link, text, selector, or x/y")
		}
		if err := a.Click(ctx, target); err != nil {
			return "", err
		}
		return a.Snapshot(), nil
	case "wait":
		var p struct {
			MS *int `json:"ms"`
		}
		if err := decodeArgs(args, &p); err != nil {
			return "", err
		}
		ms := 100
		if p.MS != nil {
			ms = *p.MS
		}
		waited, err := a.Wait(ctx, ms)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("waited %dms", waited), nil
	case "wait_for":
		var p struct {
			Selector  string `json:"selector"`
			Text      string `json:"text"`
			TimeoutMS int    `json:"timeout_ms"`
		}
		if err := decodeArgs(args, &p); err != nil {
			return "", err
		}
		return a.WaitFor(ctx, p.Selector, p.Text, p.TimeoutMS)
	case "requests":
		var p struct {
			Limit *int `json:"limit"`
		}
		if err := decodeArgs(args, &p); err != nil {
			return "", err
		}
		limit := 100
		if p.Limit != nil {
			limit = *p.Limit
		}
		return a.Requests(limit), nil
	case "logs":
		var p struct {
			Limit *int `json:"limit"`
		}
		if err := decodeArgs(args, &p); err != nil {
			return "", err
		}
		limit := 200
		if p.Limit != nil {
			limit = *p.Limit
		}
		return a.Logs(limit), nil
	case "source":
		var p struct {
			Selector string `json:"selector"`
		}
		if err := decodeArgs(args, &p); err != nil {
			return "", err
		}
		return a.Source(p.Selector)
	case "links":
		var p struct {
			Selector string `json:"selector"`
		}
		if err := decodeArgs(args, &p); err != nil {
			return "", err
		}
		return a.Links(p.Selector)
	case "form":
		var p struct {
			Selector string `json:"selector"`
		}
		if err := decodeArgs(args, &p); err != nil {
			return "", err
		}
		return a.FormFields(p.Selector)
	case "fill":
		var p struct {
			Selector string            `json:"selector"`
			Value    *string           `json:"value"`
			Fields   map[string]string `json:"fields"`
		}
		if err := decodeArgs(args, &p); err != nil {
			return "", err
		}
		fields := p.Fields
		if p.Selector != "" {
			if len(p.Fields) > 0 {
				return "", errors.New("fill takes selector/value or fields, not both")
			}
			if p.Value == nil {
				return "", errors.New("fill selector needs a value")
			}
			fields = map[string]string{p.Selector: *p.Value}
		}
		if len(fields) == 0 {
			return "", errors.New("fill needs selector/value or fields")
		}
		count, err := a.Fill(fields)
		if err != nil {
			return "", err
		}
		if err := a.waitPendingLocked(ctx); err != nil {
			return "", err
		}
		return fmt.Sprintf("filled %d field(s)", count), nil
	case "select":
		var p struct {
			Selector string   `json:"selector"`
			Value    *string  `json:"value"`
			Values   []string `json:"values"`
			Text     string   `json:"text"`
			Index    *int     `json:"index"`
		}
		if err := decodeArgs(args, &p); err != nil {
			return "", err
		}
		if p.Selector == "" {
			return "", errors.New("select needs a selector")
		}
		msg, err := a.Select(p.Selector, p.Value, p.Values, p.Text, p.Index)
		if err != nil {
			return "", err
		}
		if err := a.waitPendingLocked(ctx); err != nil {
			return "", err
		}
		return msg, nil
	case "toggle":
		var p struct {
			Selector string `json:"selector"`
			Checked  *bool  `json:"checked"`
		}
		if err := decodeArgs(args, &p); err != nil {
			return "", err
		}
		if p.Selector == "" {
			return "", errors.New("toggle needs a selector")
		}
		msg, err := a.Toggle(p.Selector, p.Checked)
		if err != nil {
			return "", err
		}
		if err := a.waitPendingLocked(ctx); err != nil {
			return "", err
		}
		return msg, nil
	case "submit":
		var p struct {
			Selector string `json:"selector"`
		}
		if err := decodeArgs(args, &p); err != nil {
			return "", err
		}
		if err := a.Submit(ctx, p.Selector); err != nil {
			return "", err
		}
		return a.Snapshot(), nil
	case "type":
		var p struct {
			Text string `json:"text"`
		}
		if err := decodeArgs(args, &p); err != nil {
			return "", err
		}
		if err := a.Type(ctx, p.Text); err != nil {
			return "", err
		}
		return "typed", nil
	case "press":
		var p struct {
			Key string `json:"key"`
		}
		if err := decodeArgs(args, &p); err != nil {
			return "", err
		}
		if err := a.Press(ctx, p.Key); err != nil {
			return "", err
		}
		return "pressed " + p.Key, nil
	case "scroll":
		var p struct {
			Lines  int  `json:"lines"`
			Pages  int  `json:"pages"`
			Top    bool `json:"top"`
			Bottom bool `json:"bottom"`
		}
		if err := decodeArgs(args, &p); err != nil {
			return "", err
		}
		if p.Lines == 0 && p.Pages == 0 && !p.Top && !p.Bottom {
			p.Lines = 1
		}
		a.Scroll(p.Lines, p.Pages, p.Top, p.Bottom)
		return fmt.Sprintf("scrolled (scroll %d%%)", a.b.scrollPercent()), nil
	case "back":
		if err := a.Back(ctx); err != nil {
			return "", err
		}
		return a.Snapshot(), nil
	case "forward":
		if err := a.Forward(ctx); err != nil {
			return "", err
		}
		return a.Snapshot(), nil
	case "reload":
		if err := a.Reload(ctx); err != nil {
			return "", err
		}
		return a.Snapshot(), nil
	case "evaluate":
		var p struct {
			Expression string `json:"expression"`
		}
		if err := decodeArgs(args, &p); err != nil {
			return "", err
		}
		v, err := a.Evaluate(ctx, p.Expression)
		if err != nil {
			return "", err
		}
		if v == "" {
			return "undefined", nil
		}
		return v, nil
	case "history":
		h := a.History()
		if h == "" {
			return "(no history)", nil
		}
		return h, nil
	default:
		return "", fmt.Errorf("unknown tool %q", name)
	}
}

func decodeArgs(raw json.RawMessage, dst any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	return nil
}

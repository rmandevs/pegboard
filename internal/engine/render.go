package engine

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/rmandevs/pegboard/internal/state"
)

var symbols = map[Action]string{
	Create: "+", Update: "~", Delete: "-", Forget: "-", Conflict: "!", Adopt: "=",
}

// Pretty shortens paths for display.
type Pretty func(string) string

// WriteText writes the plan as in SPEC §6.
func (p *Plan) WriteText(w io.Writer, pretty Pretty) {
	if p.Empty() {
		fmt.Fprintln(w, "No changes: every tool is in its silhouette.")
		return
	}
	width := 0
	for _, c := range p.Changes {
		if l := len(c.Kind) + 1 + len(c.Name); c.Visible() && l > width {
			width = l
		}
	}
	last := ""
	for _, c := range p.Changes {
		if !c.Visible() {
			continue
		}
		if head := c.Target + " · " + string(c.Scope); head != last {
			fmt.Fprintln(w, head)
			last = head
		}
		detail := "(" + pretty(c.File) + ")"
		switch {
		case c.Detail != "":
			detail = "(" + pretty(c.File) + " " + c.Detail + ")"
		case c.Link != "":
			detail = "(" + pretty(c.File) + " → " + pretty(c.Link) + ")"
		}
		if c.Reason != "" {
			detail = c.Reason
		}
		switch {
		case c.Action == Conflict && c.do == Update:
			detail += " (--force: overwrite)"
		case c.Action == Conflict && c.do == Delete:
			detail += " (--force: remove)"
		}
		if c.Origin == state.Adhoc {
			detail += " [ad-hoc]"
		}
		fmt.Fprintf(w, "  %s %-*s  %s\n", symbols[c.Action], width, c.Kind+" "+c.Name, detail)
		for _, warn := range c.Warnings {
			fmt.Fprintf(w, "      ⚠ %s\n", warn)
		}
	}
	s := p.Summary()
	line := fmt.Sprintf("Plan: %d create, %d update, %d remove, %d %s",
		s[Create], s[Update], s[Delete]+s[Forget], s[Conflict], plural(s[Conflict], "conflict", "conflicts"))
	if s[Adopt] > 0 {
		line += fmt.Sprintf(", %d adopt", s[Adopt])
	}
	fmt.Fprintln(w, line+".")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

type changeJSON struct {
	Scope    string   `json:"scope"`
	Target   string   `json:"target"`
	Kind     string   `json:"kind"`
	Name     string   `json:"name"`
	Action   Action   `json:"action"`
	Forced   Action   `json:"forced,omitempty"`
	Origin   string   `json:"origin,omitempty"`
	File     string   `json:"file"`
	Reason   string   `json:"reason,omitempty"`
	Detail   string   `json:"detail,omitempty"`
	Warnings []string `json:"warnings"`
}

// PlanJSON is the stable format of `plan --json`.
type PlanJSON struct {
	Changes  []changeJSON   `json:"changes"`
	Summary  map[Action]int `json:"summary"`
	Blocked  bool           `json:"blocked"`
	Warnings []string       `json:"warnings"`
}

// JSON builds the stable form of the plan. warnings are general notices.
func (p *Plan) JSON(warnings []string) PlanJSON {
	out := PlanJSON{Changes: []changeJSON{}, Summary: map[Action]int{}, Blocked: p.Blocked(), Warnings: warnings}
	if out.Warnings == nil {
		out.Warnings = []string{}
	}
	for _, a := range []Action{Create, Update, Delete, Conflict, Adopt} {
		out.Summary[a] = 0
	}
	for a, n := range p.Summary() {
		if a == Forget {
			a = Delete
		}
		out.Summary[a] += n
	}
	for _, c := range p.Changes {
		if !c.Visible() {
			continue
		}
		cj := changeJSON{
			Scope: string(c.Scope), Target: c.Target, Kind: c.Kind, Name: c.Name, Action: c.Action,
			Forced: c.Forced, Origin: string(c.Origin), File: c.File, Reason: c.Reason, Detail: c.Detail,
			Warnings: c.Warnings,
		}
		if cj.Warnings == nil {
			cj.Warnings = []string{}
		}
		out.Changes = append(out.Changes, cj)
	}
	return out
}

// WriteJSON writes v indented.
func WriteJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// WriteDriftText lists drift; ad-hoc entries are shown separately.
func WriteDriftText(w io.Writer, items []DriftItem, pretty Pretty) {
	var managed, adhoc []DriftItem
	for _, it := range items {
		if it.Origin == state.Adhoc {
			adhoc = append(adhoc, it)
		} else {
			managed = append(managed, it)
		}
	}
	label := map[DriftKind]string{InSync: "ok", Edited: "edited manually", Missing: "deleted manually"}
	section := func(title string, list []DriftItem) {
		if len(list) == 0 {
			return
		}
		fmt.Fprintln(w, title)
		for _, it := range list {
			fmt.Fprintf(w, "  %s · %s  %s %s  %s (%s)\n", it.Target, it.Scope, it.Resource, it.Name, label[it.Kind], pretty(it.File))
		}
	}
	if len(items) == 0 {
		fmt.Fprintln(w, "pegboard has not written anything yet.")
		return
	}
	section("Managed by pegboard:", managed)
	section("Ad-hoc, not declared:", adhoc)
	n := 0
	for _, it := range items {
		if it.Kind != InSync {
			n++
		}
	}
	if n == 0 {
		fmt.Fprintln(w, "No drift.")
	} else {
		fmt.Fprintf(w, "Drift: %d %s.\n", n, plural(n, "entry", "entries"))
	}
}

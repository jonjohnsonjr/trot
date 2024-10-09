package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"golang.org/x/exp/slices"
)

func main() {
	if err := mainE(os.Stdout, os.Stdin); err != nil {
		log.Fatal(err)
	}
}

func mainE(w io.Writer, r io.Reader) error {
	spans := map[string]*Span{}
	children := map[string][]*Span{}

	i := 0

	dec := json.NewDecoder(r)
	for {
		i++
		var span Span
		if err := dec.Decode(&span); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}

			return fmt.Errorf("line %d: %w", i, err)
		}

		spans[span.SpanContext.SpanID] = &span

		kids, ok := children[span.Parent.SpanID]
		if !ok {
			kids = []*Span{}
		}
		kids = append(kids, &span)
		children[span.Parent.SpanID] = kids
	}

	missing := map[string]struct{}{}

	for parent := range children {
		if _, ok := spans[parent]; !ok {
			missing[parent] = struct{}{}
		}
	}
	for missed := range missing {
		log.Printf("missing %q", missed)
	}

	// TODO: This feels not right.
	rootSpans, ok := children["0000000000000000"]
	if !ok {
		log.Printf("no root")

		for missed := range missing {
			root := &Node{
				Span: &Span{
					Name: "Missing span",
					SpanContext: SpanContext{
						SpanID: missed,
					},
				},
			}

			buildTree(root, children, spans)

			writeSpan(w, nil, root)
		}
	}

	fmt.Fprint(w, header)

	root := &Node{
		Span: &Span{
			Name: "root",
			SpanContext: SpanContext{
				SpanID: "0000000000000000",
			},
		},
	}

	for _, rootSpan := range rootSpans {
		root.Children = append(root.Children, &Node{
			Span: rootSpan,
		})
	}

	buildTree(root, children, spans)

	writeSpan(w, nil, root)

	fmt.Fprint(w, footer)
	return nil
}

func buildTree(root *Node, children map[string][]*Span, spans map[string]*Span) {
	kids, ok := children[root.Span.SpanContext.SpanID]
	if !ok {
		return
	}

	root.Children = make([]*Node, len(kids))
	for i, kid := range kids {
		node := &Node{
			Span: kid,
		}
		buildTree(node, children, spans)
		root.Children[i] = node
	}

	slices.SortFunc(root.Children, func(a, b *Node) int {
		return a.Span.StartTime.Compare(b.Span.StartTime)
	})

	if root.Span.StartTime == root.Span.EndTime {
		root.Span.StartTime = root.Children[0].Span.StartTime

		last := slices.MaxFunc(root.Children, func(a, b *Node) int {
			return a.Span.EndTime.Compare(b.Span.EndTime)
		})
		root.Span.EndTime = last.Span.EndTime
	}
}

func writeSpan(w io.Writer, parent, node *Node) {
	if parent == nil {
		fmt.Fprint(w, `<div>`)
	} else {
		total := parent.Span.EndTime.Sub(parent.Span.StartTime)
		left := node.Span.StartTime.Sub(parent.Span.StartTime)
		right := parent.Span.EndTime.Sub(node.Span.EndTime)

		leftpad := float64(left) / float64(total)
		rightpad := float64(right) / float64(total)

		if len(node.Children) == 0 {
			fmt.Fprintf(w, `<div style="margin: 1px %f%% 0 %f%%">`, 100.0*rightpad, 100.0*leftpad)
		} else {
			fmt.Fprintf(w, `<div class="parent" style="margin: 1px %f%% 0 %f%%">`, 100.0*rightpad, 100.0*leftpad)
		}
	}

	dur := node.Span.EndTime.Sub(node.Span.StartTime)

	if len(node.Children) == 0 {
		fmt.Fprintf(w, `<span>%s %s</span>`, node.Span.Name, dur)
	} else {
		if parent == nil {
			// Default to root being open.
			fmt.Fprintf(w, `<details open><summary>%s %s</summary>`, node.Span.Name, dur)
		} else {
			fmt.Fprintf(w, `<details><summary>%s %s</summary>`, node.Span.Name, dur)
		}
		for _, child := range node.Children {
			writeSpan(w, node, child)
		}
		fmt.Fprint(w, `</details>`)
	}
	fmt.Fprintln(w, "</div>")
}

const header = `
<html>
<head>
<title>trot</title>
<style>
summary {
  border: 1px solid;
  display: block;
  white-space: nowrap;
  padding: 3px;
}
span {
  border: 1px solid;
  display: block;
  white-space: nowrap;
  padding: 3px;
}
body {
	width: 100%;
	margin: 0px;
}
div.parent:hover {
	outline: 1.5px solid lightgrey;
}
</style>
</head>
<body>`

const footer = `
    </body>
</html>
`

type Node struct {
	Span     *Span
	Children []*Node
}

type SpanContext struct {
	TraceID    string `json:"TraceID"`
	SpanID     string `json:"SpanID"`
	TraceFlags string `json:"TraceFlags"`
	TraceState string `json:"TraceState"`
	Remote     bool   `json:"Remote"`
}

// Thank you mholt.
type Span struct {
	Name        string      `json:"Name"`
	SpanContext SpanContext `json:"SpanContext"`
	Parent      struct {
		TraceID    string `json:"TraceID"`
		SpanID     string `json:"SpanID"`
		TraceFlags string `json:"TraceFlags"`
		TraceState string `json:"TraceState"`
		Remote     bool   `json:"Remote"`
	} `json:"Parent"`
	SpanKind   int       `json:"SpanKind"`
	StartTime  time.Time `json:"StartTime"`
	EndTime    time.Time `json:"EndTime"`
	Attributes any       `json:"Attributes"`
	Events     any       `json:"Events"`
	Links      any       `json:"Links"`
	Status     struct {
		Code        string `json:"Code"`
		Description string `json:"Description"`
	} `json:"Status"`
	DroppedAttributes int `json:"DroppedAttributes"`
	DroppedEvents     int `json:"DroppedEvents"`
	DroppedLinks      int `json:"DroppedLinks"`
	ChildSpanCount    int `json:"ChildSpanCount"`
	Resource          []struct {
		Key   string `json:"Key"`
		Value struct {
			Type  string `json:"Type"`
			Value string `json:"Value"`
		} `json:"Value"`
	} `json:"Resource"`
	InstrumentationLibrary struct {
		Name      string `json:"Name"`
		Version   string `json:"Version"`
		SchemaURL string `json:"SchemaURL"`
	} `json:"InstrumentationLibrary"`
}

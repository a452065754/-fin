package fr

import (
	"bytes"
	"encoding/xml"
	"io"
	"strconv"
	"strings"
)

// XNode is a lightweight, schema-free XML tree. FineReport templates are
// highly polymorphic, so a generic tree is more practical than typed structs.
type XNode struct {
	Name     string
	AttrMap  map[string]string
	Children []*XNode
	Text     string
}

func ParseXML(data []byte) (*XNode, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	dec.AutoClose = xml.HTMLAutoClose
	var root *XNode
	var stack []*XNode
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &XNode{Name: t.Name.Local, AttrMap: make(map[string]string, len(t.Attr))}
			for _, a := range t.Attr {
				n.AttrMap[a.Name.Local] = a.Value
			}
			if len(stack) > 0 {
				p := stack[len(stack)-1]
				p.Children = append(p.Children, n)
			} else if root == nil {
				root = n
			}
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].Text += string(t)
			}
		}
	}
	if root == nil {
		return nil, io.ErrUnexpectedEOF
	}
	return root, nil
}

func (n *XNode) Attr(name string) string {
	if n == nil {
		return ""
	}
	return n.AttrMap[name]
}

func (n *XNode) AttrInt(name string, def int) int {
	v := strings.TrimSpace(n.Attr(name))
	if v == "" {
		return def
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return i
}

func (n *XNode) AttrFloat(name string, def float64) float64 {
	v := strings.TrimSpace(n.Attr(name))
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

func (n *XNode) AttrBool(name string, def bool) bool {
	v := strings.TrimSpace(strings.ToLower(n.Attr(name)))
	if v == "" {
		return def
	}
	return v == "true" || v == "1"
}

func (n *XNode) Child(name string) *XNode {
	if n == nil {
		return nil
	}
	for _, c := range n.Children {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func (n *XNode) ChildAll(name string) []*XNode {
	if n == nil {
		return nil
	}
	var out []*XNode
	for _, c := range n.Children {
		if c.Name == name {
			out = append(out, c)
		}
	}
	return out
}

func (n *XNode) TextTrim() string {
	if n == nil {
		return ""
	}
	return strings.TrimSpace(n.Text)
}

func (n *XNode) Find(name string) *XNode {
	if n == nil {
		return nil
	}
	if n.Name == name {
		return n
	}
	for _, c := range n.Children {
		if r := c.Find(name); r != nil {
			return r
		}
	}
	return nil
}

func (n *XNode) FindAll(name string) []*XNode {
	var out []*XNode
	var walk func(*XNode)
	walk = func(x *XNode) {
		if x.Name == name {
			out = append(out, x)
		}
		for _, c := range x.Children {
			walk(c)
		}
	}
	walk(n)
	return out
}

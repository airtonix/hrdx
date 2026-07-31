package ui

// splitNode is a binary layout tree. A node is either a leaf holding one
// pane, or a split with two children. vertical means the children sit side
// by side (the divider is a vertical line).
type splitNode struct {
	pane     *pane
	vertical bool
	ratio    float64
	a, b     *splitNode
}

func leafNode(target *pane) *splitNode { return &splitNode{pane: target} }

func (n *splitNode) walk(visit func(*pane)) {
	if n == nil {
		return
	}
	if n.pane != nil {
		visit(n.pane)
		return
	}
	n.a.walk(visit)
	n.b.walk(visit)
}

func (n *splitNode) contains(target *pane) bool {
	found := false
	n.walk(func(current *pane) {
		if current == target {
			found = true
		}
	})
	return found
}

// insertAt turns the leaf holding target into a split of target and add.
func insertAt(n *splitNode, target, add *pane, vertical bool) bool {
	return insertAtSide(n, target, add, vertical, false)
}

// insertAtSide splits the target leaf. With before the new pane takes the
// first slot (left of or above the target), otherwise the second.
func insertAtSide(n *splitNode, target, add *pane, vertical, before bool) bool {
	if n == nil {
		return false
	}
	if n.pane == target {
		if before {
			n.a = leafNode(add)
			n.b = leafNode(target)
		} else {
			n.a = leafNode(target)
			n.b = leafNode(add)
		}
		n.pane = nil
		n.vertical = vertical
		n.ratio = 0.5
		return true
	}
	if n.pane != nil {
		return false
	}
	return insertAtSide(n.a, target, add, vertical, before) || insertAtSide(n.b, target, add, vertical, before)
}

// removeAt deletes the leaf holding target, promoting its sibling.
func removeAt(root **splitNode, target *pane) bool {
	node := *root
	if node == nil {
		return false
	}
	if node.pane == target {
		*root = nil
		return true
	}
	if node.pane != nil {
		return false
	}
	if node.a.pane == target {
		*root = node.b
		return true
	}
	if node.b.pane == target {
		*root = node.a
		return true
	}
	return removeAt(&node.a, target) || removeAt(&node.b, target)
}

// parentSplit returns the split whose direct child is the leaf holding target.
func parentSplit(n *splitNode, target *pane) *splitNode {
	if n == nil || n.pane != nil {
		return nil
	}
	if n.a.pane == target || n.b.pane == target {
		return n
	}
	if n.a.contains(target) {
		return parentSplit(n.a, target)
	}
	return parentSplit(n.b, target)
}

type rect struct{ x, y, w, h int }

type paneRect struct {
	pane *pane
	r    rect
}

type divRect struct {
	node *splitNode // the split this boundary belongs to
	r    rect       // grabbable boundary cells (adjacent pane borders)
	full rect       // the split's whole rect, for drag ratio math
}

const (
	minPaneCols = 10
	minPaneRows = 4
)

// splitRects computes the child rects of a split node. Panes touch; each
// pane draws its own border, so the boundary is the two adjacent border
// cells, returned as the grabbable divider strip.
func splitRects(n *splitNode, r rect) (ra, rd, rb rect) {
	if n.vertical {
		aw := int(float64(r.w) * n.ratio)
		aw = clampInt(aw, minPaneCols, r.w-minPaneCols)
		ra = rect{r.x, r.y, aw, r.h}
		rb = rect{r.x + aw, r.y, r.w - aw, r.h}
		rd = rect{r.x + aw - 1, r.y, 2, r.h}
		return ra, rd, rb
	}
	ah := int(float64(r.h) * n.ratio)
	ah = clampInt(ah, minPaneRows, r.h-minPaneRows)
	ra = rect{r.x, r.y, r.w, ah}
	rb = rect{r.x, r.y + ah, r.w, r.h - ah}
	rd = rect{r.x, r.y + ah - 1, r.w, 2}
	return ra, rd, rb
}

func layoutNode(n *splitNode, r rect, panes *[]paneRect, divs *[]divRect) {
	if n == nil {
		return
	}
	if n.pane != nil {
		*panes = append(*panes, paneRect{n.pane, r})
		return
	}
	ra, rd, rb := splitRects(n, r)
	*divs = append(*divs, divRect{node: n, r: rd, full: r})
	layoutNode(n.a, ra, panes, divs)
	layoutNode(n.b, rb, panes, divs)
}

func clampInt(value, low, high int) int {
	if high < low {
		return low
	}
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func clampFloat(value, low, high float64) float64 {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func (r rect) hit(x, y int) bool {
	return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

// inner returns the content rect inside a pane's border.
func (r rect) inner() rect {
	return rect{r.x + 1, r.y + 1, max(1, r.w-2), max(1, r.h-2)}
}

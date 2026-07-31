package ui

import "testing"

func TestSplitInsertLayoutRemove(t *testing.T) {
	a := &pane{id: 1}
	b := &pane{id: 2}
	c := &pane{id: 3}

	root := leafNode(a)
	if !insertAt(root, a, b, true) {
		t.Fatal("insertAt a/b failed")
	}
	if !insertAt(root, b, c, false) {
		t.Fatal("insertAt b/c failed")
	}

	var panes []paneRect
	var divs []divRect
	layoutNode(root, rect{0, 0, 100, 40}, &panes, &divs)
	if len(panes) != 3 || len(divs) != 2 {
		t.Fatalf("panes=%d divs=%d, want 3/2", len(panes), len(divs))
	}

	// Every pane rect must respect minimums and stay inside the area.
	for _, pr := range panes {
		if pr.r.w < minPaneCols || pr.r.h < minPaneRows {
			t.Fatalf("pane %d rect %+v below minimum", pr.pane.id, pr.r)
		}
		if pr.r.x < 0 || pr.r.y < 0 || pr.r.x+pr.r.w > 100 || pr.r.y+pr.r.h > 40 {
			t.Fatalf("pane %d rect %+v out of bounds", pr.pane.id, pr.r)
		}
	}

	// Removing b promotes c into b's slot; two panes remain.
	if !removeAt(&root, b) {
		t.Fatal("removeAt b failed")
	}
	panes = nil
	divs = nil
	layoutNode(root, rect{0, 0, 100, 40}, &panes, &divs)
	if len(panes) != 2 || len(divs) != 1 {
		t.Fatalf("after remove: panes=%d divs=%d, want 2/1", len(panes), len(divs))
	}
	for _, pr := range panes {
		if pr.pane == b {
			t.Fatal("removed pane still present")
		}
	}
}

func TestDividerDragChangesRatio(t *testing.T) {
	model := newTestModel("/tmp/api")
	currentSpace := model.spaces[0]
	model.addPane(currentSpace, "shell", true)

	_, divs := model.layoutAll(currentSpace.tab())
	if len(divs) != 1 {
		t.Fatalf("divs = %d, want 1", len(divs))
	}
	model.drag = divs[0].node
	model.dragFull = divs[0].full
	model.applyDrag(divs[0].full.x+divs[0].full.w/4, 0)
	if ratio := divs[0].node.ratio; ratio > 0.3 {
		t.Fatalf("ratio = %f, want roughly 0.25", ratio)
	}
}

func TestInsertAtSideBefore(t *testing.T) {
	first := &pane{id: 1, name: "a"}
	second := &pane{id: 2, name: "b"}
	root := leafNode(first)
	if !insertAtSide(root, first, second, true, true) {
		t.Fatal("insertAtSide failed")
	}
	if root.a.pane != second || root.b.pane != first {
		t.Fatalf("order = %v/%v, want new pane first", root.a.pane.name, root.b.pane.name)
	}
	if !root.vertical {
		t.Fatal("split should be vertical")
	}
}

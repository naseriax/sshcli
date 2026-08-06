package main

import "testing"

func TestRefreshVisibleWindowLimitsItemsForSmallTerminal(t *testing.T) {
	choices := make([]string, 20)
	for i := range choices {
		choices[i] = "item"
	}

	m := baseModel{
		choices:      choices,
		allChoices:   choices,
		cursor:       15,
		windowHeight: 8,
		isSSHContext: true,
	}

	m.refreshVisibleWindow()

	if m.visibleCount != 3 {
		t.Fatalf("expected visibleCount to be 3 for a small terminal, got %d", m.visibleCount)
	}

	if m.visibleStart != 13 {
		t.Fatalf("expected visibleStart to be 13 for cursor 15, got %d", m.visibleStart)
	}
}

package tui

type layout struct {
	width          int
	height         int
	inputWidth     int
	viewportWidth  int
	viewportHeight int
	historyHeight  int
}

func newLayout(width, height int) layout {
	width = max(1, width)
	height = max(1, height)
	fixedInputLines := 8
	return layout{
		width:          width,
		height:         height,
		inputWidth:     max(1, width-4),
		viewportWidth:  max(1, width-2),
		viewportHeight: max(1, height-3),
		historyHeight:  max(0, height-fixedInputLines),
	}
}

func visibleRange(total, selected, capacity int) (int, int) {
	if total <= 0 || capacity <= 0 {
		return 0, 0
	}
	selected = min(max(0, selected), total-1)
	start := max(0, selected-capacity+1)
	end := min(total, start+capacity)
	return start, end
}

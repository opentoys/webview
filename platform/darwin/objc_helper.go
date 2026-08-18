package darwin

// cgRect is the layout of a CoreGraphics CGRect (x, y, width, height as CGFloat).
// Passed by value to objc Send; purego marshals struct args directly.
type cgRect struct {
	X, Y, W, H float64
}

func rect(x, y, w, h float64) cgRect {
	return cgRect{X: x, Y: y, W: w, H: h}
}

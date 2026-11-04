package windowshape

import "testing"

func TestAZeroHandleIsNotClipped(t *testing.T) {
	ClipTop(0, 120)
	ClipTop(0, 0)
}

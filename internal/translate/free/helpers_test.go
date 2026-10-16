package free

import "errors"

// errorsIs is errors.Is, under this package's own name: the tests read better
// saying which error they want than saying how they test for it.
func errorsIs(err, target error) bool { return errors.Is(err, target) }

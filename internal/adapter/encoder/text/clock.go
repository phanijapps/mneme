package text

import "time"

// timeNowUTC is a seam for tests; production code uses real UTC time.
var timeNowUTC = func() time.Time { return time.Now().UTC() }

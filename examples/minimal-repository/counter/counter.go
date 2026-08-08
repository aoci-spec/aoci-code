package counter

// Counter stores a mutable integer for the minimal AOCI onboarding example.
type Counter struct {
	value int
}

// Increment increases the counter by one.
func (c *Counter) Increment() {
	c.value++
}

// Value returns the current counter value.
func (c Counter) Value() int {
	return c.value
}

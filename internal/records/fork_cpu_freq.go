package records

// cpuFreqAverager rolls the cpu frequency stat [average MHz, highest MHz, ceiling MHz] up into
// longer records: the average is averaged, the highest value keeps its peak, the ceiling is the latest.
type cpuFreqAverager struct {
	avgSum  uint64
	peak    uint16
	ceiling uint16
	count   uint64
}

func (c *cpuFreqAverager) add(freq [3]uint16) {
	if freq == ([3]uint16{}) {
		return
	}
	c.avgSum += uint64(freq[0])
	c.peak = max(c.peak, freq[1])
	if freq[2] != 0 {
		c.ceiling = freq[2]
	}
	c.count++
}

func (c *cpuFreqAverager) result() [3]uint16 {
	if c.count == 0 {
		return [3]uint16{}
	}
	return [3]uint16{uint16(c.avgSum / c.count), c.peak, c.ceiling}
}

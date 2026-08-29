package domain

// MaxHistoryTextBytes bounds the UTF-8 encoded size of one text value retained
// in Task history or copied into the WorkItem event log.
const MaxHistoryTextBytes = 32 << 10

func validateHistoryText(field, value string) error {
	if len(value) > MaxHistoryTextBytes {
		return invalid(field, "exceeds the maximum of %d bytes", MaxHistoryTextBytes)
	}
	return nil
}

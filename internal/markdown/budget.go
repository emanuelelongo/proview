package markdown

// Default per-document budgets. Hosts that keep several documents open can pass
// a smaller allowance through LoadWithBudget so the workspace shares one cap.
const (
	MaxDocumentImageBytes     int64 = 64 << 20
	MaxDocumentAnimationBytes int64 = 128 << 20
)

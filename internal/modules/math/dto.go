package math

import "errors"

const maxOperandAbs int64 = 1_000_000_000

// AddRequest defines the validated addition payload.
type AddRequest struct {
	A int64 `json:"a"`
	B int64 `json:"b"`
}

// AddResponse defines the addition result payload.
type AddResponse struct {
	Sum          int64 `json:"sum"`
	HistoryCount int   `json:"history_count"`
}

// ValidateAddRequest enforces safe operand bounds.
func ValidateAddRequest(req AddRequest) error {
	if req.A > maxOperandAbs || req.A < -maxOperandAbs || req.B > maxOperandAbs || req.B < -maxOperandAbs {
		return errors.New("operand exceeds safe template bounds")
	}
	return nil
}

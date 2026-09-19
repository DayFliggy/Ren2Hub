package common

import (
	"bytes"
	"errors"
	"math"
	"strconv"
)

// Quota is an exact amount in 1/500000 USD units. JSON amounts are strings so
// browsers never round a wallet balance through IEEE-754 numbers.
type Quota int64

var ErrQuotaOverflow = errors.New("quota arithmetic overflow")

func (q Quota) MarshalJSON() ([]byte, error) {
	return Marshal(strconv.FormatInt(int64(q), 10))
}

func (q *Quota) UnmarshalJSON(data []byte) error {
	if q == nil || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("quota must be an integer string")
	}
	var value string
	if err := Unmarshal(data, &value); err != nil {
		return errors.New("quota must be an integer string")
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || strconv.FormatInt(n, 10) != value {
		return errors.New("quota must be a canonical integer string")
	}
	*q = Quota(n)
	return nil
}

func AddQuota(a, b Quota) (Quota, error) {
	if b > 0 && a > Quota(math.MaxInt64)-b || b < 0 && a < Quota(math.MinInt64)-b {
		return 0, ErrQuotaOverflow
	}
	return a + b, nil
}

func NegateQuota(q Quota) (Quota, error) {
	if q == Quota(math.MinInt64) {
		return 0, ErrQuotaOverflow
	}
	return -q, nil
}

func SubtractQuota(a, b Quota) (Quota, error) {
	if b > 0 && a < Quota(math.MinInt64)+b || b < 0 && a > Quota(math.MaxInt64)+b {
		return 0, ErrQuotaOverflow
	}
	return a - b, nil
}
